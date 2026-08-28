package handlers

import (
	"database/sql"
	"net/http"
	"net/url"
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

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Host != r.Host {
			return false
		}
		expectedScheme := "http"
		if r.TLS != nil {
			expectedScheme = "https"
		}
		return parsed.Scheme == expectedScheme
	},
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
}

// New creates a new handlers instance
func New(cfg *config.Config, healthState *health.State, wsHub *ws.Hub, db *sql.DB, notifier *notification.Service, engines func() []*syncpkg.Engine, bwManager *syncpkg.BandwidthManager) *Handlers {
	// Load auth settings from env
	AuthEnabled = os.Getenv("AUTH_ENABLED") == "true"
	AdminUser = os.Getenv("ADMIN_USER")
	AdminPass = os.Getenv("ADMIN_PASS")

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
