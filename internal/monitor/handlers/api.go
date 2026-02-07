package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

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
	for _, engine := range h.engines {
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
	if allPaused && len(h.engines) > 0 {
		progress = "Sync Paused"
	} else if totalSpeed > 0 {
		progress = "Transferring..."
	}
	return progress, speed, eta, queued, status
}

func (h *Handlers) ManualSync(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		for _, e := range h.engines {
			go func(eng *sync.Engine) { _ = eng.RunSync(nil) }(e)
		}
		http.Redirect(w, r, "/", 303)
	})(w, r)
}

func (h *Handlers) GlobalPause(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		for _, e := range h.engines {
			e.Pause()
			_ = database.SaveSetting("engine_paused_"+e.GetConfig().ID, "true")
		}
		_ = database.LogSystemEvent(h.GetUser(r), "Paused All", "User paused all engines")
		http.Redirect(w, r, "/", 303)
	})(w, r)
}

func (h *Handlers) GlobalResume(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		for _, e := range h.engines {
			e.Resume()
			_ = database.SaveSetting("engine_paused_"+e.GetConfig().ID, "false")
		}
		_ = database.LogSystemEvent(h.GetUser(r), "Resumed All", "User resumed all engines")
		http.Redirect(w, r, "/", 303)
	})(w, r)
}

func (h *Handlers) BulkAction(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", 405)
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
			for _, e := range h.engines {
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
					go func(e *sync.Engine) { _ = e.RunSync(nil) }(engine)
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
		for _, e := range h.engines {
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
		if engine.IsWaitingForApproval() {
			plan.FilesToDelete = engine.GetPendingDeletions()
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
		for _, e := range h.engines {
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
				go func() { _ = engine.RunSync(nil) }()
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
			http.Error(w, "Method not allowed", 405)
			return
		}
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/engine/"), "/alias")
		alias := r.FormValue("alias")
		if alias == "" {
			http.Error(w, "Alias required", 400)
			return
		}
		var engine *sync.Engine
		for _, e := range h.engines {
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
		http.Redirect(w, r, "/", 303)
	})(w, r)
}

func (h *Handlers) UpdateAutoApprove(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		val := r.FormValue("auto_approve")
		_ = database.SaveSetting("auto_approve", val)
		for _, e := range h.engines {
			e.SetAutoApproveDeletions(val == "on")
		}
		_ = database.LogSystemEvent(h.GetUser(r), "Update Auto Approve", "Set to "+val)
		if r.Header.Get("Accept") == "application/json" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
			return
		}
		http.Redirect(w, r, "/", 303)
	})(w, r)
}

func (h *Handlers) UpdateSenderOverride(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", 405)
			return
		}
		val := r.FormValue("enabled") == "true"
		h.healthState.SetSenderOverride(val)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
	})(w, r)
}

func (h *Handlers) TestNotify(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		go h.notifier.Send("Test from Dashboard", "INFO")
		http.Redirect(w, r, "/", 303)
	})(w, r)
}

func (h *Handlers) SetScheduler(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		h.config.QuietStart = r.FormValue("quiet_hours")
		_ = h.config.Save()
		http.Redirect(w, r, "/", 303)
	})(w, r)
}

func (h *Handlers) SetNotifications(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		h.config.DiscordWebhook = r.FormValue("webhook_url")
		_ = h.config.Save()
		http.Redirect(w, r, "/", 303)
	})(w, r)
}

func (h *Handlers) ExportHistory(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		history, _ := database.GetHistory(0, 0, "")
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", "attachment;filename=schnorarr-history.csv")
		fmt.Fprintln(w, "Timestamp,Action,Path,Size")
		for _, item := range history {
			fmt.Fprintf(w, "%s,%s,\"%s\",%s\n", item.Time, item.Action, item.Path, item.Size)
		}
	})(w, r)
}
