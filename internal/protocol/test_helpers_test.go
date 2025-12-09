package protocol

import (
	"context"
	"sync"
	"testing"
	"time"

	"p2p-chat/internal/message"
	"p2p-chat/internal/network"
	"p2p-chat/internal/storage"
	"p2p-chat/internal/ui"
)

type recordingSink struct {
	mu            sync.Mutex
	messages      []message.Message
	systems       []string
	peerSnapshots [][]ui.Presence
	notifications []ui.Notification
}

func (s *recordingSink) ShowMessage(msg message.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, msg)
}

func (s *recordingSink) ShowSystem(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.systems = append(s.systems, text)
}

func (s *recordingSink) UpdatePeers(peers []ui.Presence) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := make([]ui.Presence, len(peers))
	copy(snapshot, peers)
	s.peerSnapshots = append(s.peerSnapshots, snapshot)
}

func (s *recordingSink) ShowNotification(n ui.Notification) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifications = append(s.notifications, n)
}

func (s *recordingSink) lastMessage() message.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.messages) == 0 {
		return message.Message{}
	}
	return s.messages[len(s.messages)-1]
}

func (s *recordingSink) notificationCopy() []ui.Notification {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ui.Notification, len(s.notifications))
	copy(out, s.notifications)
	return out
}

type recordingBroadcaster struct {
	mu   sync.Mutex
	sent []message.Message
}

func (b *recordingBroadcaster) Broadcast(msg message.Message, _ string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sent = append(b.sent, msg)
}

func (b *recordingBroadcaster) Messages() []message.Message {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]message.Message, len(b.sent))
	copy(out, b.sent)
	return out
}

func newTestRuntime(t *testing.T) (*Runtime, *recordingSink, *recordingBroadcaster) {
	t.Helper()
	sink := &recordingSink{}
	broadcaster := &recordingBroadcaster{}
	cm := &network.ConnManager{Incoming: make(chan message.Message, 1)}
	dialer := NewDialScheduler(cm, "127.0.0.1:9001")
	ack := NewAckTracker(broadcaster)
	rt := NewRuntime(context.Background(), RuntimeOptions{
		ConnManager:  cm,
		CacheTTL:     10 * time.Minute,
		HistorySize:  128,
		Store:        &storage.HistoryStore{},
		Files:        nil,
		Blocklist:    NewBlockList(),
		Directory:    NewPeerDirectory(),
		Metrics:      NewMetrics(),
		Ack:          ack,
		Dialer:       dialer,
		Sink:         sink,
		Identity:     NewIdentity("tester", "tester"),
		SelfAddr:     "127.0.0.1:9001",
		BootstrapURL: "http://localhost:8000",
		PollInterval: time.Second,
		AuthAPI:      "",
	})
	t.Cleanup(func() {
		if ack := rt.AckTracker(); ack != nil {
			ack.Stop()
		}
		if dialer := rt.Dialer(); dialer != nil {
			dialer.Close()
		}
	})
	return rt, sink, broadcaster
}
