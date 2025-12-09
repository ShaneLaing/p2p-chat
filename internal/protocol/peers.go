package protocol

import (
	"sort"
	"strings"
	"sync"
	"time"

	"p2p-chat/internal/ui"
)

const presenceGrace = 20 * time.Second

// BlockList prevents unwanted peers from appearing locally.
type BlockList struct {
	mu      sync.RWMutex
	blocked map[string]struct{}
}

func NewBlockList() *BlockList {
	return &BlockList{blocked: make(map[string]struct{})}
}

func (b *BlockList) Add(token string) {
	if token == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.blocked[token] = struct{}{}
}

func (b *BlockList) Remove(token string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.blocked, token)
}

func (b *BlockList) Blocks(name, addr string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if _, ok := b.blocked[name]; ok {
		return true
	}
	if _, ok := b.blocked[addr]; ok {
		return true
	}
	return false
}

func (b *BlockList) List() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]string, 0, len(b.blocked))
	for key := range b.blocked {
		out = append(out, key)
	}
	return out
}

type peerEntry struct {
	Name     string
	Addr     string
	Online   bool
	LastSeen time.Time
}

// PeerDirectory tracks known peers and their presence info.
type PeerDirectory struct {
	mu     sync.RWMutex
	byName map[string]*peerEntry
	byAddr map[string]*peerEntry
}

func NewPeerDirectory() *PeerDirectory {
	return &PeerDirectory{
		byName: make(map[string]*peerEntry),
		byAddr: make(map[string]*peerEntry),
	}
}

func (p *PeerDirectory) Record(name, addr string) {
	if addr == "" {
		return
	}
	if name == "" {
		name = addr
	}
	key := strings.ToLower(name)
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok := p.byAddr[addr]
	if !ok {
		entry = &peerEntry{Addr: addr}
		p.byAddr[addr] = entry
	}
	entry.Name = name
	entry.Addr = addr
	entry.Online = true
	entry.LastSeen = now
	p.byName[key] = entry
}

func (p *PeerDirectory) MarkActive(addrs []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	for _, addr := range addrs {
		if entry, ok := p.byAddr[addr]; ok {
			entry.Online = true
			entry.LastSeen = now
		}
	}
	for _, entry := range p.byAddr {
		if now.Sub(entry.LastSeen) > presenceGrace {
			entry.Online = false
		}
	}
}

func (p *PeerDirectory) Resolve(token string) (addr string, name string, ok bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if entry, ok := p.byAddr[token]; ok {
		return entry.Addr, entry.Name, true
	}
	if entry, ok := p.byName[strings.ToLower(token)]; ok {
		return entry.Addr, entry.Name, true
	}
	return "", "", false
}

func (p *PeerDirectory) Snapshot() []ui.Presence {
	p.mu.RLock()
	defer p.mu.RUnlock()
	list := make([]ui.Presence, 0, len(p.byAddr))
	for _, entry := range p.byAddr {
		list = append(list, ui.Presence{
			Name:   entry.Name,
			Addr:   entry.Addr,
			Online: entry.Online,
		})
	}
	sort.Slice(list, func(i, j int) bool {
		return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
	})
	return list
}
