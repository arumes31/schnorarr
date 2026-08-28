package scheduler

import (
	"testing"

	"schnorarr/internal/monitor/config"
	syncpkg "schnorarr/internal/sync"
)

func TestInQuietWindow(t *testing.T) {
	tests := []struct {
		name       string
		start, end string
		currentHM  string
		want       bool
	}{
		{"normal window inside", "08:00", "18:00", "12:00", true},
		{"normal window at start", "08:00", "18:00", "08:00", true},
		{"normal window before end", "08:00", "18:00", "17:59", true},
		{"normal window at end is outside", "08:00", "18:00", "18:00", false},
		{"normal window before start", "08:00", "18:00", "07:59", false},
		{"midnight crossing evening", "23:00", "07:00", "23:30", true},
		{"midnight crossing at start", "23:00", "07:00", "23:00", true},
		{"midnight crossing early morning", "23:00", "07:00", "06:59", true},
		{"midnight crossing at end is outside", "23:00", "07:00", "07:00", false},
		{"midnight crossing midday outside", "23:00", "07:00", "12:00", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := InQuietWindow(tt.start, tt.end, tt.currentHM); got != tt.want {
				t.Errorf("InQuietWindow(%s, %s, %s) = %v, want %v", tt.start, tt.end, tt.currentHM, got, tt.want)
			}
		})
	}
}

func TestScheduler_Evaluate(t *testing.T) {
	m := syncpkg.NewBandwidthManager(0)
	cfg := &config.Config{
		SchedulerEnabled: true,
		// Window covers the whole day so the test is time-independent.
		QuietStart:  "00:00",
		QuietEnd:    "24:00",
		QuietLimit:  10,
		NormalLimit: 100,
	}
	s := New(cfg, m)

	// Inside the (all-day) quiet window: quiet limit is applied.
	s.evaluate()
	if got := m.CurrentLimit(); got != 10*125000 {
		t.Errorf("CurrentLimit = %d, want %d (quiet limit)", got, 10*125000)
	}
	if got := m.Source(); got != syncpkg.LimitSourceSchedule {
		t.Errorf("Source = %q, want %q", got, syncpkg.LimitSourceSchedule)
	}

	// Re-evaluating without a state change must not push again, so a manual
	// override in between survives until the next boundary.
	m.SetGlobalLimit(42*125000, syncpkg.LimitSourceManual)
	s.evaluate()
	if got := m.CurrentLimit(); got != 42*125000 {
		t.Errorf("manual override was overwritten between boundaries: CurrentLimit = %d", got)
	}

	// A config change (new quiet limit) is picked up on the next evaluation.
	cfg.QuietLimit = 20
	s.evaluate()
	if got := m.CurrentLimit(); got != 20*125000 {
		t.Errorf("after config change CurrentLimit = %d, want %d", got, 20*125000)
	}

	// Disabling the scheduler stops it from pushing.
	cfg.SchedulerEnabled = false
	m.SetGlobalLimit(7*125000, syncpkg.LimitSourceManual)
	s.evaluate()
	if got := m.CurrentLimit(); got != 7*125000 {
		t.Errorf("disabled scheduler pushed a limit: CurrentLimit = %d", got)
	}
}
