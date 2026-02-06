package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"schnorarr/internal/monitor/config"
	"schnorarr/internal/monitor/database"
	"schnorarr/internal/monitor/health"
	"schnorarr/internal/monitor/notification"
	ws "schnorarr/internal/monitor/websocket"
	"schnorarr/internal/ui"
)

const (
	StatusFile   = "/tmp/lsyncd.status"
	ProgressFile = "/tmp/current_sync.tmp"
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
}

// New creates a new handlers instance
func New(cfg *config.Config, healthState *health.State, wsHub *ws.Hub, db *sql.DB, notifier *notification.Service) *Handlers {
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

		progress := ""
		if b, err := os.ReadFile(ProgressFile); err == nil {
			progress = strings.TrimSpace(string(b))
		}

		status := "No status available yet."
		if b, err := os.ReadFile(StatusFile); err == nil {
			status = string(b)
		}

		queued := 0
		queued = len(regexp.MustCompile(`(?:Wait list|DelayedSyncs).*?\n\n`).FindAllString(status, -1))

		// Extract Speed from Progress
		currentSpeed := "0 B/s"
		reSpeed := regexp.MustCompile(`(\d+(?:\.\d+)?[kKMGT]?B/s)`)
		matches := reSpeed.FindStringSubmatch(progress)
		if len(matches) > 1 {
			currentSpeed = matches[1]
		}

		traffic := database.GetTrafficStats()
		history, _ := database.GetHistory(15, "")

		data := struct {
			Time, LastErrorMsg, Progress, LsyncdStatus string
			Healthy                                    bool
			Queued                                     int
			History                                    []database.HistoryItem
			TrafficToday, TrafficTotal                 string
			CurrentSpeed                               string
		}{
			Time:         time.Now().Format("2006-01-02 15:04:05"),
			Healthy:      healthy,
			LastErrorMsg: lastErr,
			Progress:     progress,
			LsyncdStatus: status,
			Queued:       queued,
			History:      history,
			TrafficToday: database.FormatBytes(traffic.Today),
			TrafficTotal: database.FormatBytes(traffic.Total),
			CurrentSpeed: currentSpeed,
		}

		t, err := template.ParseFS(ui.TemplateFS, "web/templates/index.html")
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

		t, err := template.ParseFS(ui.TemplateFS, "web/templates/history.html")
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
	if _, err := os.Stat(StatusFile); os.IsNotExist(err) {
		status = "initializing"
	}
	if err := json.NewEncoder(w).Encode(map[string]string{
		"status": status,
		"time":   time.Now().String(),
	}); err != nil {
		log.Printf("Health Check Encode Error: %v", err)
	}
}

// ManualSync triggers a manual sync
func (h *Handlers) ManualSync(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		go func() {
			if err := filepath.Walk("/data", func(path string, info os.FileInfo, err error) error {
				if err == nil && info.IsDir() {
					if err := os.Chtimes(path, time.Now(), time.Now()); err != nil {
						log.Printf("Failed to touch %s: %v", path, err)
					}
				}
				return nil
			}); err != nil {
				log.Printf("Manual Sync Walk Error: %v", err)
			}
		}()
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})(w, r)
}

// WebSocket handler
func (h *Handlers) WebSocket(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}

	h.wsHub.Register(ws)

	// Send initial state
	if err := ws.WriteJSON(struct {
		Type string
		Data string
	}{Type: "init", Data: "Connected"}); err != nil {
		log.Printf("WS Init Error: %v", err)
	}

	// Keep alive / Read loop
	for {
		_, _, err := ws.ReadMessage()
		if err != nil {
			h.wsHub.Unregister(ws)
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

		err := os.WriteFile("/config/bwlimit", []byte(limit), 0644)
		if err != nil {
			http.Error(w, "Failed to write config: "+err.Error(), http.StatusInternalServerError)
			return
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

// GetProgressInfo retrieves current sync progress information
func GetProgressInfo() (progress, speed, eta string, queued int, status string) {
	progress = ""
	if b, err := os.ReadFile(ProgressFile); err == nil {
		progress = strings.TrimSpace(string(b))
	}

	status = "No status available yet."
	if b, err := os.ReadFile(StatusFile); err == nil {
		status = string(b)
	}

	// Speed
	speed = "0 B/s"
	reSpeed := regexp.MustCompile(`(\d+(?:\.\d+)?[kKMGT]?B/s)`)
	matches := reSpeed.FindStringSubmatch(progress)
	if len(matches) > 1 {
		speed = matches[1]
	}

	// ETA Calculation
	eta = "Calculating..."
	var queuedBytes int64 = 0

	// Parse Queued Files from Status
	statusParts := strings.Split(status, "Wait list")
	if len(statusParts) > 1 {
		waitList := statusParts[1]
		rePath := regexp.MustCompile(`/\S+`)
		paths := rePath.FindAllString(waitList, -1)
		for _, p := range paths {
			fullP := filepath.Join("/data", strings.Trim(p, `"'`))
			if info, err := os.Stat(fullP); err == nil {
				queuedBytes += info.Size()
			}
		}
	}

	speedBytes := ParseSpeed(speed)
	if speedBytes > 0 && queuedBytes > 0 {
		seconds := float64(queuedBytes) / float64(speedBytes)
		duration := time.Duration(seconds) * time.Second
		eta = duration.Round(time.Second).String()
	} else if queuedBytes == 0 {
		eta = "Done"
	}

	queued = len(regexp.MustCompile(`(?:Wait list|DelayedSyncs).*?\n\n`).FindAllString(status, -1))

	return progress, speed, eta, queued, status
}

// ParseSpeed converts speed string to bytes/sec
func ParseSpeed(s string) int64 {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "/s")
	if len(s) == 0 {
		return 0
	}

	unit := s[len(s)-2:]
	valStr := s[:len(s)-2]

	// Handle single char unit like "B"
	if strings.ToUpper(s[len(s)-1:]) == "B" && !strings.Contains("KMGTP", strings.ToUpper(s[len(s)-2:len(s)-1])) {
		unit = "B"
		valStr = s[:len(s)-1]
	}

	var val float64
	if _, err := fmt.Sscanf(valStr, "%f", &val); err != nil {
		return 0
	}

	switch strings.ToUpper(unit) {
	case "KB", "K":
		return int64(val * 1024)
	case "MB", "M":
		return int64(val * 1024 * 1024)
	case "GB", "G":
		return int64(val * 1024 * 1024 * 1024)
	case "B":
		return int64(val)
	}
	return int64(val)
}
