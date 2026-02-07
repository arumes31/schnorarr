package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"html/template"
	"log"
	"net/http"
	"time"

	"schnorarr/internal/ui"
)

// auth middleware
func (h *Handlers) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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

	user := r.FormValue("username")
	pass := r.FormValue("password")

	if user == AdminUser && pass == AdminPass {
		// Generate crypto-random token
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			http.Error(w, "Internal Server Error", 500)
			return
		}
		token := hex.EncodeToString(b)
		expiry := time.Now().Add(24 * time.Hour)

		h.sessionMu.Lock()
		h.sessions[token] = Session{User: user, Expires: expiry}
		h.sessionMu.Unlock()

		// Cleanup expired sessions occasionally (simple probability check)
		// ... implementation omitted for brevity, or done in background task

		http.SetCookie(w, &http.Cookie{
			Name:     "schnorarr_session",
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			Secure:   true, // Require HTTPS
			SameSite: http.SameSiteLaxMode,
			Expires:  expiry,
		})
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	// Re-render login with error
	data := struct{ Error string }{Error: "Invalid credentials"}
	t, err := template.ParseFS(ui.TemplateFS, "web/templates/login.html")
	if err != nil {
		http.Error(w, "Template Error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := t.Execute(w, data); err != nil {
		log.Printf("Login Execute Error: %v", err)
	}
}

// Logout handler
func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "schnorarr_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
