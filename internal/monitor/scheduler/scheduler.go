package scheduler

import (
	"log"
	"time"

	"schnorarr/internal/monitor/config"
	syncpkg "schnorarr/internal/sync"
)

// Scheduler applies quiet-hours bandwidth limits by pushing them into the
// shared BandwidthManager. It acts only at window boundaries (and on config
// changes): between boundaries a manually set limit stays in effect.
type Scheduler struct {
	config     *config.Config
	manager    *syncpkg.BandwidthManager
	lastWindow *bool // last evaluated quiet-window state; nil = not applied yet
	lastTarget int   // last applied target limit in Mbps
}

// New creates a new bandwidth scheduler
func New(cfg *config.Config, manager *syncpkg.BandwidthManager) *Scheduler {
	return &Scheduler{
		config:  cfg,
		manager: manager,
	}
}

// InQuietWindow reports whether currentHM (HH:MM) lies inside the quiet
// window, handling windows that cross midnight (start > end, e.g. 23:00-07:00).
func InQuietWindow(start, end, currentHM string) bool {
	if start <= end {
		return currentHM >= start && currentHM < end
	}
	return currentHM >= start || currentHM < end
}

// Start begins the scheduler loop
func (s *Scheduler) Start() {
	s.evaluate()
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.evaluate()
	}
}

// evaluate pushes the scheduled limit into the manager when the quiet-window
// state changes (window boundary) or the configured target changes.
func (s *Scheduler) evaluate() {
	if !s.config.SchedulerEnabled {
		// Reset so re-enabling (or a config change while disabled) applies immediately.
		s.lastWindow = nil
		return
	}

	currentHM := time.Now().Format("15:04")
	inQuietWindow := InQuietWindow(s.config.QuietStart, s.config.QuietEnd, currentHM)

	targetLimit := s.config.NormalLimit
	if inQuietWindow {
		targetLimit = s.config.QuietLimit
	}

	if s.lastWindow != nil && *s.lastWindow == inQuietWindow && s.lastTarget == targetLimit {
		return
	}

	if s.manager != nil {
		s.manager.SetGlobalLimit(int64(targetLimit)*125000, syncpkg.LimitSourceSchedule)
		log.Printf("Scheduler: Set global bwlimit to %d Mbps (Quiet: %v)", targetLimit, inQuietWindow)
	}
	s.lastWindow = &inQuietWindow
	s.lastTarget = targetLimit
}
