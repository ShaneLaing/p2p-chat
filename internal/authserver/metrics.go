package authserver

import "sync/atomic"

// Metrics captures lightweight in-process counters for observability.
type Metrics struct {
	AuthRequests         atomic.Uint64
	LoginAttempts        atomic.Uint64
	RegisterAttempts     atomic.Uint64
	HealthChecks         atomic.Uint64
	StatelessModeLogins  atomic.Uint64
	PersistentModeLogins atomic.Uint64
}

// MetricsSnapshot is a copy-friendly view for logging/testing.
type MetricsSnapshot struct {
	AuthRequests         uint64
	LoginAttempts        uint64
	RegisterAttempts     uint64
	HealthChecks         uint64
	StatelessModeLogins  uint64
	PersistentModeLogins uint64
}
