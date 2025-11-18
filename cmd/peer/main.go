package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"p2p-chat/internal/authutil"
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

	defaultHistoryDBPath = "p2p-chat-history.db"
	defaultFilesDirPath  = "p2p-files"
	defaultFilesDBPath   = "p2p-files.db"
)

var (
	bootstrapURL = flag.String("bootstrap", "http://127.0.0.1:8000", "bootstrap base url")
	listenAddr   = flag.String("listen", "", "address to listen on (host:port)")
	port         = flag.Int("port", 9001, "port to listen on when --listen empty")
	nick         = flag.String("nick", "", "nickname displayed in chat")
	usernameFlag = flag.String("username", "", "authenticated username (overrides --nick)")
	tokenFlag    = flag.String("token", "", "JWT token for authenticated username")
	secret       = flag.String("secret", "", "shared secret for AES-256 encryption")
	pollEvery    = flag.Duration("poll", 5*time.Second, "interval to refresh peers list")
	historySize  = flag.Int("history", 200, "amount of messages kept locally")
	noColor      = flag.Bool("no-color", false, "disable ANSI colors in CLI output")
	enableTUI    = flag.Bool("tui", false, "enable terminal UI mode")
	enableWeb    = flag.Bool("web", false, "serve local web UI")
	webAddr      = flag.String("web-addr", "127.0.0.1:8081", "address for embedded web UI server")
	historyDB    = flag.String("history-db", defaultHistoryDBPath, "path to persisted chat history db")
	filesDir     = flag.String("files-dir", defaultFilesDirPath, "directory to store uploaded files")
	filesDB      = flag.String("files-db", defaultFilesDBPath, "path to persisted file metadata db")
	dataDir      = flag.String("data-dir", "p2p-data", "base directory for auto-generated peer data (history/files)")
	authAPI      = flag.String("auth-api", "http://127.0.0.1:8089", "authentication server base url")
)

func main() {
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addr := *listenAddr
	if addr == "" {
		addr = fmt.Sprintf("127.0.0.1:%d", *port)
	}

	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		log.Fatalf("init data dir: %v", err)
	}
	peerDir := derivePeerDir(*dataDir, addr)
	if err := os.MkdirAll(peerDir, 0o755); err != nil {
		log.Fatalf("prepare peer dir: %v", err)
	}
	if *historyDB == defaultHistoryDBPath {
		*historyDB = filepath.Join(peerDir, "history.db")
	}
	if *filesDB == defaultFilesDBPath {
		*filesDB = filepath.Join(peerDir, "files.db")
	}
	if *filesDir == defaultFilesDirPath {
		*filesDir = filepath.Join(peerDir, "files")
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

	cache := newMsgCache(10 * time.Minute)
	history := newHistory(*historySize)

	store, err := openHistoryStore(*historyDB)
	if err != nil {
		log.Printf("history db unavailable (%v), running without persistence", err)
	} else {
		defer store.Close()
	}

	var files *fileStore
	if *enableWeb {
		files, err = openFileStore(*filesDB, *filesDir)
		if err != nil {
			log.Fatalf("file store: %v", err)
		}
		defer files.Close()
	}

	identity := newIdentity(*nick, addr)
	if *usernameFlag != "" && *tokenFlag != "" {
		identity.SetAuth(*usernameFlag, *tokenFlag)
	}
	blocklist := newBlockList()
	directory := newPeerDirectory()
	metrics := newMetrics()
	dialer := newDialScheduler(cm, addr)
	ack := newAckTracker(cm)
	defer ack.Stop()

	app := &appContext{
		ctx:       ctx,
		cm:        cm,
		cache:     cache,
		history:   history,
		store:     store,
		files:     files,
		blocklist: blocklist,
		directory: directory,
		metrics:   metrics,
		ack:       ack,
		dialer:    dialer,
		identity:  identity,
		selfAddr:  addr,
	}

	if name := identity.Get(); name != "" {
		directory.Record(name, addr)
	}

	sinks := []displaySink{}
	cliSink := newCLIDisplay(shouldUseColor(*noColor))
	if !*enableTUI {
		sinks = append(sinks, cliSink)
	}

	var tuiSink *tuiDisplay
	if *enableTUI {
		tuiSink = newTUIDisplay(func(line string) { processLine(app, line) })
		sinks = append(sinks, tuiSink)
		go func() {
			if err := tuiSink.Run(ctx); err != nil {
				log.Printf("tui error: %v", err)
			}
		}()
	}

	var webSink *webBridge
	if *enableWeb {
		setter := func(user, token string) error {
			if app.identity.SetAuth(user, token) {
				app.sink.ShowSystem(fmt.Sprintf("logged in as %s", user))
				broadcastHandshake(app)
			}
			return nil
		}
		wb, err := newWebBridge(*webAddr, history, func(line string) { processLine(app, line) }, setter, files)
		if err != nil {
			log.Fatalf("web ui: %v", err)
		}
		webSink = wb
		sinks = append(sinks, wb)
		app.web = wb
		go wb.Run(ctx)
	}

	app.sink = newMultiSink(sinks...)

	if err := registerSelf(*bootstrapURL, addr); err != nil {
		log.Printf("register failed: %v", err)
	}
	connectToBootstrapPeers(app)
	broadcastHandshake(app)

	go dialer.Run(ctx)
	go handleIncoming(app)
	go pollBootstrapLoop(app)
	go gossipLoop(app)
	go updatePeerListLoop(app)

	if !*enableTUI {
		go readCLIInput(app)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	cancel()
	log.Println("shutting down...")
	if webSink != nil {
		webSink.Close()
	}
	dialer.Close()
	cm.Stop()
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

func readCLIInput(app *appContext) {
	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return
			}
			log.Printf("stdin err: %v", err)
			return
		}
		processLine(app, line)
	}
}

