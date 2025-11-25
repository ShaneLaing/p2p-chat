package peer

import (
	"fmt"
	"sync"
)

type metrics struct {
	mu    sync.Mutex
	sent  int
	seen  int
	acked int
}

func newMetrics() *metrics { return &metrics{} }

func (m *metrics) IncSent() { m.mu.Lock(); m.sent++; m.mu.Unlock() }
func (m *metrics) IncSeen() { m.mu.Lock(); m.seen++; m.mu.Unlock() }
func (m *metrics) IncAck()  { m.mu.Lock(); m.acked++; m.mu.Unlock() }

func (m *metrics) Snapshot() metricsSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return metricsSnapshot{Sent: m.sent, Seen: m.seen, Acked: m.acked}
}

type metricsSnapshot struct {
	Sent  int
	Seen  int
	Acked int
}

func (s metricsSnapshot) String() string {
	return fmt.Sprintf("sent=%d seen=%d acked=%d", s.Sent, s.Seen, s.Acked)
}
