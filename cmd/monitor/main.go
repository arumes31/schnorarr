package main

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"schnorarr/internal/monitor/config"
	"schnorarr/internal/monitor/database"
	"schnorarr/internal/monitor/handlers"
	"schnorarr/internal/monitor/health"
	"schnorarr/internal/monitor/notification"
	"schnorarr/internal/monitor/scheduler"
	"schnorarr/internal/monitor/tailer"
	"schnorarr/internal/monitor/websocket"
	"schnorarr/internal/sync"
)

const Port = "8080"

var syncEngines []*sync.Engine

func main() {
	// Load configuration
	cfg := config.Load()

	// Initialize database
	if err := database.Init(); err != nil {
		log.Fatalf("Database init failed: %v", err)
	}

	// Create notification service
	notifier := notification.New(
		cfg.DiscordWebhook,
		cfg.TelegramToken,
		cfg.TelegramChatID,
	)

	// Create health state tracker
	healthState := health.New()

	// Create WebSocket hub
	wsHub := websocket.New()

	// Start background services
	// 1. Rsync log tailer (still needed for monitoring existing rsync processes)
	logTailer := tailer.New(
		func(timestamp, action, path string, size int64) {
			// Log to database
			if err := database.LogEvent(timestamp, action, path, size, "Legacy"); err != nil {
				log.Printf("Failed to log event: %v", err)
			}

			// Broadcast to websocket clients
			item := database.HistoryItem{
				Time:   timestamp,
				Action: action,
				Path:   path,
				Size:   database.FormatBytes(size),
			}
			wsHub.Broadcast("history_new", item)
			wsHub.Broadcast("stats", database.GetTrafficStats())
			wsHub.Broadcast("daily", database.GetDailyTraffic(7))

			// Report success to health tracker
			healthState.ReportSuccess(notifier.Send)
		},
		func(msg string) {
			healthState.ReportError(msg, notifier.Send)
		},
	)
	go logTailer.Start()

	// 2. Start sync engines if in sender mode
	if os.Getenv("MODE") == "sender" {
		syncEngines = startSyncEngines(wsHub, healthState, notifier)

		// 3. Bandwidth scheduler
		sched := scheduler.New(cfg, func(action string) {
			if action == "reload" {
				// Bandwidth limit is now controlled directly via engine.SetBandwidthLimit
				log.Println("Bandwidth limit reload requested")
			}
		})
		go sched.Start()

		// 4. Status broadcaster for sync engines
		go startSyncStatusBroadcaster(wsHub)
	}

	// HTTP server with handlers
	h := handlers.New(cfg, healthState, wsHub, database.DB, notifier, syncEngines)

	// Register routes
	http.HandleFunc("/", h.Index)
	http.HandleFunc("/history", h.History)
	http.HandleFunc("/sync", h.ManualSync)
	http.HandleFunc("/pause", h.GlobalPause)
	http.HandleFunc("/resume", h.GlobalResume)
	http.HandleFunc("/ws", h.WebSocket)
	http.HandleFunc("/test-notify", h.TestNotify)
	http.HandleFunc("/settings/scheduler", h.SetScheduler)
	http.HandleFunc("/settings/notifications", h.SetNotifications)
	http.HandleFunc("/settings/sync-mode", h.UpdateSyncMode)

	// New engine-specific routes
	http.HandleFunc("/api/engine/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/preview") {
			h.EnginePreview(w, r)
		} else {
			h.EngineAction(w, r)
		}
	})

	log.Printf("Monitor starting on port %s", Port)
	log.Fatal(http.ListenAndServe(":"+Port, nil))
}

