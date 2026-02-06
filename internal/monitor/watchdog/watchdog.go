package watchdog

import (
	"log"
	"sync"
	"time"
)

// NotifyFunc is a callback for sending notifications
type NotifyFunc func(msg, msgType string)

// ControlFunc is called to restart lsyncd when stuck
type ControlFunc func(action string)

// Watchdog monitors sync progress and restarts if stuck
type Watchdog struct {
	lastProgressTime time.Time
	mu               sync.Mutex
	notifyFn         NotifyFunc
	controlFn        ControlFunc
}

// New creates a new watchdog
func New(notifyFn NotifyFunc, controlFn ControlFunc) *Watchdog {
	return &Watchdog{
		lastProgressTime: time.Now(),
		notifyFn:         notifyFn,
		controlFn:        controlFn,
	}
}

// Update records progress (speed > 0 or queue empty)
func (w *Watchdog) Update(queued int, speed int64) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// If things are moving (speed > 0) OR queue is empty (nothing to do), reset timer
	if speed > 0 || queued == 0 {
		w.lastProgressTime = time.Now()
	}
}

// Start begins the watchdog monitoring loop
func (w *Watchdog) Start() {
	log.Println("Starting Deep Health Watchdog...")
	ticker := time.NewTicker(2 * time.Minute)

	for range ticker.C {
		w.mu.Lock()
		lastProgress := w.lastProgressTime
		w.mu.Unlock()

		// Threshold: 15 minutes without progress WHILE items are queued
		if time.Since(lastProgress) > 15*time.Minute {
			log.Println("[WATCHDOG] Sync appears stuck (Queue > 0, Speed = 0 for >15m). Restarting...")

			if w.notifyFn != nil {
				w.notifyFn("⚠️ Watchdog detected stuck sync. Restarting engine...", "ERROR")
			}

			if w.controlFn != nil {
				w.controlFn("restart")
			}

			w.mu.Lock()
			w.lastProgressTime = time.Now()
			w.mu.Unlock()
		}
	}
}
