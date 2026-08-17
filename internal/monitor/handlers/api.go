package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"schnorarr/internal/monitor/config"
	"schnorarr/internal/monitor/database"
	"schnorarr/internal/sync"
)

func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	status := "healthy"
	_ = json.NewEncoder(w).Encode(map[string]string{"status": status, "time": time.Now().String()})
}

func (h *Handlers) GetProgressInfo() (progress, speed, eta string, queued int, status string) {
	var totalSpeed int64
	var totalRemaining int64
	allPaused := true
	var sb strings.Builder
	for _, engine := range h.engineProvider() {
		sb.WriteString(engine.GetStatus() + "\n")
		if !engine.IsPaused() {
			allPaused = false
		}
		_, transferredBytes, _, currentSpeed := engine.GetTransferStats()
		totalSpeed += currentSpeed
		totalRemaining += (engine.GetPlanRemainingBytes() - transferredBytes)
		queuedCount, queuedSize := engine.GetQueuedStats()
		totalRemaining += queuedSize
		queued += queuedCount
	}
	status = sb.String()
	speed = database.FormatBytes(totalSpeed) + "/s"
	if totalSpeed > 0 && totalRemaining > 0 {
		if totalRemaining < 0 {
			totalRemaining = 0
		}
		sec := totalRemaining / totalSpeed
		if sec > 3600 {
			eta = fmt.Sprintf("%dh %dm", sec/3600, (sec%3600)/60)
		} else if sec > 60 {
			eta = fmt.Sprintf("%dm %ds", sec/60, sec%60)
		} else {
			eta = fmt.Sprintf("%ds", sec)
		}
	} else {
		eta = "Done"
	}
	progress = "Monitoring..."
	if allPaused && len(h.engineProvider()) > 0 {
		progress = "Sync Paused"
	} else if totalSpeed > 0 {
		progress = "Transferring..."
	}
	return progress, speed, eta, queued, status
}

func (h *Handlers) ManualSync(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		for _, e := range h.engineProvider() {
			e.Resume()
			_ = database.SaveSetting("engine_paused_"+e.GetConfig().ID, "false")
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})(w, r)
}

func (h *Handlers) GlobalPause(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		for _, e := range h.engineProvider() {
			e.Pause()
			_ = database.SaveSetting("engine_paused_"+e.GetConfig().ID, "true")
		}
		_ = database.LogSystemEvent(h.GetUser(r), "Paused All", "User paused all engines")
		if r.Header.Get("Accept") == "application/json" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})(w, r)
}

func (h *Handlers) GlobalResume(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		for _, e := range h.engineProvider() {
			e.Resume()
			_ = database.SaveSetting("engine_paused_"+e.GetConfig().ID, "false")
		}
		_ = database.LogSystemEvent(h.GetUser(r), "Resumed All", "User resumed all engines")
		if r.Header.Get("Accept") == "application/json" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})(w, r)
}

func (h *Handlers) BulkAction(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			IDs    []string `json:"ids"`
			Action string   `json:"action"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid body", 400)
			return
		}
		for _, id := range req.IDs {
			var engine *sync.Engine
			for _, e := range h.engineProvider() {
				if e.GetConfig().ID == id {
					engine = e
					break
				}
			}
			if engine == nil {
				continue
			}
			switch req.Action {
			case "sync":
				if !engine.IsBusy() {
					engine.Resume()
					_ = database.SaveSetting("engine_paused_"+id, "false")
				}
			case "pause":
				engine.Pause()
				_ = database.SaveSetting("engine_paused_"+id, "true")
			case "resume":
				engine.Resume()
				_ = database.SaveSetting("engine_paused_"+id, "false")
			}
		}
		_ = database.LogSystemEvent(h.GetUser(r), "Bulk "+req.Action, fmt.Sprintf("Action on %d engines", len(req.IDs)))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
	})(w, r)
}

func (h *Handlers) EnginePreview(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/engine/"), "/preview")
		var engine *sync.Engine
		for _, e := range h.engineProvider() {
			if e.GetConfig().ID == id {
				engine = e
				break
			}
		}
		if engine == nil {
			http.Error(w, "Not found", 404)
			return
		}
		plan, err := engine.PreviewSync()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(plan)
	})(w, r)
}

func (h *Handlers) EngineAction(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) < 4 {
			http.Error(w, "Invalid", 400)
			return
		}
		id, action := parts[2], parts[3]
		var engine *sync.Engine
		for _, e := range h.engineProvider() {
			if e.GetConfig().ID == id {
				engine = e
				break
			}
		}
		if engine == nil {
			http.Error(w, "Not found", 404)
			return
		}
		switch action {
		case "pause":
			engine.Pause()
			_ = database.SaveSetting("engine_paused_"+id, "true")
		case "resume":
			engine.Resume()
			_ = database.SaveSetting("engine_paused_"+id, "false")
		case "sync":
			if !engine.IsBusy() {
				engine.Resume()
				_ = database.SaveSetting("engine_paused_"+id, "false")
			}
		case "approve":
			engine.ApproveDeletions()
		case "approve-list":
			var req struct {
				Files []string `json:"files"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
				engine.ApproveSpecificChanges(req.Files)
			}
		}
		_ = database.LogSystemEvent(h.GetUser(r), "Engine "+action, "Engine "+id)
		w.WriteHeader(200)
	})(w, r)
}

