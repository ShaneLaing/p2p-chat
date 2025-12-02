package peer

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"p2p-chat/internal/crypto"
	"p2p-chat/internal/message"
	"p2p-chat/internal/network"
)

const (
	msgTypeChat      = "chat"
	msgTypeDM        = "dm"
	msgTypeAck       = "ack"
	msgTypePeerSync  = "peer_sync"
	msgTypeHandshake = "handshake"
	msgTypeFile      = "file"

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

// App owns the peer runtime and associated resources.
type App struct {
	*appContext
	cancel       context.CancelFunc
	enableCLI    bool
	enableTUI    bool
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

	cache := newMsgCache(10 * time.Minute)
	history := newHistory(historySize)

	store, err := openHistoryStore(historyPath)
	if err != nil {
		log.Printf("history db unavailable (%v), running without persistence", err)
	}

	var files *fileStore
	if cfg.EnableWeb {
		files, err = openFileStore(filesDBPath, filesDir)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("file store: %w", err)
		}
	}

	identity := newIdentity(cfg.Nick, addr)
	if cfg.Username != "" && cfg.Token != "" {
		identity.SetAuth(cfg.Username, cfg.Token)
	}

	blocklist := newBlockList()
	directory := newPeerDirectory()
	metrics := newMetrics()
	dialer := newDialScheduler(cm, addr)
	ack := newAckTracker(cm)

	appCtx := &appContext{
		ctx:          ctx,
		cm:           cm,
		cache:        cache,
		history:      history,
		store:        store,
		files:        files,
		blocklist:    blocklist,
		directory:    directory,
		metrics:      metrics,
		ack:          ack,
		dialer:       dialer,
		identity:     identity,
		selfAddr:     addr,
		bootstrapURL: cfg.BootstrapURL,
		pollInterval: pollEvery,
		authAPI:      cfg.AuthAPI,
	}

	if name := identity.Get(); name != "" {
		directory.Record(name, addr)
	}

	sinks := []displaySink{}
	cliSink := newCLIDisplay(shouldUseColor(cfg.NoColor))
	enableCLI := !cfg.EnableTUI
	if enableCLI {
		sinks = append(sinks, cliSink)
	}

	var tuiSink *tuiDisplay
	if cfg.EnableTUI {
		tuiSink = newTUIDisplay(func(line string) { processLine(appCtx, line) })
		sinks = append(sinks, tuiSink)
	}

	var webSink *webBridge
	if cfg.EnableWeb {
		setter := func(user, token string) error {
			if appCtx.identity.SetAuth(user, token) {
				appCtx.sink.ShowSystem(fmt.Sprintf("logged in as %s", user))
				broadcastHandshake(appCtx)
			}
			return nil
		}
		share := func(record fileRecord, target string) error {
			return shareFile(appCtx, record, target)
		}
		webSink, err = newWebBridge(cfg.WebAddr, history, func(line string) { processLine(appCtx, line) }, setter, files, share)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("web ui: %w", err)
		}
		sinks = append(sinks, webSink)
		appCtx.web = webSink
	}

	appCtx.sink = newMultiSink(sinks...)

	return &App{
		appContext: appCtx,
		cancel:     cancel,
		enableCLI:  enableCLI,
		enableTUI:  cfg.EnableTUI,
	}, nil
}

// Start launches background goroutines and optional UIs.
func (a *App) Start() {
	if a == nil {
		return
	}
	a.startOnce.Do(func() {
		if a.enableTUI && a.web == nil {
			// ensure sink exists even in TUI-only mode
		}
		if a.enableTUI && a.appContext != nil {
			if td, ok := findTUISink(a.sink); ok {
				go func() {
					if err := td.Run(a.ctx); err != nil {
						log.Printf("tui error: %v", err)
					}
				}()
			}
		}
		if a.enableCLI {
			go readCLIInput(a.appContext)
		}
		if a.web != nil {
			go a.web.Run(a.ctx)
		}

		if err := registerSelf(a.appContext); err != nil {
			log.Printf("register failed: %v", err)
		}
		connectToBootstrapPeers(a.appContext)
		broadcastHandshake(a.appContext)

		go a.dialer.Run(a.ctx)
		go handleIncoming(a.appContext)
		go pollBootstrapLoop(a.appContext)
		go gossipLoop(a.appContext)
		go updatePeerListLoop(a.appContext)
		go presenceHeartbeatLoop(a.appContext)
	})
}

// Shutdown stops background goroutines and releases resources.
func (a *App) Shutdown() {
	if a == nil {
		return
	}
	a.shutdownOnce.Do(func() {
		a.cancel()
		if a.web != nil {
			a.web.Close()
		}
		if a.dialer != nil {
			a.dialer.Close()
		}
		if a.ack != nil {
			a.ack.Stop()
		}
		if a.cm != nil {
			a.cm.Stop()
		}
		if a.store != nil {
			a.store.Close()
		}
		if a.files != nil {
			a.files.Close()
		}
	})
}

