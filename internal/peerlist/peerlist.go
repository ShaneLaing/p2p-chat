package peerlist

import (
	"sort"
	"sync"
	"time"
)

// Store keeps track of live peers registering with the bootstrap server.
type Store struct {
	mu       sync.Mutex
	peers    map[string]time.Time
	expireIn time.Duration
}

// NewStore creates a peer list store with a given expiry window.
func NewStore(expireIn time.Duration) *Store {
	return &Store{
		peers:    make(map[string]time.Time),
		expireIn: expireIn,
	}
}

// Register upserts a peer address.
func (s *Store) Register(addr string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.peers[addr] = time.Now()
}

// List returns all non-expired peers.
func (s *Store) List() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneExpired()
	addrs := make([]string, 0, len(s.peers))
	for addr := range s.peers {
		addrs = append(addrs, addr)
	}
	sort.Strings(addrs)
	return addrs
}

func (s *Store) pruneExpired() {
	if s.expireIn <= 0 {
		return
	}
	deadline := time.Now().Add(-s.expireIn)
	for addr, ts := range s.peers {
		if ts.Before(deadline) {
			delete(s.peers, addr)
		}
	}
}
