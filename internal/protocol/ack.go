package protocol

import (
	"log"
	"sync"
	"time"

	"p2p-chat/internal/message"
)

const (
	ackCheckInterval = 3 * time.Second
	ackTimeout       = 7 * time.Second
	ackMaxAttempts   = 3
)

type broadcaster interface {
	Broadcast(message.Message, string)
}

type pendingAck struct {
	msg      message.Message
	attempts int
	lastSend time.Time
}

// AckTracker retries messages that have not been acknowledged yet.
type AckTracker struct {
	cm      broadcaster
	mu      sync.Mutex
	pending map[string]*pendingAck
	quit    chan struct{}
}

func NewAckTracker(cm broadcaster) *AckTracker {
	tracker := &AckTracker{
		cm:      cm,
		pending: make(map[string]*pendingAck),
		quit:    make(chan struct{}),
	}
	go tracker.loop()
	return tracker
}

func (a *AckTracker) Track(msg message.Message) {
	if msg.MsgID == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pending[msg.MsgID] = &pendingAck{msg: msg, attempts: 1, lastSend: time.Now()}
}

func (a *AckTracker) Confirm(msgID string) {
	if msgID == "" {
		return
	}
	a.mu.Lock()
	delete(a.pending, msgID)
	a.mu.Unlock()
}

func (a *AckTracker) loop() {
	ticker := time.NewTicker(ackCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			a.rebroadcastExpired()
		case <-a.quit:
			return
		}
	}
}

func (a *AckTracker) rebroadcastExpired() {
	now := time.Now()
	var resend []message.Message

	a.mu.Lock()
	for id, pending := range a.pending {
		if now.Sub(pending.lastSend) < ackTimeout {
			continue
		}
		if pending.attempts >= ackMaxAttempts {
			log.Printf("dropping msg %s after %d attempts", id, pending.attempts)
			delete(a.pending, id)
			continue
		}
		pending.attempts++
		pending.lastSend = now
		resend = append(resend, pending.msg)
	}
	a.mu.Unlock()

	for _, msg := range resend {
		a.cm.Broadcast(msg, "")
	}
}

func (a *AckTracker) Stop() {
	close(a.quit)
}
