package scheduler

import (
	"fmt"
	"log"
	"os"
	"time"

	"schnorarr/internal/monitor/config"
)

// ControlFunc is called to reload lsyncd when bandwidth limit changes
type ControlFunc func(action string)

// Scheduler handles bandwidth limit scheduling
type Scheduler struct {
	config      *config.Config
	controlFunc ControlFunc
}

// New creates a new bandwidth scheduler
func New(cfg *config.Config, controlFn ControlFunc) *Scheduler {
	return &Scheduler{
		config:      cfg,
		controlFunc: controlFn,
	}
}

// Start begins the scheduler loop
func (s *Scheduler) Start() {
	ticker := time.NewTicker(1 * time.Minute)
	for range ticker.C {
		if !s.config.SchedulerEnabled {
			continue
		}

		now := time.Now()
		currentHM := now.Format("15:04")

		// Simple check: is current time between Start and End?
		// Handle crossing midnight: Start > End (e.g. 23:00 to 07:00)
		inQuietWindow := false
		if s.config.QuietStart <= s.config.QuietEnd {
			inQuietWindow = currentHM >= s.config.QuietStart && currentHM < s.config.QuietEnd
		} else {
			inQuietWindow = currentHM >= s.config.QuietStart || currentHM < s.config.QuietEnd
		}

		targetLimit := s.config.NormalLimit
		if inQuietWindow {
			targetLimit = s.config.QuietLimit
		}

		// Read current limit from file to see if update needed
		currentFileLimit := -1
		if b, err := os.ReadFile("/config/bwlimit"); err == nil {
			if _, err := fmt.Sscanf(string(b), "%d", &currentFileLimit); err != nil {
				log.Printf("Scheduler: Failed to parse bwlimit: %v", err)
			}
		}

		if targetLimit != currentFileLimit {
			limitStr := fmt.Sprintf("%d", targetLimit)
			if err := os.WriteFile("/config/bwlimit", []byte(limitStr), 0644); err != nil {
				log.Printf("Scheduler: Failed to write bwlimit: %v", err)
			} else {
				if s.controlFunc != nil {
					s.controlFunc("reload")
				}
				log.Printf("Scheduler: Updated bwlimit to %d Mbps (Quiet: %v)", targetLimit, inQuietWindow)
			}
		}
	}
}
