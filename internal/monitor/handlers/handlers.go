package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"log"
	"net/http"
	"os"

	"sync"
	"time"

	"github.com/gorilla/websocket"

	"schnorarr/internal/monitor/config"
	"schnorarr/internal/monitor/health"
	"schnorarr/internal/monitor/notification"
	ws "schnorarr/internal/monitor/websocket"
	syncpkg "schnorarr/internal/sync"
)

var (
	AuthEnabled bool
	AdminUser   string
	AdminPass   string
)

// legacyDefaultAdminPass was shipped as the default ADMIN_PASS and is rejected.
const legacyDefaultAdminPass = "schnorarr"

var upgrader = websocket.Upgrader{
	CheckOrigin: sameOrigin,
}

type Session struct {
	User    string
	Expires time.Time
}

// Handlers contains all HTTP route handlers
type Handlers struct {
	config         *config.Config
	healthState    *health.State
	wsHub          *ws.Hub
	db             *sql.DB
	notifier       *notification.Service
	engineProvider func() []*syncpkg.Engine
	bwManager      *syncpkg.BandwidthManager
	sessions       map[string]Session
	sessionMu      sync.RWMutex
	loginLimiter   loginLimiter
}

// New creates a new handlers instance
func New(cfg *config.Config, healthState *health.State, wsHub *ws.Hub, db *sql.DB, notifier *notification.Service, engines func() []*syncpkg.Engine, bwManager *syncpkg.BandwidthManager) *Handlers {
	// Load auth settings from env. Auth is on unless explicitly disabled.
	AuthEnabled = os.Getenv("AUTH_ENABLED") != "false"
	if !AuthEnabled {
		log.Printf("WARNING: dashboard authentication is disabled (AUTH_ENABLED=false); anyone who can reach the dashboard can control sync")
	}
	AdminUser = os.Getenv("ADMIN_USER")
	if AdminUser == "" {
		AdminUser = "admin"
	}
	AdminPass = os.Getenv("ADMIN_PASS")
	if AuthEnabled && (AdminPass == "" || AdminPass == legacyDefaultAdminPass) {
		// Never fall back to a well-known password: generate one for this run.
		AdminPass = generatePassword()
		log.Printf("WARNING: ADMIN_PASS is unset or uses the old default; generated a temporary dashboard password for user %q: %s (set ADMIN_PASS to choose your own)", AdminUser, AdminPass)
	}

	return &Handlers{
		config:         cfg,
		healthState:    healthState,
		wsHub:          wsHub,
		db:             db,
		notifier:       notifier,
		engineProvider: engines,
		bwManager:      bwManager,
		sessions:       make(map[string]Session),
	}
}

// GetUser returns the username for the current request
func (h *Handlers) GetUser(r *http.Request) string {
	cookie, err := r.Cookie("schnorarr_session")
	if err != nil {
		return "unknown"
	}

	h.sessionMu.RLock()
	session, ok := h.sessions[cookie.Value]
	h.sessionMu.RUnlock()

	if !ok {
		return "unknown"
	}

	if time.Now().After(session.Expires) {
		h.sessionMu.Lock()
		delete(h.sessions, cookie.Value)
		h.sessionMu.Unlock()
		return "unknown"
	}

	return session.User
}

func generatePassword() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		log.Fatalf("failed to generate dashboard password: %v", err)
	}
	return hex.EncodeToString(b)
}
