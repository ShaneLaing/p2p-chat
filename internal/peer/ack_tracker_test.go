package peer

import (
	"sync"
	"testing"
	"time"

	"p2p-chat/internal/message"
)

type stubBroadcaster struct {
	mu   sync.Mutex
	sent []message.Message
}

func (s *stubBroadcaster) Broadcast(msg message.Message, _ string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, msg)
}

func (s *stubBroadcaster) Messages() []message.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]message.Message, len(s.sent))
	copy(out, s.sent)
	return out
}

func TestAckTrackerRebroadcastExpired(t *testing.T) {
	stub := &stubBroadcaster{}
	tracker := &ackTracker{cm: stub, pending: make(map[string]*pendingAck)}

	msg := message.Message{MsgID: "m1", Timestamp: time.Now()}
	tracker.Track(msg)

	tracker.mu.Lock()
	tracker.pending[msg.MsgID].lastSend = time.Now().Add(-2 * ackTimeout)
	tracker.mu.Unlock()

	tracker.rebroadcastExpired()

	if got := len(stub.Messages()); got != 1 {
		t.Fatalf("expected 1 rebroadcast, got %d", got)
	}
	tracker.mu.Lock()
	attempts := tracker.pending[msg.MsgID].attempts
	tracker.mu.Unlock()
	if attempts != 2 {
		t.Fatalf("expected attempts incremented to 2, got %d", attempts)
	}
}

func TestAckTrackerDropsAfterMaxAttempts(t *testing.T) {
	stub := &stubBroadcaster{}
	tracker := &ackTracker{cm: stub, pending: make(map[string]*pendingAck)}

	msg := message.Message{MsgID: "m2", Timestamp: time.Now()}
	tracker.Track(msg)

	tracker.mu.Lock()
	tracker.pending[msg.MsgID].lastSend = time.Now().Add(-2 * ackTimeout)
	tracker.pending[msg.MsgID].attempts = ackMaxAttempts
	tracker.mu.Unlock()

	tracker.rebroadcastExpired()

	if got := len(stub.Messages()); got != 0 {
		t.Fatalf("expected no rebroadcast when attempts exhausted, got %d", got)
	}
	tracker.mu.Lock()
	if len(tracker.pending) != 0 {
		tracker.mu.Unlock()
		t.Fatalf("expected message removed after max attempts")
	}
	tracker.mu.Unlock()
}
