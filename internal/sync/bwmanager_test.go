package sync

import (
	"testing"
)

func newBWTestEngine(t *testing.T, id string, m *BandwidthManager) *Engine {
	t.Helper()
	return NewEngine(SyncConfig{
		ID:        id,
		SourceDir: t.TempDir(),
		TargetDir: t.TempDir(),
		DryRun:    true,
		BWManager: m,
	})
}

func TestBandwidthManager_Shares(t *testing.T) {
	tests := []struct {
		name     string
		limitBps int64
		acquire  []string
		want     map[string]int64 // expected share per acquired engine
	}{
		{
			name:     "single engine gets full limit",
			limitBps: 10000,
			acquire:  []string{"e1"},
			want:     map[string]int64{"e1": 10000},
		},
		{
			name:     "limit divided among two engines",
			limitBps: 10000,
			acquire:  []string{"e1", "e2"},
			want:     map[string]int64{"e1": 5000, "e2": 5000},
		},
		{
			name:     "limit divided among three engines",
			limitBps: 9000,
			acquire:  []string{"e1", "e2", "e3"},
			want:     map[string]int64{"e1": 3000, "e2": 3000, "e3": 3000},
		},
		{
			name:     "unlimited pushes zero",
			limitBps: 0,
			acquire:  []string{"e1"},
			want:     map[string]int64{"e1": 0},
		},
		{
			name:     "tiny limit floored at 1 KB/s",
			limitBps: 1500,
			acquire:  []string{"e1", "e2"},
			want:     map[string]int64{"e1": MinShareBps, "e2": MinShareBps},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewBandwidthManager(0)
			engines := map[string]*Engine{
				"e1": newBWTestEngine(t, "e1", m),
				"e2": newBWTestEngine(t, "e2", m),
				"e3": newBWTestEngine(t, "e3", m),
			}
			m.SetGlobalLimit(tt.limitBps, LimitSourceManual)
			for _, id := range tt.acquire {
				m.Acquire(id)
			}
			defer func() {
				for _, id := range tt.acquire {
					m.Release(id)
				}
			}()

			if got := m.ActiveCount(); got != len(tt.acquire) {
				t.Errorf("ActiveCount = %d, want %d", got, len(tt.acquire))
			}
			for id, wantShare := range tt.want {
				if got := engines[id].transferer.BandwidthLimit(); got != wantShare {
					t.Errorf("engine %s share = %d, want %d", id, got, wantShare)
				}
			}
		})
	}
}

func TestBandwidthManager_MidFlightLimitChange(t *testing.T) {
	m := NewBandwidthManager(0)
	e1 := newBWTestEngine(t, "e1", m)
	e2 := newBWTestEngine(t, "e2", m)

	m.SetGlobalLimit(10000, LimitSourceManual)
	m.Acquire("e1")
	m.Acquire("e2")
	defer m.Release("e1")
	defer m.Release("e2")

	for id, e := range map[string]*Engine{"e1": e1, "e2": e2} {
		if got := e.transferer.BandwidthLimit(); got != 5000 {
			t.Fatalf("engine %s share = %d, want 5000", id, got)
		}
	}

	// Changing the limit mid-flight re-pushes shares to active engines.
	m.SetGlobalLimit(20000, LimitSourceManual)
	for id, e := range map[string]*Engine{"e1": e1, "e2": e2} {
		if got := e.transferer.BandwidthLimit(); got != 10000 {
			t.Errorf("after limit change engine %s share = %d, want 10000", id, got)
		}
	}
}

func TestBandwidthManager_Release(t *testing.T) {
	m := NewBandwidthManager(10000)
	e1 := newBWTestEngine(t, "e1", m)
	e2 := newBWTestEngine(t, "e2", m)

	m.Acquire("e1")
	m.Acquire("e2")

	// Releasing one engine gives the full limit back to the other.
	m.Release("e2")
	if got := e1.transferer.BandwidthLimit(); got != 10000 {
		t.Errorf("after release engine e1 share = %d, want 10000", got)
	}

	// Release-all returns to unlimited.
	m.Release("e1")
	if got := e1.transferer.BandwidthLimit(); got != 0 {
		t.Errorf("after release-all engine e1 share = %d, want 0 (unlimited)", got)
	}
	if got := e2.transferer.BandwidthLimit(); got != 0 {
		t.Errorf("released engine e2 should be back to unlimited, got %d", got)
	}
	if got := m.ActiveCount(); got != 0 {
		t.Errorf("ActiveCount = %d, want 0", got)
	}
}

func TestBandwidthManager_Source(t *testing.T) {
	m := NewBandwidthManager(0)
	if got := m.Source(); got != LimitSourceManual {
		t.Errorf("initial source = %q, want %q", got, LimitSourceManual)
	}
	m.SetGlobalLimit(5000, LimitSourceSchedule)
	if got := m.Source(); got != LimitSourceSchedule {
		t.Errorf("source = %q, want %q", got, LimitSourceSchedule)
	}
	if got := m.CurrentLimit(); got != 5000 {
		t.Errorf("CurrentLimit = %d, want 5000", got)
	}
}
