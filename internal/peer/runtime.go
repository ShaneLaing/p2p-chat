package peer

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"p2p-chat/internal/authutil"
	"p2p-chat/internal/message"
)

const (
	msgTypeChat      = "chat"
	msgTypeDM        = "dm"
	msgTypeAck       = "ack"
	msgTypePeerSync  = "peer_sync"
	msgTypeHandshake = "handshake"
)

func (a *App) readCLIInput() {
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
		a.processLine(line)
	}
}

func (a *App) processLine(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	if strings.HasPrefix(line, "/") {
		a.handleCommand(line)
		return
	}
	a.sendChatMessage(line)
}

func (a *App) runInboundHandler() {
	for {
		select {
		case <-a.ctx.Done():
			return
		case msg, ok := <-a.ConnMgr.Incoming:
			if !ok {
				return
			}
			a.processIncoming(msg)
		}
	}
}

func (a *App) processIncoming(msg message.Message) {
	if msg.MsgID == "" {
		msg.MsgID = newMsgID()
	}
	if a.Cache.Seen(msg.MsgID) {
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
			a.Ack.Confirm(msg.AckFor)
			a.Metrics.IncAck()
		}
		return
	case msgTypePeerSync:
		for _, peer := range msg.PeerList {
			a.Scheduler.Add(peer)
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
		a.Directory.Record(msg.From, msg.Origin)
		a.sink.UpdatePeers(a.Directory.Snapshot())
		return
	}

	a.Directory.Record(msg.From, msg.Origin)

	if a.Blocklist.Blocks(msg.From, msg.Origin) {
		return
	}

	if msg.ToAddr != "" && msg.ToAddr != a.SelfAddr {
		a.ConnMgr.Broadcast(msg, "")
		return
	}
	if msg.To != "" && !strings.EqualFold(msg.To, a.Identity.Get()) && msg.ToAddr == "" {
		a.ConnMgr.Broadcast(msg, "")
		return
	}

	a.History.Add(msg)
	if err := a.Store.Append(msg); err != nil {
		log.Printf("history append: %v", err)
	}
	a.Metrics.IncSeen()
	a.sink.ShowMessage(msg)
	a.maybeNotify(msg)
	a.sendAck(msg)
	a.ConnMgr.Broadcast(msg, "")
}

func (a *App) sendChatMessage(content string) {
	msg := message.Message{
		MsgID:     newMsgID(),
		Type:      msgTypeChat,
		From:      a.Identity.Get(),
		Origin:    a.SelfAddr,
		Content:   content,
		Timestamp: time.Now(),
	}
	a.Cache.Seen(msg.MsgID)
	a.History.Add(msg)
	if err := a.Store.Append(msg); err != nil {
		log.Printf("history append: %v", err)
	}
	a.Metrics.IncSent()
	a.sink.ShowMessage(msg)
	a.ConnMgr.Broadcast(msg, "")
	a.Ack.Track(msg)
	a.persistExternal(msg, "")
}

func (a *App) sendDirectMessage(target, content string) {
	addr, resolvedName, _ := a.Directory.Resolve(target)
	recipient := chooseName(target, resolvedName)
	msg := message.Message{
		MsgID:     newMsgID(),
		Type:      msgTypeDM,
		From:      a.Identity.Get(),
		Origin:    a.SelfAddr,
		To:        recipient,
		ToAddr:    addr,
		Content:   content,
		Timestamp: time.Now(),
	}
	a.Cache.Seen(msg.MsgID)
	a.History.Add(msg)
	if err := a.Store.Append(msg); err != nil {
		log.Printf("history append: %v", err)
	}
	a.Metrics.IncSent()
	a.sink.ShowMessage(msg)
	a.ConnMgr.Broadcast(msg, "")
	a.Ack.Track(msg)
	a.persistExternal(msg, recipient)
}

func chooseName(target, resolved string) string {
	if resolved != "" {
		return resolved
	}
	return target
}

