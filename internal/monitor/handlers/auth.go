package handlers

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"html/template"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"schnorarr/internal/ui"
)

// auth middleware
func (h *Handlers) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if csrfProtection.Check(r) != nil {
			http.Error(w, "Cross-origin request rejected", http.StatusForbidden)
			return
		}
		if !AuthEnabled {
			next(w, r)
			return
		}

		// Check for session cookie
		cookie, err := r.Cookie("schnorarr_session")
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		h.sessionMu.RLock()
		session, exists := h.sessions[cookie.Value]
		h.sessionMu.RUnlock()

		if !exists || time.Now().After(session.Expires) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

// LoginPage handler
func (h *Handlers) LoginPage(w http.ResponseWriter, r *http.Request) {
	data := struct{ Error string }{Error: ""}
	t, err := template.ParseFS(ui.TemplateFS, "web/templates/login.html")
	if err != nil {
		http.Error(w, "Template Error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := t.Execute(w, data); err != nil {
		log.Printf("LoginPage Execute Error: %v", err)
	}
}

// Login handler processes credentials
func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if csrfProtection.Check(r) != nil {
		http.Error(w, "Cross-origin request rejected", http.StatusForbidden)
		return
	}
	ip := clientIP(r)
	if h.loginLimiter.blocked(ip) {
		h.renderLogin(w, http.StatusTooManyRequests, "Too many failed attempts, try again later")
		return
	}

	user := r.FormValue("username")
	pass := r.FormValue("password")

	// Constant-time compares; evaluate both to avoid short-circuit timing.
	userOK := constantTimeEqual(user, AdminUser)
	passOK := constantTimeEqual(pass, AdminPass)
	if userOK && passOK {
		h.loginLimiter.reset(ip)
		// Generate crypto-random token
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			http.Error(w, "Internal Server Error", 500)
			return
		}
		token := hex.EncodeToString(b)
		expiry := time.Now().Add(24 * time.Hour)

		now := time.Now()
		h.sessionMu.Lock()
		// Sessions only grow on login, so sweeping expired ones here bounds the map.
		for t, s := range h.sessions {
			if now.After(s.Expires) {
				delete(h.sessions, t)
			}
		}
		h.sessions[token] = Session{User: user, Expires: expiry}
		h.sessionMu.Unlock()

		http.SetCookie(w, &http.Cookie{
			Name:     "schnorarr_session",
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			Secure:   isHTTPS(r), // Browsers drop Secure cookies on plain http:// LAN deployments
			SameSite: http.SameSiteStrictMode,
			Expires:  expiry,
		})
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	h.loginLimiter.fail(ip)
	h.renderLogin(w, http.StatusOK, "Invalid credentials")
}

// renderLogin re-renders the login page with an error message.
func (h *Handlers) renderLogin(w http.ResponseWriter, status int, msg string) {
	data := struct{ Error string }{Error: msg}
	t, err := template.ParseFS(ui.TemplateFS, "web/templates/login.html")
	if err != nil {
		http.Error(w, "Template Error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(status)
	if err := t.Execute(w, data); err != nil {
		log.Printf("Login Execute Error: %v", err)
	}
}

// Logout handler
func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	// Revoke the session server-side, not just in the browser.
	if cookie, err := r.Cookie("schnorarr_session"); err == nil {
		h.sessionMu.Lock()
		delete(h.sessions, cookie.Value)
		h.sessionMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "schnorarr_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

const (
	loginMaxFailures = 5
	loginWindow      = 15 * time.Minute
)

// loginLimiter blocks an IP after repeated failed logins within loginWindow.
type loginLimiter struct {
	mu       sync.Mutex
	failures map[string]loginFailures
}

type loginFailures struct {
	count int
	first time.Time
}

func (l *loginLimiter) blocked(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	f, ok := l.failures[ip]
	if !ok {
		return false
	}
	if time.Since(f.first) > loginWindow {
		delete(l.failures, ip)
		return false
	}
	return f.count >= loginMaxFailures
}

func (l *loginLimiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.failures == nil {
		l.failures = make(map[string]loginFailures)
	}
	now := time.Now()
	for k, f := range l.failures {
		if now.Sub(f.first) > loginWindow {
			delete(l.failures, k)
		}
	}
	f := l.failures[ip]
	if f.count == 0 {
		f.first = now
	}
	f.count++
	l.failures[ip] = f
}

func (l *loginLimiter) reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, ip)
}

func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// isHTTPS reports whether the client connection is HTTPS, directly or via a TLS proxy.
func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// csrfProtection rejects cross-origin state-changing browser requests
// (Sec-Fetch-Site, falling back to Origin vs Host).
var csrfProtection = http.NewCrossOriginProtection()

// sameOrigin applies the same rule to the WebSocket handshake, which is a GET.
// Requests without Origin or Sec-Fetch-Site headers (non-browser clients) are allowed.
func sameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		return site == "same-origin" || site == "none"
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

// requirePOST rejects state-changing requests that are not POST.
func requirePOST(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	return true
}
