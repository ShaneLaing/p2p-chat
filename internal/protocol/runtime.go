package protocol

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync"
	"time"

	"p2p-chat/internal/message"
	"p2p-chat/internal/network"
	"p2p-chat/internal/storage"
	"p2p-chat/internal/ui"
)

// Runtime aggregates the long-lived state and collaborators used by the
// protocol layer.
type Runtime struct {
	ctx          context.Context
	cm           *network.ConnManager
	cache        *MsgCache
	history      *HistoryBuffer
	store        *storage.HistoryStore
	files        *storage.FileStore
	blocklist    *BlockList
	directory    *PeerDirectory
	metrics      *Metrics
	ack          *AckTracker
	dialer       *DialScheduler
	sink         ui.Sink
	identity     *Identity
	selfAddr     string
	web          *ui.WebBridge
	bootstrapURL string
	pollInterval time.Duration
	authAPI      string
}

// RuntimeOptions describes the dependencies needed to construct Runtime.
type RuntimeOptions struct {
	ConnManager  *network.ConnManager
	CacheTTL     time.Duration
	HistorySize  int
	Store        *storage.HistoryStore
	Files        *storage.FileStore
	Blocklist    *BlockList
	Directory    *PeerDirectory
	Metrics      *Metrics
	Ack          *AckTracker
	Dialer       *DialScheduler
	Sink         ui.Sink
	Identity     *Identity
	SelfAddr     string
	Web          *ui.WebBridge
	BootstrapURL string
	PollInterval time.Duration
	AuthAPI      string
}

func NewRuntime(ctx context.Context, opts RuntimeOptions) *Runtime {
	cache := opts.CacheTTL
	if cache <= 0 {
		cache = 10 * time.Minute
	}
	historySize := opts.HistorySize
	if historySize <= 0 {
		historySize = 200
	}
	rt := &Runtime{
		ctx:          ctx,
		cm:           opts.ConnManager,
		cache:        NewMsgCache(cache),
		history:      NewHistoryBuffer(historySize),
		store:        opts.Store,
		files:        opts.Files,
		blocklist:    opts.Blocklist,
		directory:    opts.Directory,
		metrics:      opts.Metrics,
		ack:          opts.Ack,
		dialer:       opts.Dialer,
		sink:         opts.Sink,
		identity:     opts.Identity,
		selfAddr:     opts.SelfAddr,
		web:          opts.Web,
		bootstrapURL: opts.BootstrapURL,
		pollInterval: opts.PollInterval,
		authAPI:      opts.AuthAPI,
	}
	return rt
}

func (r *Runtime) Context() context.Context          { return r.ctx }
func (r *Runtime) ConnManager() *network.ConnManager { return r.cm }
func (r *Runtime) Cache() *MsgCache                  { return r.cache }
func (r *Runtime) History() *HistoryBuffer           { return r.history }
func (r *Runtime) Store() *storage.HistoryStore      { return r.store }
func (r *Runtime) Files() *storage.FileStore         { return r.files }
func (r *Runtime) Blocklist() *BlockList             { return r.blocklist }
func (r *Runtime) Directory() *PeerDirectory         { return r.directory }
func (r *Runtime) Metrics() *Metrics                 { return r.metrics }
func (r *Runtime) AckTracker() *AckTracker           { return r.ack }
func (r *Runtime) Dialer() *DialScheduler            { return r.dialer }
func (r *Runtime) Sink() ui.Sink                     { return r.sink }
func (r *Runtime) SetSink(s ui.Sink)                 { r.sink = s }
func (r *Runtime) Identity() *Identity               { return r.identity }
func (r *Runtime) SelfAddr() string                  { return r.selfAddr }
func (r *Runtime) Web() *ui.WebBridge                { return r.web }
func (r *Runtime) SetWeb(w *ui.WebBridge)            { r.web = w }
func (r *Runtime) BootstrapURL() string              { return r.bootstrapURL }
func (r *Runtime) PollInterval() time.Duration       { return r.pollInterval }
func (r *Runtime) AuthAPI() string                   { return r.authAPI }

// MsgCache tracks recently seen message IDs to drop duplicates.
type MsgCache struct {
	mu   sync.Mutex
	seen map[string]time.Time
	ttl  time.Duration
}

func NewMsgCache(ttl time.Duration) *MsgCache {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &MsgCache{seen: make(map[string]time.Time), ttl: ttl}
}

func (m *MsgCache) Seen(id string) bool {
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

// HistoryBuffer keeps a sliding window of recent chat messages in memory.
type HistoryBuffer struct {
	mu     sync.Mutex
	max    int
	buffer []message.Message
}

func NewHistoryBuffer(max int) *HistoryBuffer {
	if max <= 0 {
		max = 50
	}
	return &HistoryBuffer{max: max}
}

func (h *HistoryBuffer) Add(msg message.Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.buffer = append(h.buffer, msg)
	if len(h.buffer) > h.max {
		h.buffer = h.buffer[len(h.buffer)-h.max:]
	}
}

func (h *HistoryBuffer) All() []message.Message {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]message.Message, len(h.buffer))
	copy(out, h.buffer)
	return out
}

// Identity tracks the current nickname and auth token.
type Identity struct {
	mu    sync.RWMutex
	name  string
	token string
}

func NewIdentity(initial, fallback string) *Identity {
	if initial == "" {
		initial = fallback
	}
	return &Identity{name: initial}
}

func (i *Identity) Get() string {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.name
}

func (i *Identity) Token() string {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.token
}

func (i *Identity) SetDisplay(name string) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	if name == "" || i.name == name {
		return false
	}
	i.name = name
	return true
}

func (i *Identity) SetAuth(name, token string) bool {
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

// NewMsgID produces a random hex identifier for outbound messages.
func NewMsgID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", b)
}
