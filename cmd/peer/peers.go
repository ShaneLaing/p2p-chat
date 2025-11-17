package main

import (
	"strings"
	"sync"
)

type blockList struct {
	mu      sync.RWMutex
	blocked map[string]struct{}
}

func newBlockList() *blockList {
	return &blockList{blocked: make(map[string]struct{})}
}

func (b *blockList) Add(token string) {
	if token == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.blocked[token] = struct{}{}
}

func (b *blockList) Remove(token string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.blocked, token)
}

func (b *blockList) Blocks(name, addr string) bool {
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

func (b *blockList) List() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]string, 0, len(b.blocked))
	for key := range b.blocked {
		out = append(out, key)
	}
	return out
}

type peerDirectory struct {
	mu     sync.RWMutex
	byName map[string]string
	byAddr map[string]string
}

func newPeerDirectory() *peerDirectory {
	return &peerDirectory{
		byName: make(map[string]string),
		byAddr: make(map[string]string),
	}
}

func (p *peerDirectory) Record(name, addr string) {
	if addr == "" {
		return
	}
	if name == "" {
		name = addr
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.byName[strings.ToLower(name)] = addr
	p.byAddr[addr] = name
}

func (p *peerDirectory) Resolve(token string) (string, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if _, ok := p.byAddr[token]; ok {
		return token, true
	}
	addr, ok := p.byName[strings.ToLower(token)]
	return addr, ok
}
