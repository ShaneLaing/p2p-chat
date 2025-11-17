package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"p2p-chat/internal/crypto"
	"p2p-chat/internal/message"
	"p2p-chat/internal/network"
)

var (
	bootstrapURL = flag.String("bootstrap", "http://127.0.0.1:8000", "bootstrap base url")
	listenAddr   = flag.String("listen", "", "address to listen on (host:port)")
	port         = flag.Int("port", 9001, "port to listen on when --listen empty")
	nick         = flag.String("nick", "", "nickname displayed in chat")
	secret       = flag.String("secret", "", "shared secret for AES-256 encryption")
	pollEvery    = flag.Duration("poll", 5*time.Second, "interval to refresh peers list")
	historySize  = flag.Int("history", 200, "amount of messages kept locally")
	noColor      = flag.Bool("no-color", false, "disable ANSI colors in CLI output")
)

const (
	ansiReset = "\x1b[0m"
	ansiTime  = "\x1b[36m"
	ansiName  = "\x1b[33m"
)

func main() {
	flag.Parse()
	useColor := shouldUseColor(*noColor)

	addr := *listenAddr
	if addr == "" {
		addr = fmt.Sprintf("127.0.0.1:%d", *port)
	}

	box, err := crypto.NewBox(*secret)
	if err != nil {
		log.Fatalf("init encryption: %v", err)
	}

	cm := network.NewConnManager(addr, box)
	if err := cm.StartListen(); err != nil {
		log.Fatalf("listen failed: %v", err)
	}
	log.Printf("peer listening on %s (encryption:%t)", addr, cm.EncryptionEnabled())

	displayName := *nick
	if displayName == "" {
		displayName = addr
	}

	if err := registerSelf(*bootstrapURL, addr); err != nil {
		log.Printf("register failed: %v", err)
	}
	connectToBootstrapPeers(cm, addr)

	cache := newMsgCache(10 * time.Minute)
	history := newHistory(*historySize)
	quit := make(chan struct{})

	go handleIncoming(cm, cache, history, useColor)
	go pollBootstrap(cm, addr, quit)
	go cliLoop(cm, displayName, cache, history, useColor)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	close(quit)
	log.Println("shutting down...")
	cm.Stop()
}

func registerSelf(url, addr string) error {
	payload := map[string]string{"addr": addr}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(url+"/register", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return nil
}

func fetchPeers(url string) ([]string, error) {
	resp, err := http.Get(url + "/peers")
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

func connectToBootstrapPeers(cm *network.ConnManager, self string) {
	peers, err := fetchPeers(*bootstrapURL)
	if err != nil {
		log.Printf("fetch peers: %v", err)
		return
	}
	for _, peer := range peers {
		if peer == self {
			continue
		}
		if err := cm.ConnectToPeer(peer); err != nil {
			log.Printf("connect to %s: %v", peer, err)
		} else {
			log.Printf("connected to %s", peer)
		}
	}
}

func pollBootstrap(cm *network.ConnManager, self string, quit <-chan struct{}) {
	ticker := time.NewTicker(*pollEvery)
	defer ticker.Stop()
	for {
		select {
		case <-quit:
			return
		case <-ticker.C:
			peers, err := fetchPeers(*bootstrapURL)
			if err != nil {
				log.Printf("poll peers: %v", err)
				continue
			}
			for _, peer := range peers {
				if peer == self {
					continue
				}
				if err := cm.ConnectToPeer(peer); err == nil {
					log.Printf("connected to %s", peer)
				}
			}
		}
	}
}

func handleIncoming(cm *network.ConnManager, cache *msgCache, history *historyBuffer, color bool) {
	for msg := range cm.Incoming {
		if msg.MsgID == "" {
			msg.MsgID = newMsgID()
		}
		if cache.Seen(msg.MsgID) {
			continue
		}
		history.Add(msg)
		printChatLine(msg, color)
		cm.Broadcast(msg, "")
	}
}

func cliLoop(cm *network.ConnManager, display string, cache *msgCache, history *historyBuffer, color bool) {
	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			log.Printf("stdin err: %v", err)
			return
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "/") {
			handleCommand(line, cm, history, color)
			continue
		}
		msg := message.Message{
			MsgID:     newMsgID(),
			From:      display,
			Content:   line,
			Timestamp: time.Now(),
		}
		cache.Seen(msg.MsgID)
		history.Add(msg)
		cm.Broadcast(msg, "")
	}
}

func handleCommand(cmd string, cm *network.ConnManager, history *historyBuffer, color bool) {
	switch cmd {
	case "/peers":
		peers := cm.ConnsList()
		if len(peers) == 0 {
			fmt.Println("no active peers")
			return
		}
		fmt.Println("connected peers:")
		for _, p := range peers {
			fmt.Println(" -", p)
		}
	case "/history":
		entries := history.All()
		for _, msg := range entries {
			printHistoryLine(msg, color)
		}
	case "/quit":
		fmt.Println("bye")
		cm.Stop()
		os.Exit(0)
	default:
		fmt.Println("commands: /peers /history /quit")
	}
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

func printChatLine(msg message.Message, color bool) {
	ts := msg.Timestamp.Format("15:04:05")
	if color {
		fmt.Printf("%s[%s]%s %s%s%s: %s\n", ansiTime, ts, ansiReset, ansiName, msg.From, ansiReset, msg.Content)
		return
	}
	fmt.Printf("[%s] %s: %s\n", ts, msg.From, msg.Content)
}

func printHistoryLine(msg message.Message, color bool) {
	ts := msg.Timestamp.Format("01-02 15:04:05")
	if color {
		fmt.Printf("%s[%s]%s %s%s%s: %s\n", ansiTime, ts, ansiReset, ansiName, msg.From, ansiReset, msg.Content)
		return
	}
	fmt.Printf("[%s] %s: %s\n", ts, msg.From, msg.Content)
}

func shouldUseColor(disable bool) bool {
	if disable {
		return false
	}
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	if runtime.GOOS == "windows" {
		if os.Getenv("WT_SESSION") != "" || os.Getenv("ANSICON") != "" || strings.EqualFold(os.Getenv("ConEmuANSI"), "ON") {
			return true
		}
		return false
	}
	return true
}

func newMsgID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", b)
}
