package peer

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"p2p-chat/internal/crypto"
	"p2p-chat/internal/network"
	"p2p-chat/internal/protocol"
	"p2p-chat/internal/storage"
	"p2p-chat/internal/ui"
)

const (
	defaultHistoryDBPath = "p2p-chat-history.db"
	defaultFilesDirPath  = "p2p-files"
	defaultFilesDBPath   = "p2p-files.db"
)

var (
	bootstrapFlag = flag.String("bootstrap", "http://127.0.0.1:8000", "bootstrap base url")
	listenFlag    = flag.String("listen", "", "address to listen on (host:port)")
	portFlag      = flag.Int("port", 9001, "port to listen on when --listen empty")
	nickFlag      = flag.String("nick", "", "nickname displayed in chat")
	usernameFlag  = flag.String("username", "", "authenticated username (overrides --nick)")
	tokenFlag     = flag.String("token", "", "JWT token for authenticated username")
	secretFlag    = flag.String("secret", "", "shared secret for AES-256 encryption")
	pollFlag      = flag.Duration("poll", 5*time.Second, "interval to refresh peers list")
	historyFlag   = flag.Int("history", 200, "amount of messages kept locally")
	noColorFlag   = flag.Bool("no-color", false, "disable ANSI colors in CLI output")
	enableTUIFlag = flag.Bool("tui", false, "enable terminal UI mode")
	enableWebFlag = flag.Bool("web", false, "serve local web UI")
	webAddrFlag   = flag.String("web-addr", "127.0.0.1:8081", "address for embedded web UI server")
	historyDBFlag = flag.String("history-db", defaultHistoryDBPath, "path to persisted chat history db")
	filesDirFlag  = flag.String("files-dir", defaultFilesDirPath, "directory to store uploaded files")
	filesDBFlag   = flag.String("files-db", defaultFilesDBPath, "path to persisted file metadata db")
	dataDirFlag   = flag.String("data-dir", "p2p-data", "base directory for auto-generated peer data (history/files)")
	authAPIFlag   = flag.String("auth-api", "http://127.0.0.1:8089", "authentication server base url")
)

// Config captures runtime settings for a peer instance.
type Config struct {
	BootstrapURL string
	ListenAddr   string
	Port         int
	Nick         string
	Username     string
	Token        string
	Secret       string
	PollEvery    time.Duration
	HistorySize  int
	NoColor      bool
	EnableTUI    bool
	EnableWeb    bool
	WebAddr      string
	HistoryDB    string
	FilesDir     string
	FilesDB      string
	DataDir      string
	AuthAPI      string
}

var (
	cfgOnce      sync.Once
	parsedConfig Config
)

// LoadConfig parses command-line flags (once) and returns a snapshot of the peer configuration.
func LoadConfig() Config {
	cfgOnce.Do(func() {
		flag.Parse()
		parsedConfig = Config{
			BootstrapURL: *bootstrapFlag,
			ListenAddr:   *listenFlag,
			Port:         *portFlag,
			Nick:         *nickFlag,
			Username:     *usernameFlag,
			Token:        *tokenFlag,
			Secret:       *secretFlag,
			PollEvery:    *pollFlag,
			HistorySize:  *historyFlag,
			NoColor:      *noColorFlag,
			EnableTUI:    *enableTUIFlag,
			EnableWeb:    *enableWebFlag,
			WebAddr:      *webAddrFlag,
			HistoryDB:    *historyDBFlag,
			FilesDir:     *filesDirFlag,
			FilesDB:      *filesDBFlag,
			DataDir:      *dataDirFlag,
			AuthAPI:      *authAPIFlag,
		}
	})
	return parsedConfig
}

// App wires dependencies together and exposes lifecycle hooks.
type App struct {
	runtime      *protocol.Runtime
	cancel       context.CancelFunc
	enableCLI    bool
	enableTUI    bool
	tui          *ui.TUIDisplay
	startOnce    sync.Once
	shutdownOnce sync.Once
}

