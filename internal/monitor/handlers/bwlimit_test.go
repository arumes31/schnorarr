package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"schnorarr/internal/monitor/config"
	syncpkg "schnorarr/internal/sync"
)

func postForm(t *testing.T, handler http.HandlerFunc, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	oldAuth := AuthEnabled
	AuthEnabled = false
	defer func() { AuthEnabled = oldAuth }()

	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

func TestHandlers_UpdateBwlimit(t *testing.T) {
	m := syncpkg.NewBandwidthManager(0)
	cfg := &config.Config{}
	h := New(cfg, nil, nil, nil, nil, nil, m)

	// Valid value
	w := postForm(t, h.UpdateBwlimit, "/api/settings/bwlimit", url.Values{"mbps": {"50"}})
	if w.Result().StatusCode != http.StatusOK {
		t.Errorf("Expected 200 for valid Mbps, got %d", w.Result().StatusCode)
	}
	if got := m.CurrentLimit(); got != 50*125000 {
		t.Errorf("Manager limit = %d, want %d", got, 50*125000)
	}
	if cfg.BwlimitMbps == nil || *cfg.BwlimitMbps != 50 {
		t.Errorf("Config BwlimitMbps = %v, want 50", cfg.BwlimitMbps)
	}
	if got := m.Source(); got != syncpkg.LimitSourceManual {
		t.Errorf("Manager source = %q, want %q", got, syncpkg.LimitSourceManual)
	}

	// Zero means unlimited and must be accepted
	w = postForm(t, h.UpdateBwlimit, "/api/settings/bwlimit", url.Values{"mbps": {"0"}})
	if w.Result().StatusCode != http.StatusOK {
		t.Errorf("Expected 200 for Mbps=0, got %d", w.Result().StatusCode)
	}
	if got := m.CurrentLimit(); got != 0 {
		t.Errorf("Manager limit = %d, want 0 (unlimited)", got)
	}

	// Invalid values
	for _, bad := range []string{"-5", "abc", "", "1.5"} {
		w = postForm(t, h.UpdateBwlimit, "/api/settings/bwlimit", url.Values{"mbps": {bad}})
		if w.Result().StatusCode != http.StatusBadRequest {
			t.Errorf("Expected 400 for mbps=%q, got %d", bad, w.Result().StatusCode)
		}
	}
}

func TestHandlers_SetScheduler(t *testing.T) {
	cfg := &config.Config{}
	h := New(cfg, nil, nil, nil, nil, nil, nil)

	// Valid full schedule
	w := postForm(t, h.SetScheduler, "/settings/scheduler", url.Values{
		"scheduler_enabled": {"on"},
		"quiet_start":       {"23:00"},
		"quiet_end":         {"07:00"},
		"quiet_limit":       {"10"},
		"normal_limit":      {"100"},
	})
	if w.Result().StatusCode != http.StatusOK {
		t.Errorf("Expected 200 for valid schedule, got %d", w.Result().StatusCode)
	}
	if !cfg.SchedulerEnabled {
		t.Error("Expected SchedulerEnabled to be true")
	}
	if cfg.QuietStart != "23:00" || cfg.QuietEnd != "07:00" {
		t.Errorf("Quiet window = %s-%s, want 23:00-07:00", cfg.QuietStart, cfg.QuietEnd)
	}
	if cfg.QuietLimit != 10 || cfg.NormalLimit != 100 {
		t.Errorf("Limits = %d/%d, want 10/100", cfg.QuietLimit, cfg.NormalLimit)
	}

	// Invalid HH:MM values
	for _, tc := range []struct{ start, end string }{
		{"25:00", "07:00"},
		{"23:00", "7pm"},
		{"23:60", "07:00"},
		{"", "07:00"},
	} {
		w = postForm(t, h.SetScheduler, "/settings/scheduler", url.Values{
			"quiet_start":  {tc.start},
			"quiet_end":    {tc.end},
			"quiet_limit":  {"10"},
			"normal_limit": {"100"},
		})
		if w.Result().StatusCode != http.StatusBadRequest {
			t.Errorf("Expected 400 for window %q-%q, got %d", tc.start, tc.end, w.Result().StatusCode)
		}
	}

	// Invalid limits
	w = postForm(t, h.SetScheduler, "/settings/scheduler", url.Values{
		"quiet_start":  {"23:00"},
		"quiet_end":    {"07:00"},
		"quiet_limit":  {"-1"},
		"normal_limit": {"100"},
	})
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Errorf("Expected 400 for negative quiet_limit, got %d", w.Result().StatusCode)
	}

	// Last-known-good config must be untouched by the rejected requests
	if cfg.QuietStart != "23:00" || cfg.QuietLimit != 10 {
		t.Error("Config was modified by an invalid request")
	}
}
