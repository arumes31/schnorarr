package health

import (
	"sync"
)

type ReceiverStatus struct {
	Healthy bool
	Message string
	Version string
	Uptime  string
}

type State struct {
	mu              sync.RWMutex
	healthy         bool
	lastError       string
	receiver        ReceiverStatus
	senderOverride  bool
}

func New() *State {
	return &State{
		healthy: true,
	}
}

func (s *State) ReportSuccess(notify func(string, string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.healthy = true
	s.lastError = ""
}

func (s *State) ReportError(msg string, notify func(string, string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.healthy = false
	s.lastError = msg
	if notify != nil {
		go notify("System Error: "+msg, "CRITICAL")
	}
}

func (s *State) ReportReceiverStatus(healthy bool, msg string, version, uptime string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.receiver.Healthy = healthy
	s.receiver.Message = msg
	s.receiver.Version = version
	s.receiver.Uptime = uptime
}

func (s *State) GetStatus() (bool, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.healthy, s.lastError
}

func (s *State) GetReceiverStatus() (bool, string, string, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.receiver.Healthy, s.receiver.Message, s.receiver.Version, s.receiver.Uptime
}

func (s *State) SetSenderOverride(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.senderOverride = enabled
}

func (s *State) IsOverrideEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.senderOverride
}