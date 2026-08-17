package sync

import (
	"log"
	"sync"
)

const (
	// MinShareBps is the minimum per-engine share (1 KB/s) pushed when a global
	// limit is set. rsync treats --bwlimit=0 as unlimited, so a limit that is
	// meant must never be pushed as 0.
	MinShareBps = 1024

	// LimitSourceManual marks a limit set via the UI/API (or startup config).
	LimitSourceManual = "manual"
	// LimitSourceSchedule marks a limit set by the quiet-hours scheduler.
	LimitSourceSchedule = "schedule"
)

// BandwidthManager coordinates a global bandwidth limit shared across all
// concurrently transferring engines. On every change (limit set, transfer
// batch start/end) it recomputes each active engine's share (limit ÷ n active)
// and pushes it down; engines pick up the new share at the next file boundary.
type BandwidthManager struct {
	mu       sync.Mutex
	limitBps int64              // global limit, bytes/sec; 0 = unlimited
	source   string             // who set the current limit ("manual" / "schedule")
	active   map[string]struct{} // engine IDs currently transferring
	engines  map[string]*Engine // registry for pushing shares down
}

// NewBandwidthManager creates a manager with the given initial global limit
// in bytes/sec (0 = unlimited).
func NewBandwidthManager(initialLimitBps int64) *BandwidthManager {
	if initialLimitBps < 0 {
		initialLimitBps = 0
	}
	return &BandwidthManager{
		limitBps: initialLimitBps,
		source:   LimitSourceManual,
		active:   make(map[string]struct{}),
		engines:  make(map[string]*Engine),
	}
}

// RegisterEngine adds an engine to the registry so shares can be pushed to it.
func (m *BandwidthManager) RegisterEngine(id string, e *Engine) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.engines[id] = e
}

// SetGlobalLimit sets the global limit in bytes/sec (0 = unlimited) and
// re-pushes shares to all active engines. source records who set the limit
// (LimitSourceManual / LimitSourceSchedule) for display in the UI.
func (m *BandwidthManager) SetGlobalLimit(bps int64, source string) {
	if bps < 0 {
		bps = 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.limitBps = bps
	m.source = source
	log.Printf("[BWManager] Global limit set to %d B/s (source: %s, active: %d)", bps, source, len(m.active))
	m.recompute()
}

// Acquire marks the start of an engine's transfer batch and rebalances shares.
func (m *BandwidthManager) Acquire(engineID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active[engineID] = struct{}{}
	m.recompute()
}

// Release marks the end of an engine's transfer batch, clears its share back
// to unlimited, and rebalances shares among the remaining active engines.
// Must be deferred at the call site so a crashed transfer cannot leak a slot.
func (m *BandwidthManager) Release(engineID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.active, engineID)
	// Only active engines hold a share; a released engine goes back to
	// unlimited so a stale limit never survives into its next batch.
	if e, ok := m.engines[engineID]; ok {
		e.SetBandwidthLimit(0)
	}
	m.recompute()
}

// CurrentLimit returns the global limit in bytes/sec (0 = unlimited).
func (m *BandwidthManager) CurrentLimit() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.limitBps
}

// Source returns who set the current limit ("manual" / "schedule").
func (m *BandwidthManager) Source() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.source
}

// ActiveCount returns the number of engines currently transferring.
func (m *BandwidthManager) ActiveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.active)
}

// recompute pushes limit/n shares to every active engine.
// Callers must hold m.mu.
func (m *BandwidthManager) recompute() {
	var share int64
	if m.limitBps > 0 && len(m.active) > 0 {
		share = m.limitBps / int64(len(m.active))
		if share < MinShareBps {
			log.Printf("[BWManager] Share %d B/s below floor for %d active engines, clamping to %d B/s", share, len(m.active), MinShareBps)
			share = MinShareBps
		}
	}
	for id := range m.active {
		if e, ok := m.engines[id]; ok {
			e.SetBandwidthLimit(share)
		}
	}
}
