package health

import (
	"fmt"
	"sync"
	"time"
)

const MaxConsecutiveErrors = 5

// State tracks the system health state
type State struct {
	Healthy       bool
	ErrorCount    int
	LastErrorTime time.Time
	LastErrorMsg  string
	mu            sync.Mutex
}

// NotifyFunc is a callback for sending notifications
type NotifyFunc func(msg, msgType string)

// New creates a new health state tracker
func New() *State {
	return &State{
		Healthy: true,
	}
}

// ReportError records an error and triggers unhealthy state if threshold exceeded
func (s *State) ReportError(msg string, notifyFn NotifyFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ErrorCount++
	s.LastErrorTime = time.Now()
	s.LastErrorMsg = msg

	if s.Healthy && s.ErrorCount >= MaxConsecutiveErrors {
		s.Healthy = false
		if notifyFn != nil {
			go notifyFn(fmt.Sprintf("System Unhealthy: %s", msg), "ERROR")
		}
	}
}

// ReportSuccess clears errors and marks system as healthy
func (s *State) ReportSuccess(notifyFn NotifyFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.Healthy {
		s.Healthy = true
		if notifyFn != nil {
			go notifyFn("System Recovered. Syncing normally.", "SUCCESS")
		}
	}
	s.ErrorCount = 0
	s.LastErrorMsg = ""
}

// GetStatus returns the current health status (thread-safe)
func (s *State) GetStatus() (healthy bool, lastErr string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Healthy, s.LastErrorMsg
}
