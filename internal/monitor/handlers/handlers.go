package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/http"
	"os"

	"github.com/gorilla/websocket"

	"schnorarr/internal/monitor/config"
	"schnorarr/internal/monitor/health"
	"schnorarr/internal/monitor/notification"
	ws "schnorarr/internal/monitor/websocket"
	"schnorarr/internal/sync"
)

var (
	AuthEnabled bool
	AdminUser   string
	AdminPass   string
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Handlers contains all HTTP route handlers
type Handlers struct {
	config      *config.Config
	healthState *health.State
	wsHub       *ws.Hub
	db          *sql.DB
	notifier     *notification.Service
	engines      []*sync.Engine
	sessionToken string
}

// New creates a new handlers instance
func New(cfg *config.Config, healthState *health.State, wsHub *ws.Hub, db *sql.DB, notifier *notification.Service, engines []*sync.Engine) *Handlers {
	// Load auth settings from env
	AuthEnabled = os.Getenv("AUTH_ENABLED") == "true"
	AdminUser = os.Getenv("ADMIN_USER")
	if AdminUser == "" {
		AdminUser = "admin"
	}
	AdminPass = os.Getenv("ADMIN_PASS")
	if AdminPass == "" {
		AdminPass = "schnorarr"
	}

	// Generate random session token
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		// Fallback (should not happen)
		copy(token, []byte("fallback-secret"))
	}
	sessionToken := hex.EncodeToString(token)

	return &Handlers{
		config:       cfg,
		healthState:  healthState,
		wsHub:        wsHub,
		db:           db,
		notifier:     notifier,
		engines:      engines,
		sessionToken: sessionToken,
	}
}