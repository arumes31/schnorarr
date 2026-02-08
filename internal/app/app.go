package app

import (
	"fmt"
	"io"
	"log"
	"log/slog"
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

type App struct {
	Config      *config.Config
	HealthState *health.State
	WSHub       *websocket.Hub
	Notifier    *notification.Service
	SyncEngines []*sync.Engine
}

func New() (*App, error) {
	cfg := config.Load()
	if err := database.Init(); err != nil {
		return nil, fmt.Errorf("db init failed: %w", err)
	}
	app := &App{
		Config: cfg, HealthState: health.New(), WSHub: websocket.New(),
		Notifier: notification.New(cfg.DiscordWebhook, cfg.TelegramToken, cfg.TelegramChatID),
	}

	// Setup structured logging
	wsWriter := websocket.NewLogWriter(app.WSHub)
	multiWriter := io.MultiWriter(os.Stdout, wsWriter)
	logger := slog.New(slog.NewJSONHandler(multiWriter, nil))
	slog.SetDefault(logger)
	log.SetOutput(multiWriter) // Keep standard log output redirected for legacy calls
	return app, nil
}

func (a *App) Start(port string) error {
	database.StartTrafficManager()
	a.startLogTailer()
	go a.startHousekeeping()
	if os.Getenv("MODE") == "sender" {
		a.startSenderServices()
	}

	h := handlers.New(a.Config, a.HealthState, a.WSHub, database.DB, a.Notifier, a.SyncEngines)
	mux := http.NewServeMux()
	mux.HandleFunc("/", h.Index)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(ui.StaticFS))))
	mux.HandleFunc("/health", h.Health)
	mux.HandleFunc("/history", h.History)
	mux.HandleFunc("/history/export", h.ExportHistory)
	mux.HandleFunc("/sync", h.ManualSync)
	mux.HandleFunc("/pause", h.GlobalPause)
	mux.HandleFunc("/resume", h.GlobalResume)
	mux.HandleFunc("/ws", h.WebSocket)
	mux.HandleFunc("/test-notify", h.TestNotify)
	mux.HandleFunc("/settings/scheduler", h.SetScheduler)
	mux.HandleFunc("/settings/notifications", h.SetNotifications)
	mux.HandleFunc("/settings/sync-mode", h.UpdateSyncMode)
	mux.HandleFunc("/settings/auto-approve", h.UpdateAutoApprove)
	mux.HandleFunc("/settings/sender-override", h.UpdateSenderOverride)

	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			h.Login(w, r)
		} else {
			h.LoginPage(w, r)
		}
	})
	mux.HandleFunc("/logout", h.Logout)

	// Engine API
	mux.HandleFunc("/api/manifest", a.ManifestHandler)
	mux.HandleFunc("/api/delete", a.DeleteHandler)
	mux.HandleFunc("/api/stat", a.StatHandler)
	mux.HandleFunc("/api/engines/bulk", h.BulkAction)
	mux.HandleFunc("/api/engine/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/preview") {
			h.EnginePreview(w, r)
		} else if strings.HasSuffix(r.URL.Path, "/alias") {
			h.EngineAlias(w, r)
		} else {
			h.EngineAction(w, r)
		}
	})

	log.Printf("Monitor starting on port %s", port)
	return http.ListenAndServe(":"+port, mux)
}

func (a *App) startLogTailer() {
	logTailer := tailer.New(func(ts, act, p string, sz int64) {
		_ = database.LogEvent(ts, act, p, sz, "Legacy")
		item := database.HistoryItem{Time: ts, Action: act, Path: p, Size: database.FormatBytes(sz)}
		a.WSHub.Broadcast("history", item)
		a.WSHub.Broadcast("stats", database.GetTrafficStats())
		a.WSHub.Broadcast("daily", database.GetDailyTraffic(7))
		a.HealthState.ReportSuccess(a.Notifier.Send)
	}, func(msg string) { a.HealthState.ReportError(msg, a.Notifier.Send) })
	go logTailer.Start()
}

func (a *App) startHousekeeping() {
	if err := database.PruneHistory(30); err != nil {
		log.Printf("Housekeeping error: %v", err)
	}
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		_ = database.PruneHistory(30)
	}
}