func (a *App) persistExternal(msg message.Message, receiver string) {
	if a.Cfg.AuthAPI == "" {
		return
	}
	token := a.Identity.Token()
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
	url := strings.TrimRight(a.Cfg.AuthAPI, "/") + "/messages"
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

func (a *App) sendAck(original message.Message) {
	ackMsg := message.Message{
		MsgID:     newMsgID(),
		Type:      msgTypeAck,
		From:      a.Identity.Get(),
		Origin:    a.SelfAddr,
		To:        original.From,
		ToAddr:    original.Origin,
		AckFor:    original.MsgID,
		Timestamp: time.Now(),
	}
	a.ConnMgr.Broadcast(ackMsg, "")
}

func (a *App) handleCommand(line string) {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return
	}
	switch parts[0] {
	case "/peers":
		conns := a.ConnMgr.ConnsList()
		desired := a.Scheduler.Desired()
		a.sink.ShowSystem(fmt.Sprintf("connected: %v | desired: %v", conns, desired))
	case "/history":
		entries := a.History.All()
		for _, msg := range entries {
			a.sink.ShowMessage(msg)
		}
	case "/save":
		if len(parts) < 2 {
			a.sink.ShowSystem("usage: /save <path>")
			return
		}
		if err := saveHistoryToFile(a.History.All(), parts[1]); err != nil {
			a.sink.ShowSystem(fmt.Sprintf("save failed: %v", err))
			return
		}
		a.sink.ShowSystem("history saved")
	case "/load":
		limit := 20
		if len(parts) >= 2 {
			if v, err := strconv.Atoi(parts[1]); err == nil {
				limit = v
			}
		}
		records, err := a.Store.Recent(limit)
		if err != nil {
			a.sink.ShowSystem(fmt.Sprintf("load failed: %v", err))
			return
		}
		for i := len(records) - 1; i >= 0; i-- {
			a.sink.ShowMessage(records[i])
		}
	case "/msg":
		if len(parts) < 3 {
			a.sink.ShowSystem("usage: /msg <target> <message>")
			return
		}
		target := parts[1]
		idx := strings.Index(line, target)
		content := strings.TrimSpace(line[idx+len(target):])
		if content == "" {
			a.sink.ShowSystem("message required")
			return
		}
		a.sendDirectMessage(target, content)
	case "/nick":
		if len(parts) < 2 {
			a.sink.ShowSystem("usage: /nick <name>")
			return
		}
		if a.Identity.SetDisplay(parts[1]) {
			a.sink.ShowSystem(fmt.Sprintf("nickname set to %s", parts[1]))
			a.broadcastHandshake()
		}
	case "/stats":
		snap := a.Metrics.Snapshot()
		a.sink.ShowSystem(snap.String())
	case "/block":
		if len(parts) < 2 {
			a.sink.ShowSystem("usage: /block <name|addr>")
			return
		}
		a.Blocklist.Add(parts[1])
		a.sink.ShowSystem(fmt.Sprintf("blocked %s", parts[1]))
	case "/unblock":
		if len(parts) < 2 {
			a.sink.ShowSystem("usage: /unblock <name|addr>")
			return
		}
		a.Blocklist.Remove(parts[1])
		a.sink.ShowSystem(fmt.Sprintf("unblocked %s", parts[1]))
	case "/blocked":
		a.sink.ShowSystem(fmt.Sprintf("blocked: %v", a.Blocklist.List()))
	case "/quit":
		a.sink.ShowSystem("bye")
		os.Exit(0)
	default:
		a.sink.ShowSystem("commands: /peers /history /save /load /msg /nick /stats /block /unblock /blocked /quit")
	}
}

func (a *App) registerSelf() error {
	payload := map[string]string{"addr": a.SelfAddr}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(a.Cfg.BootstrapURL+"/register", "application/json", bytes.NewReader(body))
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

func (a *App) connectToBootstrapPeers() {
	peers, err := fetchPeers(a.Cfg.BootstrapURL)
	if err != nil {
		log.Printf("fetch peers: %v", err)
		return
	}
	for _, peer := range peers {
		if peer == a.SelfAddr {
			continue
		}
		a.Scheduler.Add(peer)
		if err := a.ConnMgr.ConnectToPeer(peer); err != nil {
			log.Printf("connect to %s: %v", peer, err)
		}
	}
}

func (a *App) runBootstrapLoop() {
	ticker := time.NewTicker(a.Cfg.PollEvery)
	defer ticker.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			peers, err := fetchPeers(a.Cfg.BootstrapURL)
			if err != nil {
				log.Printf("poll peers: %v", err)
				continue
			}
			for _, peer := range peers {
				if peer == a.SelfAddr {
					continue
				}
				a.Scheduler.Add(peer)
			}
		}
	}
}

func (a *App) runGossipLoop() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			peers := a.Scheduler.Desired()
			if len(peers) == 0 {
				continue
			}
			msg := message.Message{
				MsgID:     newMsgID(),
				Type:      msgTypePeerSync,
				From:      a.Identity.Get(),
				Origin:    a.SelfAddr,
				Timestamp: time.Now(),
				PeerList:  peers,
			}
			a.ConnMgr.Broadcast(msg, "")
		}
	}
}

func (a *App) runPeerListLoop() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			addrs := a.ConnMgr.ConnsList()
			a.Directory.MarkActive(addrs)
			a.sink.UpdatePeers(a.Directory.Snapshot())
		}
	}
}

func (a *App) runPresenceLoop() {
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.broadcastHandshake()
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

func (a *App) broadcastHandshake() {
	name := a.Identity.Get()
	if name == "" {
		return
	}
	msg := message.Message{
		MsgID:     newMsgID(),
		Type:      msgTypeHandshake,
		From:      name,
		Origin:    a.SelfAddr,
		AuthToken: a.Identity.Token(),
		Timestamp: time.Now(),
	}
	a.ConnMgr.Broadcast(msg, "")
}

func (a *App) maybeNotify(msg message.Message) {
	self := a.Identity.Get()
	if self == "" || strings.EqualFold(msg.From, self) {
		return
	}
	n := notificationPayload{
		ID:        msg.MsgID,
		From:      msg.From,
		Timestamp: time.Now(),
	}
	if msg.Type == msgTypeDM {
		if strings.EqualFold(msg.To, self) || strings.EqualFold(msg.ToAddr, a.SelfAddr) {
			n.Level = "dm"
			n.Text = fmt.Sprintf("%s sent you a direct message", msg.From)
			a.sink.ShowNotification(n)
		}
		return
	}
	content := strings.ToLower(msg.Content)
	needle := strings.ToLower(self)
	if content != "" && strings.Contains(content, needle) {
		n.Level = "mention"
		n.Text = fmt.Sprintf("%s mentioned you", msg.From)
		a.sink.ShowNotification(n)
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
