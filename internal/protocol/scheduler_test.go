package protocol

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type mockConnector struct {
	mu        sync.Mutex
	failures  map[string]int
	callCount map[string]int
}

func newMockConnector() *mockConnector {
	return &mockConnector{
		failures:  make(map[string]int),
		callCount: make(map[string]int),
	}
}

func (m *mockConnector) ConnectToPeer(addr string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount[addr]++
	if remaining, ok := m.failures[addr]; ok && remaining > 0 {
		m.failures[addr] = remaining - 1
		return errors.New("dial error")
	}
	return nil
}

func (m *mockConnector) Calls(addr string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount[addr]
}

func TestDialSchedulerAddIgnoresInvalid(t *testing.T) {
	connector := newMockConnector()
	scheduler := NewDialScheduler(connector, "self")
	scheduler.Add("")
	scheduler.Add("self")
	scheduler.Add("peer1")
	scheduler.Add("peer1")
	desired := scheduler.Desired()
	if len(desired) != 1 || desired[0] != "peer1" {
		t.Fatalf("unexpected desired list: %+v", desired)
	}
}

func TestDialSchedulerRunKeepsDesiredAfterSuccess(t *testing.T) {
	connector := newMockConnector()
	scheduler := NewDialScheduler(connector, "self")
	originalBackoff, originalJitter := dialBackoff, dialJitterRange
	dialBackoff, dialJitterRange = 5*time.Millisecond, 0
	defer func() { dialBackoff, dialJitterRange = originalBackoff, originalJitter }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go scheduler.Run(ctx)
	scheduler.Add("peer2")
	waitFor(t, func() bool { return connector.Calls("peer2") >= 1 })
	waitFor(t, func() bool {
		desired := scheduler.Desired()
		return len(desired) == 1 && desired[0] == "peer2"
	})
	waitFor(t, func() bool { return connector.Calls("peer2") >= 2 })
	scheduler.Close()
}

func TestDialSchedulerRetriesAfterFailure(t *testing.T) {
	connector := newMockConnector()
	connector.failures["peer3"] = 1
	scheduler := NewDialScheduler(connector, "self")
	originalBackoff, originalJitter := dialBackoff, dialJitterRange
	dialBackoff, dialJitterRange = 5*time.Millisecond, 0
	defer func() { dialBackoff, dialJitterRange = originalBackoff, originalJitter }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go scheduler.Run(ctx)
	scheduler.Add("peer3")
	waitFor(t, func() bool { return connector.Calls("peer3") >= 2 })
	scheduler.Close()
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition not met before deadline")
}
