package app

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"schnorarr/internal/monitor/config"
	"schnorarr/internal/monitor/database"
	"schnorarr/internal/monitor/handlers"
	"schnorarr/internal/monitor/health"
	"schnorarr/internal/monitor/notification"
	"schnorarr/internal/monitor/tailer"
	"schnorarr/internal/monitor/websocket"
	"schnorarr/internal/sync"
	"schnorarr/internal/ui"
)

// App encapsulates the application state
type App struct {
	Config      *config.Config
	HealthState *health.State
	WSHub       *websocket.Hub
	Notifier    *notification.Service
	SyncEngines []*sync.Engine
}

// New creates and initializes the application
func New() (*App, error) {
	// Load configuration
	cfg := config.Load()

	// Initialize database
	if err := database.Init(); err != nil {
		return nil, fmt.Errorf("database init failed: %w", err)
	}

	// Create core components
	app := &App{
		Config:      cfg,
		HealthState: health.New(),
		WSHub:       websocket.New(),
		Notifier: notification.New(
			cfg.DiscordWebhook,
			cfg.TelegramToken,
			cfg.TelegramChatID,
		),
	}

	// Setup logging redirection
	wsLogWriter := websocket.NewLogWriter(app.WSHub)
	log.SetOutput(io.MultiWriter(os.Stdout, wsLogWriter))

	return app, nil
}

// Start launches the application services and HTTP server
func (a *App) Start(port string) error {
	// Start Database Traffic Manager (Memory Buffer)
	database.StartTrafficManager()

	// Start Log Tailer
	a.startLogTailer()

	// Start Housekeeping
	go a.startHousekeeping()

	// Start Sync Engines (if in sender mode)
	if os.Getenv("MODE") == "sender" {
		a.startSenderServices()
	}

	// Setup HTTP Server
	h := handlers.New(a.Config, a.HealthState, a.WSHub, database.DB, a.Notifier, a.SyncEngines)
	mux := http.NewServeMux()

	// Register routes
	mux.HandleFunc("/", h.Index)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(ui.StaticFS))))
	mux.HandleFunc("/health", h.Health)
	mux.HandleFunc("/history", h.History)
	mux.HandleFunc("/sync", h.ManualSync)
	mux.HandleFunc("/pause", h.GlobalPause)
	mux.HandleFunc("/resume", h.GlobalResume)
	mux.HandleFunc("/ws", h.WebSocket)
	mux.HandleFunc("/test-notify", h.TestNotify)
	mux.HandleFunc("/settings/scheduler", h.SetScheduler)
	mux.HandleFunc("/settings/notifications", h.SetNotifications)
	mux.HandleFunc("/settings/sync-mode", h.UpdateSyncMode)
	mux.HandleFunc("/settings/auto-approve", h.UpdateAutoApprove)
	
	// Auth routes
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			h.Login(w, r)
		} else {
			h.LoginPage(w, r)
		}
	})
	mux.HandleFunc("/logout", h.Logout)

	// Engine API
	mux.HandleFunc("/api/engine/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/preview") {
			h.EnginePreview(w, r)
		} else {
			h.EngineAction(w, r)
		}
	})

	log.Printf("Monitor starting on port %s", port)
	return http.ListenAndServe(":"+port, mux)
}

func (a *App) startLogTailer() {
	logTailer := tailer.New(
		func(timestamp, action, path string, size int64) {
			// Log to database
			if err := database.LogEvent(timestamp, action, path, size, "Legacy"); err != nil {
				log.Printf("Failed to log event: %v", err)
			}

			// Broadcast
			item := database.HistoryItem{
				Time:   timestamp,
				Action: action,
				Path:   path,
				Size:   database.FormatBytes(size),
			}
			a.WSHub.Broadcast("history_new", item)
			a.WSHub.Broadcast("stats", database.GetTrafficStats())
			a.WSHub.Broadcast("daily", database.GetDailyTraffic(7))

			// Report success
			a.HealthState.ReportSuccess(a.Notifier.Send)
		},
		func(msg string) {
			a.HealthState.ReportError(msg, a.Notifier.Send)
		},
	)
	go logTailer.Start()
}

func (a *App) startHousekeeping() {
	// Run immediately on start
	_, _ = database.PruneHistory()

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		_, _ = database.PruneHistory()
	}
}
