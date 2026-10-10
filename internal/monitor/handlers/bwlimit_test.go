package handlers

import (
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
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

// useTempConfigPath points config.Save at a writable temp file.
func useTempConfigPath(t *testing.T) {
	t.Helper()
	old := config.ConfigPath
	config.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	t.Cleanup(func() { config.ConfigPath = old })
}

// useBrokenConfigPath points config.Save at a path inside a nonexistent
// directory so it always fails.
func useBrokenConfigPath(t *testing.T) {
	t.Helper()
	old := config.ConfigPath
	config.ConfigPath = filepath.Join(t.TempDir(), "missing", "config.json")
	t.Cleanup(func() { config.ConfigPath = old })
}

func TestHandlers_UpdateBwlimit(t *testing.T) {
	useTempConfigPath(t)
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

	// Boundary: largest int64-safe value is accepted
	w = postForm(t, h.UpdateBwlimit, "/api/settings/bwlimit", url.Values{"mbps": {strconv.Itoa(math.MaxInt64 / 125000)}})
	if w.Result().StatusCode != http.StatusOK {
		t.Errorf("Expected 200 for boundary Mbps, got %d", w.Result().StatusCode)
	}

	// Overflow: parses as int but its bytes/sec value would overflow int64
	w = postForm(t, h.UpdateBwlimit, "/api/settings/bwlimit", url.Values{"mbps": {strconv.Itoa(math.MaxInt64/125000 + 1)}})
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Errorf("Expected 400 for overflowing Mbps, got %d", w.Result().StatusCode)
	}
}

func TestHandlers_UpdateBwlimit_SaveFailure(t *testing.T) {
	useBrokenConfigPath(t)
	m := syncpkg.NewBandwidthManager(0)
	cfg := &config.Config{}
	h := New(cfg, nil, nil, nil, nil, nil, m)

	w := postForm(t, h.UpdateBwlimit, "/api/settings/bwlimit", url.Values{"mbps": {"50"}})
	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Errorf("Expected 500 when Save fails, got %d", w.Result().StatusCode)
	}
	if got := m.CurrentLimit(); got != 0 {
		t.Errorf("Manager limit changed despite Save failure: %d", got)
	}
	if cfg.BwlimitMbps != nil {
		t.Errorf("Config BwlimitMbps changed despite Save failure: %d", *cfg.BwlimitMbps)
	}
}

func TestHandlers_SetScheduler(t *testing.T) {
	useTempConfigPath(t)
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

	// Overflowing limits are rejected even though they parse as int
	for field, form := range map[string]url.Values{
		"quiet_limit": {
			"quiet_start": {"23:00"}, "quiet_end": {"07:00"},
			"quiet_limit": {strconv.Itoa(math.MaxInt64/125000 + 1)}, "normal_limit": {"100"},
		},
		"normal_limit": {
			"quiet_start": {"23:00"}, "quiet_end": {"07:00"},
			"quiet_limit": {"10"}, "normal_limit": {strconv.Itoa(math.MaxInt64/125000 + 1)},
		},
	} {
		w = postForm(t, h.SetScheduler, "/settings/scheduler", form)
		if w.Result().StatusCode != http.StatusBadRequest {
			t.Errorf("Expected 400 for overflowing %s, got %d", field, w.Result().StatusCode)
		}
	}
}

func TestHandlers_SetScheduler_SaveFailure(t *testing.T) {
	useBrokenConfigPath(t)
	cfg := &config.Config{
		QuietStart: "01:00", QuietEnd: "02:00", QuietLimit: 5, NormalLimit: 50,
	}
	h := New(cfg, nil, nil, nil, nil, nil, nil)

	w := postForm(t, h.SetScheduler, "/settings/scheduler", url.Values{
		"scheduler_enabled": {"on"},
		"quiet_start":       {"23:00"},
		"quiet_end":         {"07:00"},
		"quiet_limit":       {"10"},
		"normal_limit":      {"100"},
	})
	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Errorf("Expected 500 when Save fails, got %d", w.Result().StatusCode)
	}
	// Effective config must be restored to the pre-request state
	if cfg.SchedulerEnabled || cfg.QuietStart != "01:00" || cfg.QuietEnd != "02:00" ||
		cfg.QuietLimit != 5 || cfg.NormalLimit != 50 {
		t.Error("Config was not restored after Save failure")
	}
}