// startSyncEngines creates and starts sync engines based on environment config
func startSyncEngines(wsHub *websocket.Hub, healthState *health.State, notifier *notification.Service) []*sync.Engine {
	var engines []*sync.Engine

	// Check for multi-sync configuration (SYNC_1, SYNC_2, etc.)
	for i := 1; i <= 10; i++ {
		src := os.Getenv("SYNC_" + strconv.Itoa(i) + "_SOURCE")
		tgt := os.Getenv("SYNC_" + strconv.Itoa(i) + "_TARGET")
		rule := os.Getenv("SYNC_" + strconv.Itoa(i) + "_RULE")

		if src == "" || tgt == "" {
			continue
		}

		// Resolve target path if it's an rsync string
		resolvedTgt := sync.ResolveTargetPath(tgt, os.Getenv("DEST_HOST"), os.Getenv("DEST_MODULE"))
		if resolvedTgt != tgt {
			log.Printf("Resolved target %s to local path %s", tgt, resolvedTgt)
		}

		log.Printf("Configuring sync engine %d: %s -> %s (rule: %s)", i, src, resolvedTgt, rule)

		// Determine exclusion patterns based on rule
		var excludePatterns []string
		if rule == "flat" {
			// Flat sync: minimal exclusions
			excludePatterns = []string{".git", ".DS_Store", "Thumbs.db"}
		} else {
			// Series sync: protect receiver-only directories
			excludePatterns = []string{".git", ".DS_Store", "Thumbs.db"}
			// Note: Series protection is handled in comparison logic, not exclusions
		}

		// Get bandwidth limit from environment (legacy) or config file
		bwlimitBytes := int64(0)
		if bwStr := os.Getenv("BWLIMIT_MBPS"); bwStr != "" {
			if bwlimitMBps, err := strconv.ParseInt(bwStr, 10, 64); err == nil && bwlimitMBps > 0 {
				bwlimitBytes = bwlimitMBps * 125000 // MB/s to bytes/s
			}
		}

		// Get poll interval
		pollInterval := 10 * time.Second
		if pollStr := os.Getenv("POLL_INTERVAL"); pollStr != "" {
			if pi, err := strconv.Atoi(pollStr); err == nil && pi > 0 {
				pollInterval = time.Duration(pi) * time.Second
			}
		}

		// Create sync engine
		engine := sync.NewEngine(sync.SyncConfig{
			ID:              strconv.Itoa(i),
			SourceDir:       src,
			TargetDir:       resolvedTgt,
			Rule:            rule,
			ExcludePatterns: excludePatterns,
			BandwidthLimit:  bwlimitBytes,
			WatchInterval:   3 * time.Hour, // Full scan every 3 hours
			PollInterval:    pollInterval,  // Fast source-only polling
			DryRunFunc: func() bool {
				return database.GetSetting("sync_mode", "dry") == "dry"
			},
			OnSyncEvent: func(timestamp, action, path string, size int64) {
				// Log to database
				engineID := strconv.Itoa(i)
				if err := database.LogEvent(timestamp, action, path, size, engineID); err != nil {
					log.Printf("Failed to log event: %v", err)
				}

				// Broadcast to websocket clients
				item := database.HistoryItem{
					Time:   timestamp,
					Action: action,
					Path:   path,
					Size:   database.FormatBytes(size),
				}
				wsHub.Broadcast("history_new", item)
				wsHub.Broadcast("stats", database.GetTrafficStats())
				wsHub.Broadcast("daily", database.GetDailyTraffic(7))

				// Report success to health tracker
				healthState.ReportSuccess(notifier.Send)
			},
			OnError: func(msg string) {
				healthState.ReportError(msg, notifier.Send)
			},
		})

		// Start the engine
		if err := engine.Start(); err != nil {
			log.Printf("Failed to start sync engine %d: %v", i, err)
			notifier.Send("Failed to start sync engine "+strconv.Itoa(i)+": "+err.Error(), "ERROR")
			continue
		}

		engines = append(engines, engine)
	}

	if len(engines) == 0 {
		log.Println("WARNING: No sync engines configured. Set SYNC_X_SOURCE and SYNC_X_TARGET environment variables.")
	} else {
		log.Printf("Started %d sync engine(s)", len(engines))
	}

	return engines
}

// makeAuthSyncHandler wraps sync engine control calls with auth
func makeAuthSyncHandler(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Auth check - get from env
		authEnabled := os.Getenv("AUTH_ENABLED") == "true"
		if authEnabled {
			adminUser := os.Getenv("ADMIN_USER")
			if adminUser == "" {
				adminUser = "admin"
			}
			adminPass := os.Getenv("ADMIN_PASS")
			if adminPass == "" {
				adminPass = "schnorarr"
			}

			user, pass, ok := r.BasicAuth()
			if !ok || user != adminUser || pass != adminPass {
				w.Header().Set("WWW-Authenticate", `Basic realm="Mirrqr Dashboard"`)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}

		// Control all sync engines
		for _, engine := range syncEngines {
			switch strings.ToLower(action) {
			case "pause":
				engine.Pause()
			case "resume":
				engine.Resume()
			}
		}

		log.Printf("Sync engines %s", action)
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

// makeBandwidthHandler wraps bandwidth limit handler with engine update logic
func makeBandwidthHandler(h *handlers.Handlers) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Call the original handler
		h.SetBandwidthLimit(w, r)

		// Update bandwidth limit for all engines
		if bwStr := r.FormValue("bwlimit"); bwStr != "" {
			if bwlimitMBps, err := strconv.ParseInt(bwStr, 10, 64); err == nil && bwlimitMBps > 0 {
				bwlimitBytes := bwlimitMBps * 125000 // MB/s to bytes/s
				for _, engine := range syncEngines {
					engine.SetBandwidthLimit(bwlimitBytes)
				}
				log.Printf("Updated bandwidth limit: %d MB/s", bwlimitMBps)
			}
		}
	}
}

// startSyncStatusBroadcaster broadcasts sync engine status every second
func startSyncStatusBroadcaster(wsHub *websocket.Hub) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		// Build status summary
		status := "No sync engines active."
		progress := "System Idle"
		state := "ACTIVE"

		if len(syncEngines) > 0 {
			var sb strings.Builder
			allPaused := true
			for _, engine := range syncEngines {
				sb.WriteString(engine.GetStatus() + "\n")
				if !engine.IsPaused() {
					allPaused = false
				}
			}
			status = sb.String()

			if allPaused {
				state = "PAUSED"
				progress = "Sync Paused"
			} else {
				progress = "Monitoring..."
			}
		}

		wsHub.Broadcast("progress", map[string]interface{}{
			"status": status,
			"speed":  "0 B/s",
			"eta":    "Done",
			"state":  state,
		})

		wsHub.Broadcast("sync_status", map[string]interface{}{
			"status":  progress,
			"engines": len(syncEngines),
		})
	}
}