// NewApp wires up the peer runtime based on the provided configuration.
func NewApp(cfg Config) (*App, error) {
	ctx, cancel := context.WithCancel(context.Background())

	addr := cfg.ListenAddr
	if addr == "" {
		addr = fmt.Sprintf("127.0.0.1:%d", cfg.Port)
	}

	dataDir := cfg.DataDir
	if dataDir == "" {
		dataDir = "p2p-data"
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		cancel()
		return nil, fmt.Errorf("init data dir: %w", err)
	}
	peerDir := derivePeerDir(dataDir, addr)
	if err := os.MkdirAll(peerDir, 0o755); err != nil {
		cancel()
		return nil, fmt.Errorf("prepare peer dir: %w", err)
	}

	historyPath := cfg.HistoryDB
	if historyPath == "" || historyPath == defaultHistoryDBPath {
		historyPath = filepath.Join(peerDir, "history.db")
	}
	filesDBPath := cfg.FilesDB
	if filesDBPath == "" || filesDBPath == defaultFilesDBPath {
		filesDBPath = filepath.Join(peerDir, "files.db")
	}
	filesDir := cfg.FilesDir
	if filesDir == "" || filesDir == defaultFilesDirPath {
		filesDir = filepath.Join(peerDir, "files")
	}

	pollEvery := cfg.PollEvery
	if pollEvery <= 0 {
		pollEvery = 5 * time.Second
	}
	historySize := cfg.HistorySize
	if historySize <= 0 {
		historySize = 200
	}

	box, err := crypto.NewBox(cfg.Secret)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("init encryption: %w", err)
	}

	cm := network.NewConnManager(addr, box)
	if err := cm.StartListen(); err != nil {
		cancel()
		return nil, fmt.Errorf("listen failed: %w", err)
	}
	log.Printf("peer listening on %s (encryption:%t)", addr, cm.EncryptionEnabled())

	store, err := storage.OpenHistoryStore(historyPath)
	if err != nil {
		log.Printf("history db unavailable (%v), running without persistence", err)
	}

	var files *storage.FileStore
	if cfg.EnableWeb {
		files, err = storage.OpenFileStore(filesDBPath, filesDir)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("file store: %w", err)
		}
	}

	identity := protocol.NewIdentity(cfg.Nick, addr)
	if cfg.Username != "" && cfg.Token != "" {
		identity.SetAuth(cfg.Username, cfg.Token)
	}

	blocklist := protocol.NewBlockList()
	directory := protocol.NewPeerDirectory()
	metrics := protocol.NewMetrics()
	dialer := protocol.NewDialScheduler(cm, addr)
	ack := protocol.NewAckTracker(cm)

	runtime := protocol.NewRuntime(ctx, protocol.RuntimeOptions{
		ConnManager:  cm,
		CacheTTL:     10 * time.Minute,
		HistorySize:  historySize,
		Store:        store,
		Files:        files,
		Blocklist:    blocklist,
		Directory:    directory,
		Metrics:      metrics,
		Ack:          ack,
		Dialer:       dialer,
		Sink:         nil,
		Identity:     identity,
		SelfAddr:     addr,
		Web:          nil,
		BootstrapURL: cfg.BootstrapURL,
		PollInterval: pollEvery,
		AuthAPI:      cfg.AuthAPI,
	})

	if name := identity.Get(); name != "" {
		directory.Record(name, addr)
	}

	sinks := []ui.Sink{}
	cliSink := ui.NewCLIDisplay(ui.ShouldUseColor(cfg.NoColor))
	enableCLI := !cfg.EnableTUI
	if enableCLI {
		sinks = append(sinks, cliSink)
	}

	var tuiSink *ui.TUIDisplay
	if cfg.EnableTUI {
		tuiSink = ui.NewTUIDisplay(runtime.ProcessLine)
		sinks = append(sinks, tuiSink)
	}

	var webSink *ui.WebBridge
	if cfg.EnableWeb {
		setter := func(user, token string) error {
			if runtime.Identity().SetAuth(user, token) {
				if sink := runtime.Sink(); sink != nil {
					sink.ShowSystem(fmt.Sprintf("logged in as %s", user))
				}
				runtime.BroadcastHandshake()
			}
			return nil
		}
		share := func(record storage.FileRecord, target string) error {
			return runtime.ShareFile(record, target)
		}
		webSink, err = ui.NewWebBridge(cfg.WebAddr, runtime.History(), func(line string) { runtime.ProcessLine(line) }, setter, files, share)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("web ui: %w", err)
		}
		sinks = append(sinks, webSink)
		runtime.SetWeb(webSink)
	}

	runtime.SetSink(ui.NewMultiSink(sinks...))

	return &App{
		runtime:   runtime,
		cancel:    cancel,
		enableCLI: enableCLI,
		enableTUI: cfg.EnableTUI,
		tui:       tuiSink,
	}, nil
}

func derivePeerDir(base, addr string) string {
	if base == "" {
		base = "."
	}
	hostPart := "peer"
	portPart := "peer"
	if host, port, err := net.SplitHostPort(addr); err == nil {
		if host != "" {
			hostPart = sanitizePathToken(host)
		}
		if port != "" {
			portPart = sanitizePathToken(port)
		}
	} else if addr != "" {
		hostPart = sanitizePathToken(strings.ReplaceAll(addr, ":", "_"))
	}
	folder := fmt.Sprintf("%s-%s", hostPart, portPart)
	return filepath.Join(base, folder)
}

func sanitizePathToken(val string) string {
	val = strings.TrimSpace(val)
	if val == "" {
		return "peer"
	}
	var b strings.Builder
	for _, r := range val {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_':
			b.WriteRune(r)
		case r == '.', r == ':':
			b.WriteRune('-')
		}
	}
	out := b.String()
	if out == "" {
		return "peer"
	}
	return out
}
