package protocol

import (
	"context"
	"log"
	"math/rand"
	"sync"
	"time"
)

const dialQueueSize = 128

var (
	dialBackoff     = 5 * time.Second
	dialJitterRange = 2 * time.Second
	randSrc         = rand.New(rand.NewSource(time.Now().UnixNano()))
	randMu          sync.Mutex
)

type peerConnector interface {
	ConnectToPeer(string) error
}

// DialScheduler manages peer dialing with retries and jitter.
type DialScheduler struct {
	cm       peerConnector
	selfAddr string

	mu      sync.RWMutex
	desired map[string]time.Time

	queue chan string
	quit  chan struct{}
}

func NewDialScheduler(cm peerConnector, self string) *DialScheduler {
	return &DialScheduler{
		cm:       cm,
		selfAddr: self,
		desired:  make(map[string]time.Time),
		queue:    make(chan string, dialQueueSize),
		quit:     make(chan struct{}),
	}
}

func (d *DialScheduler) Add(addr string) {
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

func (d *DialScheduler) Desired() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	list := make([]string, 0, len(d.desired))
	for addr := range d.desired {
		list = append(list, addr)
	}
	return list
}

func (d *DialScheduler) enqueue(addr string) {
	select {
	case d.queue <- addr:
	default:
		log.Printf("dial queue full, dropping %s", addr)
	}
}

func (d *DialScheduler) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.quit:
			return
		case addr := <-d.queue:
			d.tryDial(ctx, addr)
		}
	}
}

func (d *DialScheduler) tryDial(ctx context.Context, addr string) {
	if err := d.cm.ConnectToPeer(addr); err != nil {
		log.Printf("dial %s failed: %v", addr, err)
		d.scheduleRetry(ctx, addr)
		return
	}
	d.mu.Lock()
	_, stillDesired := d.desired[addr]
	if stillDesired {
		d.desired[addr] = time.Now()
	}
	d.mu.Unlock()
	if stillDesired {
		d.scheduleRetry(ctx, addr)
	}
}

func (d *DialScheduler) scheduleRetry(ctx context.Context, addr string) {
	go func() {
		var jitter time.Duration
		if dialJitterRange > 0 {
			randMu.Lock()
			jitter = time.Duration(randSrc.Int63n(int64(dialJitterRange)))
			randMu.Unlock()
		}
		delay := dialBackoff + jitter
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-d.quit:
			return
		case <-timer.C:
		}
		select {
		case <-ctx.Done():
			return
		case <-d.quit:
			return
		default:
			d.enqueue(addr)
		}
	}()
}

func (d *DialScheduler) Close() {
	close(d.quit)
}
