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

	"schnorarr/internal/internalapi"
	"schnorarr/internal/monitor/config"
	"schnorarr/internal/monitor/database"
	"schnorarr/internal/monitor/handlers"
	"schnorarr/internal/monitor/health"
	"schnorarr/internal/monitor/notification"
	"schnorarr/internal/monitor/scheduler"
	"schnorarr/internal/monitor/tailer"
	ws "schnorarr/internal/monitor/websocket"
	"schnorarr/internal/security"
	syncpkg "schnorarr/internal/sync"
	"schnorarr/internal/ui"
	"sync"
)

type App struct {
	Config      *config.Config
	HealthState *health.State
	WSHub       *ws.Hub
	Notifier    *notification.Service
	SyncEngines []*syncpkg.Engine
	BWManager   *syncpkg.BandwidthManager
	engineMu    sync.RWMutex
	dataRoot    *os.Root
}

func New() (*App, error) {
	if err := security.ValidateEnvironment(os.Getenv); err != nil {
		return nil, fmt.Errorf("invalid security configuration: %w", err)
	}
	dataRoot, err := os.OpenRoot(configuredDataRoot(os.Getenv))
	if err != nil {
		return nil, fmt.Errorf("opening data root: %w", err)
	}
	cfg := config.Load()
	if err := database.Init(); err != nil {
		_ = dataRoot.Close()
		return nil, fmt.Errorf("db init failed: %w", err)
	}
	initialBps := int64(0)
	if cfg.BwlimitMbps != nil {
		if bps, err := config.MbpsToBps(*cfg.BwlimitMbps); err == nil {
			initialBps = bps
		} else {
			log.Printf("Ignoring persisted bwlimit_mbps: %v", err)
		}
	}
	app := &App{
		Config: cfg, HealthState: health.New(), WSHub: ws.New(),
		Notifier:  notification.New(cfg.DiscordWebhook, cfg.TelegramToken, cfg.TelegramChatID),
		BWManager: syncpkg.NewBandwidthManager(initialBps),
		dataRoot:  dataRoot,
	}

	// Load persisted settings
	override := database.GetSetting("sender_override", "false")
	app.HealthState.SetSenderOverride(override == "true")

	// Setup structured logging
	wsWriter := ws.NewLogWriter(app.WSHub)
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
		go a.startSenderServices()
		sched := scheduler.New(a.Config, a.BWManager)
		go sched.Start()
	}

	h := handlers.New(a.Config, a.HealthState, a.WSHub, database.DB, a.Notifier, a.GetSyncEngines, a.BWManager)
	apiToken, _ := internalapi.Token(os.Getenv) // validated in New
	mux := http.NewServeMux()
	mux.HandleFunc("/", h.Index)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(ui.StaticFS))))
	mux.Handle("/health", internalapi.RequireToken(apiToken, http.HandlerFunc(h.Health)))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
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
	mux.HandleFunc("/api/settings/bwlimit", h.UpdateBwlimit)

	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			h.Login(w, r)
		} else {
			h.LoginPage(w, r)
		}
	})
	mux.HandleFunc("/logout", h.Logout)

	// Receiver machine API uses a credential distinct from browser sessions.
	mux.Handle("/api/manifest", internalapi.RequireToken(apiToken, http.HandlerFunc(a.ManifestHandler)))
	mux.Handle("/api/delete", internalapi.RequireToken(apiToken, http.HandlerFunc(a.DeleteHandler)))
	mux.Handle("/api/stat", internalapi.RequireToken(apiToken, http.HandlerFunc(a.StatHandler)))
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

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
	log.Printf("Monitor starting with TLS on port %s", port)
	return server.ListenAndServeTLS(os.Getenv("TLS_CERT_FILE"), os.Getenv("TLS_KEY_FILE"))
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

func (a *App) GetSyncEngines() []*syncpkg.Engine {
	a.engineMu.RLock()
	defer a.engineMu.RUnlock()
	return a.SyncEngines
}
