package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"schnorarr/internal/sync"
)

func TestHandlers_Login(t *testing.T) {
	// Setup
	_ = os.Setenv("AUTH_ENABLED", "true")
	_ = os.Setenv("ADMIN_USER", "admin")
	_ = os.Setenv("ADMIN_PASS", "password")
	defer func() {
		_ = os.Unsetenv("AUTH_ENABLED")
		_ = os.Unsetenv("ADMIN_USER")
		_ = os.Unsetenv("ADMIN_PASS")
	}()

	h := New(nil, nil, nil, nil, nil, nil, nil)

	// Case 1: Success
	form := url.Values{}
	form.Add("username", "admin")
	form.Add("password", "password")
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()

	h.Login(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Expected StatusSeeOther, got %d", resp.StatusCode)
	}

	cookies := resp.Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "schnorarr_session" {
			sessionCookie = c
			break
		}
	}

	if sessionCookie == nil {
		t.Fatal("Expected session cookie to be set")
	}
	if !sessionCookie.HttpOnly {
		t.Error("Expected cookie to be HttpOnly")
	}
	if !sessionCookie.Secure {
		t.Error("Expected cookie to be Secure behind HTTPS")
	}
	if sessionCookie.SameSite != http.SameSiteStrictMode {
		t.Error("Expected cookie to be SameSite=Strict")
	}

	// Verify session storage
	h.sessionMu.RLock()
	session, exists := h.sessions[sessionCookie.Value]
	h.sessionMu.RUnlock()

	if !exists {
		t.Error("Session not found in store")
	}
	if session.User != "admin" {
		t.Errorf("Expected user admin, got %s", session.User)
	}

	// Case 2: Invalid Credentials
	form = url.Values{}
	form.Add("username", "admin")
	form.Add("password", "wrong")
	req = httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()

	h.Login(w, req)

	if w.Result().StatusCode != http.StatusOK {
		t.Errorf("Expected StatusOK (login page render), got %d", w.Result().StatusCode)
	}
}

func TestHandlers_AuthMiddleware(t *testing.T) {
	_ = os.Setenv("AUTH_ENABLED", "true")
	defer func() { _ = os.Unsetenv("AUTH_ENABLED") }()

	h := New(nil, nil, nil, nil, nil, nil, nil)

	// Create a valid session
	token := "valid_token"
	h.sessionMu.Lock()
	h.sessions[token] = Session{User: "user", Expires: time.Now().Add(1 * time.Hour)}
	h.sessionMu.Unlock()

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	authenticatedHandler := h.auth(nextHandler)

	// Case 1: No Cookie
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	authenticatedHandler.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusSeeOther {
		t.Errorf("Expected redirect for missing cookie, got %d", w.Result().StatusCode)
	}

	// Case 2: Invalid Cookie
	req = httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "schnorarr_session", Value: "invalid"})
	w = httptest.NewRecorder()
	authenticatedHandler.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusSeeOther {
		t.Errorf("Expected redirect for invalid cookie, got %d", w.Result().StatusCode)
	}

	// Case 3: Valid Cookie
	req = httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "schnorarr_session", Value: token})
	w = httptest.NewRecorder()
	authenticatedHandler.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Errorf("Expected OK for valid cookie, got %d", w.Result().StatusCode)
	}
}

func TestHandlers_GetUser(t *testing.T) {
	h := New(nil, nil, nil, nil, nil, nil, nil)
	token := "user_token"
	h.sessionMu.Lock()
	h.sessions[token] = Session{User: "testuser", Expires: time.Now().Add(time.Hour)}
	h.sessionMu.Unlock()

	// Case 1: Valid User
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "schnorarr_session", Value: token})
	user := h.GetUser(req)
	if user != "testuser" {
		t.Errorf("Expected testuser, got %s", user)
	}

	// Case 2: No Cookie
	req = httptest.NewRequest("GET", "/", nil)
	user = h.GetUser(req)
	if user != "unknown" {
		t.Errorf("Expected unknown, got %s", user)
	}
}