// WaitForShutdown blocks until an interrupt signal arrives, then shuts down the peer gracefully.
func WaitForShutdown(app *App) {
	if app == nil {
		return
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("shutting down...")
	app.Shutdown()
}

func findTUISink(s displaySink) (*tuiDisplay, bool) {
	if ms, ok := s.(*multiSink); ok {
		for _, sink := range ms.sinks {
			if td, ok := sink.(*tuiDisplay); ok {
				return td, true
			}
		}
	}
	if td, ok := s.(*tuiDisplay); ok {
		return td, true
	}
	return nil, false
}

func handleCommand(app *appContext, line string) {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return
	}
	switch parts[0] {
	case "/peers":
		conns := app.cm.ConnsList()
		desired := app.dialer.Desired()
		app.sink.ShowSystem(fmt.Sprintf("connected: %v | desired: %v", conns, desired))
	case "/history":
		for _, msg := range app.history.All() {
			app.sink.ShowMessage(msg)
		}
	case "/save":
		if len(parts) < 2 {
			app.sink.ShowSystem("usage: /save <path>")
			return
		}
		if err := saveHistoryToFile(app.history.All(), parts[1]); err != nil {
			app.sink.ShowSystem(fmt.Sprintf("save failed: %v", err))
			return
		}
		app.sink.ShowSystem("history saved")
	case "/load":
		limit := 20
		if len(parts) >= 2 {
			if v, err := strconv.Atoi(parts[1]); err == nil {
				limit = v
			}
		}
		if app.store == nil {
			app.sink.ShowSystem("history persistence disabled")
			return
		}
		records, err := app.store.Recent(limit)
		if err != nil {
			app.sink.ShowSystem(fmt.Sprintf("load failed: %v", err))
			return
		}
		for i := len(records) - 1; i >= 0; i-- {
			app.sink.ShowMessage(records[i])
		}
	case "/msg":
		if len(parts) < 3 {
			app.sink.ShowSystem("usage: /msg <target> <message>")
			return
		}
		target := parts[1]
		idx := strings.Index(line, target)
		content := strings.TrimSpace(line[idx+len(target):])
		if content == "" {
			app.sink.ShowSystem("message required")
			return
		}
		sendDirectMessage(app, target, content)
	case "/file":
		if len(parts) < 2 {
			app.sink.ShowSystem("usage: /file <path> [target]")
			return
		}
		target := ""
		if len(parts) >= 3 {
			target = parts[2]
		}
		if err := sendFileFromPath(app, parts[1], target); err != nil {
			app.sink.ShowSystem(fmt.Sprintf("file send failed: %v", err))
		}
	case "/nick":
		if len(parts) < 2 {
			app.sink.ShowSystem("usage: /nick <name>")
			return
		}
		if app.identity.SetDisplay(parts[1]) {
			app.sink.ShowSystem(fmt.Sprintf("nickname set to %s", parts[1]))
			broadcastHandshake(app)
		}
	case "/stats":
		snap := app.metrics.Snapshot()
		app.sink.ShowSystem(snap.String())
	case "/block":
		if len(parts) < 2 {
			app.sink.ShowSystem("usage: /block <name|addr>")
			return
		}
		app.blocklist.Add(parts[1])
		app.sink.ShowSystem(fmt.Sprintf("blocked %s", parts[1]))
	case "/unblock":
		if len(parts) < 2 {
			app.sink.ShowSystem("usage: /unblock <name|addr>")
			return
		}
		app.blocklist.Remove(parts[1])
		app.sink.ShowSystem(fmt.Sprintf("unblocked %s", parts[1]))
	case "/blocked":
		app.sink.ShowSystem(fmt.Sprintf("blocked: %v", app.blocklist.List()))
	case "/quit":
		app.sink.ShowSystem("bye")
		os.Exit(0)
	default:
		app.sink.ShowSystem("commands: /peers /history /save /load /msg /file /nick /stats /block /unblock /blocked /quit")
	}
}

func registerSelf(app *appContext) error {
	if app.bootstrapURL == "" {
		return nil
	}
	payload := map[string]string{"addr": app.selfAddr}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(strings.TrimRight(app.bootstrapURL, "/")+"/register", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return nil
}

func fetchPeers(url string) ([]string, error) {
	resp, err := http.Get(strings.TrimRight(url, "/") + "/peers")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var peers []string
	if err := json.NewDecoder(resp.Body).Decode(&peers); err != nil {
		return nil, err
	}
	return peers, nil
}

func connectToBootstrapPeers(app *appContext) {
	if app.bootstrapURL == "" {
		return
	}
	peers, err := fetchPeers(app.bootstrapURL)
	if err != nil {
		log.Printf("fetch peers: %v", err)
		return
	}
	for _, peer := range peers {
		if peer == app.selfAddr {
			continue
		}
		app.dialer.Add(peer)
		if err := app.cm.ConnectToPeer(peer); err != nil {
			log.Printf("connect to %s: %v", peer, err)
		}
	}
}

func pollBootstrapLoop(app *appContext) {
	if app.bootstrapURL == "" {
		return
	}
	ticker := time.NewTicker(app.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-app.ctx.Done():
			return
		case <-ticker.C:
			peers, err := fetchPeers(app.bootstrapURL)
			if err != nil {
				log.Printf("poll peers: %v", err)
				continue
			}
			for _, peer := range peers {
				if peer == app.selfAddr {
					continue
				}
				app.dialer.Add(peer)
			}
		}
	}
}

