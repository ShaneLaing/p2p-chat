package protocol

import (
	"fmt"
	"sync"
)

// Metrics captures a snapshot of sent/seen/acked counters for diagnostics.
type Metrics struct {
	mu    sync.Mutex
	sent  int
	seen  int
	acked int
}

func NewMetrics() *Metrics { return &Metrics{} }

func (m *Metrics) IncSent() { m.mu.Lock(); m.sent++; m.mu.Unlock() }
func (m *Metrics) IncSeen() { m.mu.Lock(); m.seen++; m.mu.Unlock() }
func (m *Metrics) IncAck()  { m.mu.Lock(); m.acked++; m.mu.Unlock() }

func (m *Metrics) Snapshot() MetricsSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return MetricsSnapshot{Sent: m.sent, Seen: m.seen, Acked: m.acked}
}

// MetricsSnapshot is printed in `/stats` command output.
type MetricsSnapshot struct {
	Sent  int
	Seen  int
	Acked int
}

func (s MetricsSnapshot) String() string {
	return fmt.Sprintf("sent=%d seen=%d acked=%d", s.Sent, s.Seen, s.Acked)
}