func loginRequest(user, pass string) *http.Request {
	form := url.Values{"username": {user}, "password": {pass}}
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func TestHandlers_LoginPlainHTTPCookieNotSecure(t *testing.T) {
	t.Setenv("ADMIN_PASS", "password")
	h := New(nil, nil, nil, nil, nil, nil, nil)
	w := httptest.NewRecorder()
	h.Login(w, loginRequest("admin", "password"))
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Secure {
		t.Fatalf("expected one non-Secure session cookie on plain HTTP, got %+v", cookies)
	}
}

func TestHandlers_AuthDefaults(t *testing.T) {
	t.Setenv("AUTH_ENABLED", "")
	t.Setenv("ADMIN_PASS", "")
	New(nil, nil, nil, nil, nil, nil, nil)
	if !AuthEnabled {
		t.Error("auth must be enabled by default")
	}
	if AdminPass == "" || AdminPass == legacyDefaultAdminPass {
		t.Errorf("expected a generated password, got %q", AdminPass)
	}
	t.Setenv("ADMIN_PASS", legacyDefaultAdminPass)
	New(nil, nil, nil, nil, nil, nil, nil)
	if AdminPass == legacyDefaultAdminPass {
		t.Error("legacy default password must be rejected")
	}
	t.Setenv("AUTH_ENABLED", "false")
	New(nil, nil, nil, nil, nil, nil, nil)
	if AuthEnabled {
		t.Error("AUTH_ENABLED=false must disable auth")
	}
}

func TestHandlers_LoginRateLimit(t *testing.T) {
	t.Setenv("AUTH_ENABLED", "true")
	t.Setenv("ADMIN_PASS", "password")
	h := New(nil, nil, nil, nil, nil, nil, nil)
	for i := 0; i < loginMaxFailures; i++ {
		w := httptest.NewRecorder()
		h.Login(w, loginRequest("admin", "wrong"))
		if w.Code != http.StatusOK {
			t.Fatalf("attempt %d: got %d", i, w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.Login(w, loginRequest("admin", "password"))
	if w.Code != http.StatusTooManyRequests || len(w.Result().Cookies()) != 0 {
		t.Fatalf("expected lockout after %d failures, got %d", loginMaxFailures, w.Code)
	}
}

func TestHandlers_LogoutRevokesSession(t *testing.T) {
	t.Setenv("AUTH_ENABLED", "true")
	h := New(nil, nil, nil, nil, nil, nil, nil)
	h.sessions["tok"] = Session{User: "admin", Expires: time.Now().Add(time.Hour)}
	h.sessions["old"] = Session{User: "admin", Expires: time.Now().Add(-time.Hour)}
	req := httptest.NewRequest("GET", "/logout", nil)
	req.AddCookie(&http.Cookie{Name: "schnorarr_session", Value: "tok"})
	h.Logout(httptest.NewRecorder(), req)
	if _, ok := h.sessions["tok"]; ok {
		t.Error("session still valid after logout")
	}
	t.Setenv("ADMIN_PASS", "password")
	h2 := New(nil, nil, nil, nil, nil, nil, nil)
	h2.sessions["old"] = Session{User: "admin", Expires: time.Now().Add(-time.Hour)}
	h2.Login(httptest.NewRecorder(), loginRequest("admin", "password"))
	if _, ok := h2.sessions["old"]; ok {
		t.Error("expired session not swept on login")
	}
}

func TestHandlers_CSRFAndMethod(t *testing.T) {
	t.Setenv("AUTH_ENABLED", "false")
	h := New(nil, nil, nil, nil, nil, func() []*sync.Engine { return nil }, nil)

	w := httptest.NewRecorder()
	h.GlobalPause(w, httptest.NewRequest("GET", "/pause", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET on mutating route returned %d", w.Code)
	}

	req := httptest.NewRequest("POST", "http://dash.local/settings/sync-mode", strings.NewReader("mode=auto"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	w = httptest.NewRecorder()
	h.UpdateSyncMode(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("cross-site POST returned %d", w.Code)
	}

	req = httptest.NewRequest("POST", "http://dash.local/pause", nil)
	req.Header.Set("Origin", "https://evil.example")
	w = httptest.NewRecorder()
	h.GlobalPause(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("foreign Origin POST returned %d", w.Code)
	}
}

func TestSameOriginWebSocket(t *testing.T) {
	cases := []struct {
		origin, site string
		want         bool
	}{
		{"", "", true},
		{"http://dash.local", "", true},
		{"http://evil.example", "", false},
		{"", "cross-site", false},
		{"http://dash.local", "same-origin", true},
	}
	for _, tc := range cases {
		req := httptest.NewRequest("GET", "http://dash.local/ws", nil)
		if tc.origin != "" {
			req.Header.Set("Origin", tc.origin)
		}
		if tc.site != "" {
			req.Header.Set("Sec-Fetch-Site", tc.site)
		}
		if got := sameOrigin(req); got != tc.want {
			t.Errorf("origin=%q site=%q: got %v want %v", tc.origin, tc.site, got, tc.want)
		}
	}
}

func TestCSVCell(t *testing.T) {
	for in, want := range map[string]string{"=cmd()": "'=cmd()", "+1": "'+1", "@x": "'@x", "-2": "'-2", "movie.mkv": "movie.mkv", "": ""} {
		if got := csvCell(in); got != want {
			t.Errorf("csvCell(%q) = %q, want %q", in, got, want)
		}
	}
}