func processLine(app *appContext, line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	if strings.HasPrefix(line, "/") {
		handleCommand(app, line)
		return
	}
	sendChatMessage(app, line)
}

func handleIncoming(app *appContext) {
	for {
		select {
		case <-app.ctx.Done():
			return
		case msg, ok := <-app.cm.Incoming:
			if !ok {
				return
			}
			app.processIncoming(msg)
		}
	}
}

func (app *appContext) processIncoming(msg message.Message) {
	if msg.MsgID == "" {
		msg.MsgID = newMsgID()
	}
	if app.cache.Seen(msg.MsgID) {
		return
	}
	if msg.Origin == "" {
		msg.Origin = msg.From
	}
	if msg.Type == "" {
		msg.Type = msgTypeChat
	}

	switch msg.Type {
	case msgTypeAck:
		if msg.AckFor != "" {
			app.ack.Confirm(msg.AckFor)
			app.metrics.IncAck()
		}
		return
	case msgTypePeerSync:
		for _, peer := range msg.PeerList {
			app.dialer.Add(peer)
		}
		return
	case msgTypeHandshake:
		if msg.AuthToken != "" {
			username, err := authutil.ValidateToken(msg.AuthToken)
			if err != nil || !strings.EqualFold(username, msg.From) {
				log.Printf("handshake rejected from %s: %v", msg.Origin, err)
				return
			}
		}
		app.directory.Record(msg.From, msg.Origin)
		app.sink.UpdatePeers(app.directory.Snapshot())
		return
	}

	app.directory.Record(msg.From, msg.Origin)

	if app.blocklist.Blocks(msg.From, msg.Origin) {
		return
	}

	if msg.ToAddr != "" && msg.ToAddr != app.selfAddr {
		app.cm.Broadcast(msg, "")
		return
	}
	if msg.To != "" && !strings.EqualFold(msg.To, app.identity.Get()) && msg.ToAddr == "" {
		app.cm.Broadcast(msg, "")
		return
	}

	app.history.Add(msg)
	if err := app.store.Append(msg); err != nil {
		log.Printf("history append: %v", err)
	}
	app.metrics.IncSeen()
	app.sink.ShowMessage(msg)
	app.maybeNotify(msg)
	sendAck(app, msg)
	app.cm.Broadcast(msg, "")
}

