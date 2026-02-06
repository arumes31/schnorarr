package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"schnorarr/internal/monitor/config"
	"schnorarr/internal/monitor/database"
	"schnorarr/internal/monitor/health"
	"schnorarr/internal/monitor/notification"
	ws "schnorarr/internal/monitor/websocket"
	"schnorarr/internal/sync"
	"schnorarr/internal/ui"
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
	notifier    *notification.Service
	engines     []*sync.Engine
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

	return &Handlers{
		config:      cfg,
		healthState: healthState,
		wsHub:       wsHub,
		db:          db,
		notifier:    notifier,
		engines:     engines,
	}
}

// auth middleware
func (h *Handlers) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !AuthEnabled {
			next(w, r)
			return
		}

		user, pass, ok := r.BasicAuth()
		if !ok || user != AdminUser || pass != AdminPass {
			w.Header().Set("WWW-Authenticate", `Basic realm="Mirrqr Dashboard"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// Index handler
func (h *Handlers) Index(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		healthy, lastErr := h.healthState.GetStatus()

		progress, currentSpeed, eta, queued, status := h.GetProgressInfo()

		// Determine overall state for the badge
		state := "ACTIVE"
		if !healthy {
			state = "CRITICAL"
		} else if len(h.engines) > 0 {
			allPaused := true
			for _, e := range h.engines {
				if !e.IsPaused() {
					allPaused = false
					break
				}
			}
			if allPaused {
				state = "PAUSED"
			}
		}

		// Prepare engine data for the template
		type EngineView struct {
			ID, Source, Target         string
			Status, State              string
			IsPaused                   bool
			LastSync                   string
			TrafficToday, TrafficTotal string
			Rule                       string
			PendingDeletions           int
			WaitingForApproval         bool
		}
		var engineViews []EngineView
		for _, engine := range h.engines {
			cfg := engine.GetConfig()
			stats := database.GetEngineTrafficStats(cfg.ID)
			engineViews = append(engineViews, EngineView{
				ID:                 cfg.ID,
				Source:             cfg.SourceDir,
				Target:             cfg.TargetDir,
				Status:             engine.GetStatus(),
				State:              "ACTIVE", // Default to ACTIVE
				IsPaused:           engine.IsPaused(),
				LastSync:           engine.GetLastSyncTime().Format("15:04:05"),
				TrafficToday:       database.FormatBytes(stats.Today),
				TrafficTotal:       database.FormatBytes(stats.Total),
				Rule:               cfg.Rule,
				PendingDeletions:   len(engine.GetPendingDeletions()),
				WaitingForApproval: engine.IsWaitingForApproval(),
			})
			if engine.IsPaused() {
				engineViews[len(engineViews)-1].State = "PAUSED"
			}
			if engine.IsWaitingForApproval() {
				engineViews[len(engineViews)-1].State = "WAITING_APPROVAL"
			}
		}

		traffic := database.GetTrafficStats()
		history, _ := database.GetHistory(15, "")

		data := struct {
			Time, LastErrorMsg, Progress, LsyncdStatus string
			Healthy, ReceiverHealthy                   bool
			State                                      string
			Queued                                     int
			History                                    []database.HistoryItem
			TrafficToday, TrafficTotal                 string
			CurrentSpeed                               string
			ETA                                        string
			SyncMode                                   string
			Engines                                    []EngineView
		}{
			Time:         time.Now().Format("2006-01-02 15:04:05"),
			Healthy:      healthy,
			State:        state,
			LastErrorMsg: lastErr,
			Progress:     progress,
			LsyncdStatus: status,
			Queued:       queued,
			History:      history,
			TrafficToday: database.FormatBytes(traffic.Today),
			TrafficTotal: database.FormatBytes(traffic.Total),
			CurrentSpeed: currentSpeed,
			ETA:          eta,
			SyncMode:     database.GetSetting("sync_mode", "dry"),
			Engines:      engineViews,
			ReceiverHealthy: func() bool {
				h, _ := h.healthState.GetReceiverStatus()
				return h
			}(),
		}

		funcMap := template.FuncMap{
			"lower": strings.ToLower,
		}

		t, err := template.New("index.html").Funcs(funcMap).ParseFS(ui.TemplateFS, "web/templates/index.html")
		if err != nil {
			http.Error(w, "Template Error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if err := t.Execute(w, data); err != nil {
			log.Printf("Template Execute Error: %v", err)
		}
	})(w, r)
}

// History handler
func (h *Handlers) History(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		history, _ := database.GetHistory(0, query)

		data := struct {
			History []database.HistoryItem
			Query   string
		}{
			History: history,
			Query:   query,
		}

		funcMap := template.FuncMap{
			"lower": strings.ToLower,
		}

		t, err := template.New("history.html").Funcs(funcMap).ParseFS(ui.TemplateFS, "web/templates/history.html")
		if err != nil {
			http.Error(w, "Template Error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if err := t.Execute(w, data); err != nil {
			log.Printf("Template Execute Error: %v", err)
		}
	})(w, r)
}

// Health handler
func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	status := "healthy"
	if err := json.NewEncoder(w).Encode(map[string]string{
		"status": status,
		"time":   time.Now().String(),
	}); err != nil {
		log.Printf("Health Check Encode Error: %v", err)
	}
}

// ManualSync triggers a manual sync on all active engines
func (h *Handlers) ManualSync(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[Dashboard] Manual sync triggered for %d engines", len(h.engines))

		// Run syncs in background to avoid blocking redirect
		go func() {
			for _, engine := range h.engines {
				log.Printf("[Dashboard] Triggering manual sync for engine %s", engine.GetConfig().ID)
				if err := engine.RunSync(nil); err != nil {
					log.Printf("[Dashboard] Manual sync error for engine %s: %v", engine.GetConfig().ID, err)
				}
			}
		}()

		http.Redirect(w, r, "/", http.StatusSeeOther)
	})(w, r)
}

// GlobalPause pauses all engines
func (h *Handlers) GlobalPause(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		for _, engine := range h.engines {
			engine.Pause()
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})(w, r)
}

// GlobalResume resumes all engines
func (h *Handlers) GlobalResume(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		for _, engine := range h.engines {
			engine.Resume()
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})(w, r)
}

// WebSocket handler
func (h *Handlers) WebSocket(w http.ResponseWriter, r *http.Request) {
	wsConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}

	h.wsHub.Register(wsConn)

	// Send initial state
	if err := wsConn.WriteJSON(struct {
		Type string
		Data string
	}{Type: "init", Data: "Connected"}); err != nil {
		log.Printf("WS Init Error: %v", err)
	}

	// Keep alive / Read loop
	for {
		_, _, err := wsConn.ReadMessage()
		if err != nil {
			h.wsHub.Unregister(wsConn)
			break
		}
	}
}

// SetBandwidthLimit handler
func (h *Handlers) SetBandwidthLimit(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		limit := r.FormValue("bwlimit")
		var l int
		if _, err := fmt.Sscanf(limit, "%d", &l); err != nil {
			http.Error(w, "Invalid bandwidth limit", http.StatusBadRequest)
			return
		}

		// Update Config Normal Limit
		h.config.NormalLimit = l
		if err := h.config.Save(); err != nil {
			log.Printf("Failed to save config: %v", err)
		}

		// Reload logic handled in main.go
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})(w, r)
}

// SetNotifications handler
func (h *Handlers) SetNotifications(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.config.DiscordWebhook = r.FormValue("discord_webhook")
		h.config.TelegramToken = r.FormValue("telegram_token")
		h.config.TelegramChatID = r.FormValue("telegram_chat_id")
		if err := h.config.Save(); err != nil {
			log.Printf("Failed to save config: %v", err)
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})(w, r)
}

// SetScheduler handler
func (h *Handlers) SetScheduler(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.config.SchedulerEnabled = r.FormValue("scheduler_enabled") == "on"
		h.config.QuietStart = r.FormValue("quiet_start")
		h.config.QuietEnd = r.FormValue("quiet_end")
		if _, err := fmt.Sscanf(r.FormValue("quiet_limit"), "%d", &h.config.QuietLimit); err != nil {
			log.Printf("Failed to parse quiet_limit: %v", err)
		}
		if err := h.config.Save(); err != nil {
			log.Printf("Failed to save config: %v", err)
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})(w, r)
}

// TestNotify handler
func (h *Handlers) TestNotify(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		go h.notifier.Send("Test Notification from Dashboard", "INFO")
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})(w, r)
}

// GetProgressInfo retrieves current sync progress information from active engines
func (h *Handlers) GetProgressInfo() (progress, speed, eta string, queued int, status string) {
	status = "No sync engines active."
	allPaused := true
	if len(h.engines) > 0 {
		var sb strings.Builder
		for _, engine := range h.engines {
			sb.WriteString(engine.GetStatus() + "\n")
			if !engine.IsPaused() {
				allPaused = false
			}
		}
		status = sb.String()
	}

	progress = "System Idle"
	if allPaused && len(h.engines) > 0 {
		progress = "Sync Paused"
	}
	speed = "0 B/s"
	eta = "Done"
	queued = 0

	if len(h.engines) > 0 && !allPaused {
		for _, engine := range h.engines {
			if !engine.IsPaused() {
				// For the dashboard overview, we just report that we are monitoring
				progress = "Sync engine monitoring active..."
				break
			}
		}
	}

	return progress, speed, eta, queued, status
}

// EnginePreview returns a preview of what would be synced for a specific engine
func (h *Handlers) EnginePreview(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/api/engine/")
		id = strings.TrimSuffix(id, "/preview")

		var engine *sync.Engine
		for _, e := range h.engines {
			if e.GetConfig().ID == id {
				engine = e
				break
			}
		}

		if engine == nil {
			http.Error(w, "Engine not found", http.StatusNotFound)
			return
		}

		plan, err := engine.PreviewSync()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(plan)
	})(w, r)
}

// EngineAction handles per-engine actions like pause, resume, sync
func (h *Handlers) EngineAction(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) < 4 {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		id := parts[2]
		action := parts[3]

		var engine *sync.Engine
		for _, e := range h.engines {
			if e.GetConfig().ID == id {
				engine = e
				break
			}
		}

		if engine == nil {
			http.Error(w, "Engine not found", http.StatusNotFound)
			return
		}

		switch action {
		case "pause":
			engine.Pause()
		case "resume":
			engine.Resume()
		case "sync":
			if engine.IsBusy() {
				http.Error(w, "Sync already in progress for this engine", http.StatusConflict)
				return
			}
			log.Printf("[Dashboard] Triggering AJAX sync for engine %s", id)
			go engine.RunSync(nil)
		case "approve":
			engine.ApproveDeletions()
		default:
			http.Error(w, "Invalid action", http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Action %s initiated for engine %s", action, id)
	})(w, r)
}

// UpdateSyncMode handler
func (h *Handlers) UpdateSyncMode(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		mode := r.FormValue("mode")
		if mode != "dry" && mode != "auto" {
			http.Error(w, "Invalid mode", http.StatusBadRequest)
			return
		}

		if err := database.SaveSetting("sync_mode", mode); err != nil {
			log.Printf("Failed to save sync_mode: %v", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}

		log.Printf("[Dashboard] Sync mode updated to: %s", mode)

		// Return JSON for AJAX requests (detected by header or if it's likely an API-style call)
		if r.Header.Get("X-Requested-With") == "XMLHttpRequest" || r.Header.Get("Accept") == "application/json" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "success", "mode": mode})
			return
		}

		// Fallback for direct form submissions
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})(w, r)
}