func (h *Handlers) EngineAlias(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/engine/"), "/alias")
		alias := r.FormValue("alias")
		if alias == "" {
			http.Error(w, "Alias required", 400)
			return
		}
		var engine *sync.Engine
		for _, e := range h.engineProvider() {
			if e.GetConfig().ID == id {
				engine = e
				break
			}
		}
		if engine == nil {
			http.Error(w, "Not found", 404)
			return
		}
		engine.SetAlias(alias)
		_ = database.SaveSetting("alias_"+id, alias)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
	})(w, r)
}

func (h *Handlers) UpdateSyncMode(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		mode := r.FormValue("mode")
		if mode != "dry" && mode != "manual" && mode != "auto" {
			http.Error(w, "Invalid", 400)
			return
		}
		_ = database.SaveSetting("sync_mode", mode)
		_ = database.LogSystemEvent(h.GetUser(r), "Update Sync Mode", "Mode set to "+mode)
		if r.Header.Get("Accept") == "application/json" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})(w, r)
}

func (h *Handlers) UpdateAutoApprove(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		val := r.FormValue("auto_approve")
		_ = database.SaveSetting("auto_approve", val)
		for _, e := range h.engineProvider() {
			e.SetAutoApproveDeletions(val == "on")
		}
		_ = database.LogSystemEvent(h.GetUser(r), "Update Auto Approve", "Set to "+val)
		if r.Header.Get("Accept") == "application/json" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})(w, r)
}

func (h *Handlers) UpdateSenderOverride(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		val := r.FormValue("enabled") == "true"
		h.healthState.SetSenderOverride(val)

		// Persist the setting
		valStr := "false"
		if val {
			valStr = "true"
		}
		_ = database.SaveSetting("sender_override", valStr)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
	})(w, r)
}

func (h *Handlers) TestNotify(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		go h.notifier.Send("Test from Dashboard", "INFO")
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})(w, r)
}

func (h *Handlers) UpdateBwlimit(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		mbps, err := strconv.Atoi(strings.TrimSpace(r.FormValue("mbps")))
		if err != nil {
			http.Error(w, "Invalid Mbps value: must be an integer >= 0", http.StatusBadRequest)
			return
		}
		bps, err := config.MbpsToBps(mbps)
		if err != nil {
			http.Error(w, "Invalid Mbps value: "+err.Error(), http.StatusBadRequest)
			return
		}
		oldMbps := h.config.BwlimitMbps
		h.config.BwlimitMbps = &mbps
		if err := h.config.Save(); err != nil {
			h.config.BwlimitMbps = oldMbps
			http.Error(w, "Failed to save configuration", http.StatusInternalServerError)
			return
		}
		if h.bwManager != nil {
			h.bwManager.SetGlobalLimit(bps, sync.LimitSourceManual)
		}
		_ = database.LogSystemEvent(h.GetUser(r), "Update Bandwidth Limit", fmt.Sprintf("Global limit set to %d Mbps", mbps))
		if r.Header.Get("Accept") == "application/json" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})(w, r)
}

var hhmmPattern = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

func (h *Handlers) SetScheduler(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		quietStart := r.FormValue("quiet_start")
		quietEnd := r.FormValue("quiet_end")
		if !hhmmPattern.MatchString(quietStart) || !hhmmPattern.MatchString(quietEnd) {
			http.Error(w, "Invalid time format: quiet_start and quiet_end must be HH:MM", http.StatusBadRequest)
			return
		}
		quietLimit, err := strconv.Atoi(strings.TrimSpace(r.FormValue("quiet_limit")))
		if err != nil {
			http.Error(w, "Invalid quiet_limit: must be an integer >= 0", http.StatusBadRequest)
			return
		}
		if _, err := config.MbpsToBps(quietLimit); err != nil {
			http.Error(w, "Invalid quiet_limit: "+err.Error(), http.StatusBadRequest)
			return
		}
		normalLimit, err := strconv.Atoi(strings.TrimSpace(r.FormValue("normal_limit")))
		if err != nil {
			http.Error(w, "Invalid normal_limit: must be an integer >= 0", http.StatusBadRequest)
			return
		}
		if _, err := config.MbpsToBps(normalLimit); err != nil {
			http.Error(w, "Invalid normal_limit: "+err.Error(), http.StatusBadRequest)
			return
		}
		enabledRaw := r.FormValue("scheduler_enabled")
		oldCfg := *h.config
		h.config.SchedulerEnabled = enabledRaw == "on" || enabledRaw == "true"
		h.config.QuietStart = quietStart
		h.config.QuietEnd = quietEnd
		h.config.QuietLimit = quietLimit
		h.config.NormalLimit = normalLimit
		if err := h.config.Save(); err != nil {
			*h.config = oldCfg
			http.Error(w, "Failed to save configuration", http.StatusInternalServerError)
			return
		}
		_ = database.LogSystemEvent(h.GetUser(r), "Update Scheduler", fmt.Sprintf("Enabled: %v, Quiet: %s-%s @ %d Mbps, Normal: %d Mbps",
			h.config.SchedulerEnabled, quietStart, quietEnd, quietLimit, normalLimit))
		if r.Header.Get("Accept") == "application/json" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})(w, r)
}

func (h *Handlers) SetNotifications(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		h.config.DiscordWebhook = r.FormValue("webhook_url")
		_ = h.config.Save()
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})(w, r)
}

func (h *Handlers) ExportHistory(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		history, _ := database.GetHistory(0, 0, "")
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", "attachment;filename=schnorarr-history.csv")
		if _, err := fmt.Fprintln(w, "Timestamp,Action,Path,Size"); err != nil {
			return
		}
		for _, item := range history {
			if _, err := fmt.Fprintf(w, "%s,%s,\"%s\",%s\n", item.Time, item.Action, item.Path, item.Size); err != nil {
				return
			}
		}
	})(w, r)
}
