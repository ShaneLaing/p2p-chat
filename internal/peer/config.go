package peer

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultHistoryDBPath = "p2p-chat-history.db"
	defaultFilesDirPath  = "p2p-files"
	defaultFilesDBPath   = "p2p-files.db"
)

// Config holds peer runtime settings derived from CLI flags.
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
	UseTUI       bool
	UseCLI       bool
	UseWeb       bool
	WebAddr      string
	HistoryDB    string
	FilesDir     string
	FilesDB      string
	DataDir      string
	PeerDir      string
	AuthAPI      string
}

// LoadConfig parses CLI flags and returns a populated Config.
func LoadConfig() *Config {
	cfg := &Config{}

	flag.StringVar(&cfg.BootstrapURL, "bootstrap", "http://127.0.0.1:8000", "bootstrap base url")
	flag.StringVar(&cfg.ListenAddr, "listen", "", "address to listen on (host:port)")
	flag.IntVar(&cfg.Port, "port", 9001, "port to listen on when --listen empty")
	flag.StringVar(&cfg.Nick, "nick", "", "nickname displayed in chat")
	flag.StringVar(&cfg.Username, "username", "", "authenticated username (overrides --nick)")
	flag.StringVar(&cfg.Token, "token", "", "JWT token for authenticated username")
	flag.StringVar(&cfg.Secret, "secret", "", "shared secret for AES-256 encryption")
	flag.DurationVar(&cfg.PollEvery, "poll", 5*time.Second, "interval to refresh peers list")
	flag.IntVar(&cfg.HistorySize, "history", 200, "amount of messages kept locally")
	flag.BoolVar(&cfg.NoColor, "no-color", false, "disable ANSI colors in CLI output")
	flag.BoolVar(&cfg.UseTUI, "tui", false, "enable terminal UI mode")
	flag.BoolVar(&cfg.UseWeb, "web", false, "serve local web UI")
	flag.StringVar(&cfg.WebAddr, "web-addr", "127.0.0.1:8081", "address for embedded web UI server")
	flag.StringVar(&cfg.HistoryDB, "history-db", defaultHistoryDBPath, "path to persisted chat history db")
	flag.StringVar(&cfg.FilesDir, "files-dir", defaultFilesDirPath, "directory to store uploaded files")
	flag.StringVar(&cfg.FilesDB, "files-db", defaultFilesDBPath, "path to persisted file metadata db")
	flag.StringVar(&cfg.DataDir, "data-dir", "p2p-data", "base directory for auto-generated peer data (history/files)")
	flag.StringVar(&cfg.AuthAPI, "auth-api", "http://127.0.0.1:8089", "authentication server base url")

	flag.Parse()

	if cfg.ListenAddr == "" {
		cfg.ListenAddr = fmt.Sprintf("127.0.0.1:%d", cfg.Port)
	}
	cfg.UseCLI = !cfg.UseTUI

	cfg.ensureDirs()
	return cfg
}

func (cfg *Config) ensureDirs() {
	if cfg.DataDir == "" {
		cfg.DataDir = "p2p-data"
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		log.Fatalf("init data dir: %v", err)
	}
	cfg.PeerDir = derivePeerDir(cfg.DataDir, cfg.ListenAddr)
	if err := os.MkdirAll(cfg.PeerDir, 0o755); err != nil {
		log.Fatalf("prepare peer dir: %v", err)
	}
	if cfg.HistoryDB == defaultHistoryDBPath {
		cfg.HistoryDB = filepath.Join(cfg.PeerDir, "history.db")
	}
	if cfg.FilesDB == defaultFilesDBPath {
		cfg.FilesDB = filepath.Join(cfg.PeerDir, "files.db")
	}
	if cfg.FilesDir == defaultFilesDirPath {
		cfg.FilesDir = filepath.Join(cfg.PeerDir, "files")
	}
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