func gossipLoop(app *appContext) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-app.ctx.Done():
			return
		case <-ticker.C:
			peers := app.dialer.Desired()
			if len(peers) == 0 {
				continue
			}
			msg := message.Message{
				MsgID:     newMsgID(),
				Type:      msgTypePeerSync,
				From:      app.identity.Get(),
				Origin:    app.selfAddr,
				Timestamp: time.Now(),
				PeerList:  peers,
			}
			app.cm.Broadcast(msg, "")
		}
	}
}

func updatePeerListLoop(app *appContext) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-app.ctx.Done():
			return
		case <-ticker.C:
			addrs := app.cm.ConnsList()
			app.directory.MarkActive(addrs)
			app.sink.UpdatePeers(app.directory.Snapshot())
		}
	}
}

func presenceHeartbeatLoop(app *appContext) {
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-app.ctx.Done():
			return
		case <-ticker.C:
			broadcastHandshake(app)
		}
	}
}

func saveHistoryToFile(entries []message.Message, path string) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// appContext aggregates peer state shared across goroutines.
type appContext struct {
	ctx          context.Context
	cm           *network.ConnManager
	cache        *msgCache
	history      *historyBuffer
	store        *historyStore
	files        *fileStore
	blocklist    *blockList
	directory    *peerDirectory
	metrics      *metrics
	ack          *ackTracker
	dialer       *dialScheduler
	sink         displaySink
	identity     *identity
	selfAddr     string
	web          *webBridge
	bootstrapURL string
	pollInterval time.Duration
	authAPI      string
}

type msgCache struct {
	mu   sync.Mutex
	seen map[string]time.Time
	ttl  time.Duration
}

func newMsgCache(ttl time.Duration) *msgCache {
	return &msgCache{seen: make(map[string]time.Time), ttl: ttl}
}

func (m *msgCache) Seen(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id == "" {
		return false
	}
	now := time.Now()
	if ts, ok := m.seen[id]; ok && now.Sub(ts) < m.ttl {
		return true
	}
	m.seen[id] = now
	for key, ts := range m.seen {
		if now.Sub(ts) > m.ttl {
			delete(m.seen, key)
		}
	}
	return false
}

type historyBuffer struct {
	mu     sync.Mutex
	max    int
	buffer []message.Message
}

func newHistory(max int) *historyBuffer {
	if max <= 0 {
		max = 50
	}
	return &historyBuffer{max: max}
}

func (h *historyBuffer) Add(msg message.Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.buffer = append(h.buffer, msg)
	if len(h.buffer) > h.max {
		h.buffer = h.buffer[len(h.buffer)-h.max:]
	}
}

func (h *historyBuffer) All() []message.Message {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]message.Message, len(h.buffer))
	copy(out, h.buffer)
	return out
}

type identity struct {
	mu    sync.RWMutex
	name  string
	token string
}

func newIdentity(initial, fallback string) *identity {
	if initial == "" {
		initial = fallback
	}
	return &identity{name: initial}
}

func (i *identity) Get() string {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.name
}

func (i *identity) Token() string {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.token
}

func (i *identity) SetDisplay(name string) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	if name == "" || i.name == name {
		return false
	}
	i.name = name
	return true
}

func (i *identity) SetAuth(name, token string) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	if name == "" || token == "" {
		return false
	}
	changed := i.name != name || i.token != token
	i.name = name
	i.token = token
	return changed
}

func newMsgID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", b)
}

func broadcastHandshake(app *appContext) {
	name := app.identity.Get()
	if name == "" {
		return
	}
	msg := message.Message{
		MsgID:     newMsgID(),
		Type:      msgTypeHandshake,
		From:      name,
		Origin:    app.selfAddr,
		AuthToken: app.identity.Token(),
		Timestamp: time.Now(),
	}
	app.cm.Broadcast(msg, "")
}

func (app *appContext) maybeNotify(msg message.Message) {
	self := app.identity.Get()
	if self == "" || strings.EqualFold(msg.From, self) {
		return
	}
	n := notificationPayload{
		ID:        msg.MsgID,
		From:      msg.From,
		Timestamp: time.Now(),
	}
	if msg.Type == msgTypeDM {
		if strings.EqualFold(msg.To, self) || strings.EqualFold(msg.ToAddr, app.selfAddr) {
			n.Level = "dm"
			n.Text = fmt.Sprintf("%s sent you a direct message", msg.From)
			app.sink.ShowNotification(n)
		}
		return
	}
	content := strings.ToLower(msg.Content)
	needle := strings.ToLower(self)
	if content != "" && strings.Contains(content, needle) {
		n.Level = "mention"
		n.Text = fmt.Sprintf("%s mentioned you", msg.From)
		app.sink.ShowNotification(n)
	}
}
