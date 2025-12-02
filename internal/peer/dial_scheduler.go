package peer

import (
	"context"
	"log"
	"math/rand"
	"sync"
	"time"

	"p2p-chat/internal/network"
)

// dialScheduler manages dialing peers with retries and backoff.
const (
	dialQueueSize   = 128
	dialBackoff     = 5 * time.Second
	dialJitterRange = 2 * time.Second
)

type dialScheduler struct {
	cm       *network.ConnManager
	selfAddr string

	mu      sync.RWMutex
	desired map[string]time.Time

	queue chan string
	quit  chan struct{}
}

func newDialScheduler(cm *network.ConnManager, self string) *dialScheduler {
	return &dialScheduler{
		cm:       cm,
		selfAddr: self,
		desired:  make(map[string]time.Time),
		queue:    make(chan string, dialQueueSize),
		quit:     make(chan struct{}),
	}
}

func (d *dialScheduler) Add(addr string) {
	if addr == "" || addr == d.selfAddr {
		return
	}
	d.mu.Lock()
	if _, exists := d.desired[addr]; !exists {
		d.desired[addr] = time.Now()
		d.enqueue(addr)
	}
	d.mu.Unlock()
}

func (d *dialScheduler) Desired() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	list := make([]string, 0, len(d.desired))
	for addr := range d.desired {
		list = append(list, addr)
	}
	return list
}

func (d *dialScheduler) enqueue(addr string) {
	select {
	case d.queue <- addr:
	default:
		log.Printf("dial queue full, dropping %s", addr)
	}
}

func (d *dialScheduler) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.quit:
			return
		case addr := <-d.queue:
			d.tryDial(addr)
		}
	}
}

func (d *dialScheduler) tryDial(addr string) {
	if err := d.cm.ConnectToPeer(addr); err != nil {
		log.Printf("dial %s failed: %v", addr, err)
		d.scheduleRetry(addr)
		return
	}
	d.mu.Lock()
	delete(d.desired, addr)
	d.mu.Unlock()
}

func (d *dialScheduler) scheduleRetry(addr string) {
	go func() {
		jitter := time.Duration(rand.Int63n(int64(dialJitterRange)))
		time.Sleep(dialBackoff + jitter)
		d.enqueue(addr)
	}()
}

func (d *dialScheduler) Close() {
	close(d.quit)
}
