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