func sendChatMessage(app *appContext, content string) {
	msg := message.Message{
		MsgID:     newMsgID(),
		Type:      msgTypeChat,
		From:      app.identity.Get(),
		Origin:    app.selfAddr,
		Content:   content,
		Timestamp: time.Now(),
	}
	app.cache.Seen(msg.MsgID)
	app.history.Add(msg)
	if err := app.store.Append(msg); err != nil {
		log.Printf("history append: %v", err)
	}
	app.metrics.IncSent()
	app.sink.ShowMessage(msg)
	app.cm.Broadcast(msg, "")
	app.ack.Track(msg)
	app.persistExternal(msg, "")
}

func sendDirectMessage(app *appContext, target, content string) {
	addr, resolvedName, _ := app.directory.Resolve(target)
	recipient := chooseName(target, resolvedName)
	msg := message.Message{
		MsgID:     newMsgID(),
		Type:      msgTypeDM,
		From:      app.identity.Get(),
		Origin:    app.selfAddr,
		To:        recipient,
		ToAddr:    addr,
		Content:   content,
		Timestamp: time.Now(),
	}
	app.cache.Seen(msg.MsgID)
	app.history.Add(msg)
	if err := app.store.Append(msg); err != nil {
		log.Printf("history append: %v", err)
	}
	app.metrics.IncSent()
	app.sink.ShowMessage(msg)
	app.cm.Broadcast(msg, "")
	app.ack.Track(msg)
	app.persistExternal(msg, recipient)
}

func chooseName(target, resolved string) string {
	if resolved != "" {
		return resolved
	}
	return target
}

func (app *appContext) persistExternal(msg message.Message, receiver string) {
	if authAPI == nil || *authAPI == "" {
		return
	}
	token := app.identity.Token()
	if token == "" {
		return
	}
	payload := map[string]interface{}{
		"sender":  msg.From,
		"content": msg.Content,
	}
	if receiver != "" {
		payload["receiver"] = receiver
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	url := strings.TrimRight(*authAPI, "/") + "/messages"
	go func(endpoint string, data []byte, tok string) {
		req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(data))
		if err != nil {
			return
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("auth store: %v", err)
			return
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}(url, body, token)
}

func sendAck(app *appContext, original message.Message) {
	ackMsg := message.Message{
		MsgID:     newMsgID(),
		Type:      msgTypeAck,
		From:      app.identity.Get(),
		Origin:    app.selfAddr,
		To:        original.From,
		ToAddr:    original.Origin,
		AckFor:    original.MsgID,
		Timestamp: time.Now(),
	}
	app.cm.Broadcast(ackMsg, "")
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
		entries := app.history.All()
		for _, msg := range entries {
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
	case "/nick":
		if len(parts) < 2 {
			app.sink.ShowSystem("usage: /nick <name>")
			return
		}
		if app.identity.SetDisplay(parts[1]) {
			app.sink.ShowSystem(fmt.Sprintf("nickname set to %s", parts[1]))
			broadcastHandshake(app)
		}
		return
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
		app.sink.ShowSystem("commands: /peers /history /save /load /msg /nick /stats /block /unblock /blocked /quit")
	}
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

func connectToBootstrapPeers(app *appContext) {
	peers, err := fetchPeers(*bootstrapURL)
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
	ticker := time.NewTicker(*pollEvery)
	defer ticker.Stop()
	for {
		select {
		case <-app.ctx.Done():
			return
		case <-ticker.C:
			peers, err := fetchPeers(*bootstrapURL)
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

func saveHistoryToFile(entries []message.Message, path string) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// appContext aggregates peer state shared across goroutines.
type appContext struct {
	ctx       context.Context
	cm        *network.ConnManager
	cache     *msgCache
	history   *historyBuffer
	store     *historyStore
	files     *fileStore
	blocklist *blockList
	directory *peerDirectory
	metrics   *metrics
	ack       *ackTracker
	dialer    *dialScheduler
	sink      displaySink
	identity  *identity
	selfAddr  string
	web       *webBridge
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
	addr  string
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
	if name == "" {
		return false
	}
	if i.name == name {
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
