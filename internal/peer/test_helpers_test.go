package peer

import (
	"context"
	"sync"
	"time"

	"p2p-chat/internal/message"
	"p2p-chat/internal/network"
)

type recordingSink struct {
	mu            sync.Mutex
	messages      []message.Message
	systems       []string
	peerSnapshots [][]peerPresence
	notifications []notificationPayload
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

func (s *recordingSink) UpdatePeers(peers []peerPresence) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := make([]peerPresence, len(peers))
	copy(snapshot, peers)
	s.peerSnapshots = append(s.peerSnapshots, snapshot)
}

func (s *recordingSink) ShowNotification(n notificationPayload) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifications = append(s.notifications, n)
}

func (s *recordingSink) Notifications() []notificationPayload {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]notificationPayload, len(s.notifications))
	copy(out, s.notifications)
	return out
}

func (s *recordingSink) LastMessage() message.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.messages) == 0 {
		return message.Message{}
	}
	return s.messages[len(s.messages)-1]
}

func (s *recordingSink) SystemMessages() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.systems))
	copy(out, s.systems)
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

func newTestAppContext() (*appContext, *recordingSink, *recordingBroadcaster) {
	sink := &recordingSink{}
	broadcaster := &recordingBroadcaster{}
	ctx := &appContext{
		ctx:          context.Background(),
		cm:           &network.ConnManager{},
		cache:        newMsgCache(10 * time.Minute),
		history:      newHistory(128),
		store:        &historyStore{},
		blocklist:    newBlockList(),
		directory:    newPeerDirectory(),
		metrics:      newMetrics(),
		sink:         sink,
		identity:     newIdentity("tester", "tester"),
		selfAddr:     "127.0.0.1:9001",
		bootstrapURL: "http://localhost:8000",
		pollInterval: time.Second,
	}
	ctx.ack = &ackTracker{cm: broadcaster, pending: make(map[string]*pendingAck), quit: make(chan struct{})}
	return ctx, sink, broadcaster
}
