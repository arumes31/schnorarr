package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
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

	h := New(nil, nil, nil, nil, nil, nil)

	// Case 1: Success
	form := url.Values{}
	form.Add("username", "admin")
	form.Add("password", "password")
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
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
		t.Error("Expected cookie to be Secure")
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

	h := New(nil, nil, nil, nil, nil, nil)

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
	h := New(nil, nil, nil, nil, nil, nil)
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
