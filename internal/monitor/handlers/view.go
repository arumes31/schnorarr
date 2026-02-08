package handlers

import (
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"schnorarr/internal/monitor/database"
	"schnorarr/internal/ui"
)

func (h *Handlers) Index(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		healthy, lastErr := h.healthState.GetStatus()
		progress, currentSpeed, eta, queued, status := h.GetProgressInfo()
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

		type EngineView struct {
			ID, Source, Target         string
			Status, State              string
			IsPaused                   bool
			LastSync                   string
			TrafficToday, TrafficTotal string
			Rule                       string
			PendingDeletions           int
			WaitingForApproval         bool
			IsSyncing                  bool
			CurrentFile                string
			CurrentPercent             float64
			CurrentSpeed               string
			AvgSpeed                   string
			SpeedHistory               string
			Alias                      string
			HealthGrade, HealthColor   string
			IsRemoteScan               bool
		}
		var engineViews []EngineView
		for _, engine := range h.engines {
			cfg := engine.GetConfig()
			stats := database.GetEngineTrafficStats(cfg.ID)
			isSyncing := engine.IsBusy()
			file, prog, total, speed, avg, _ := engine.GetTransferStatsExtended()
			percent := 0.0
			if total > 0 {
				percent = float64(prog) / float64(total) * 100
			}
			var historyStrings []string
			for _, s := range engine.GetSpeedHistory() {
				historyStrings = append(historyStrings, strconv.FormatInt(s, 10))
			}

			grade, color := database.GetEngineHealth(cfg.ID)

			engineViews = append(engineViews, EngineView{
				ID: cfg.ID, Source: cfg.SourceDir, Target: cfg.TargetDir, Status: engine.GetStatus(), State: "ACTIVE", IsPaused: engine.IsPaused(),
				LastSync: engine.GetLastSyncTime().Format(time.RFC3339), TrafficToday: database.FormatBytes(stats.Today), TrafficTotal: database.FormatBytes(stats.Total),
				Rule: cfg.Rule, PendingDeletions: len(engine.GetPendingDeletions()), WaitingForApproval: engine.IsWaitingForApproval(), IsSyncing: isSyncing,
				CurrentFile: filepath.Base(file), CurrentPercent: percent, CurrentSpeed: database.FormatBytes(speed) + "/s", SpeedHistory: strings.Join(historyStrings, ","),
				AvgSpeed: database.FormatBytes(avg) + "/s", Alias: engine.GetAlias(),
				HealthGrade: grade, HealthColor: color, IsRemoteScan: engine.IsRemoteScan(),
			})
			if isSyncing {
				engineViews[len(engineViews)-1].State = "SYNCING"
			}
			if engine.IsPaused() {
				engineViews[len(engineViews)-1].State = "PAUSED"
			}
			if engine.IsWaitingForApproval() {
				engineViews[len(engineViews)-1].State = "WAITING_APPROVAL"
			}
		}

		traffic := database.GetTrafficStats()
		yesterday := database.GetYesterdayTraffic()
		history, _ := database.GetHistory(15, 0, "")
		deltaPct := 0
		if yesterday > 0 {
			deltaPct = int(((float64(traffic.Today) - float64(yesterday)) / float64(yesterday)) * 100)
		}

		h_rec, _, rVer, rUp := h.healthState.GetReceiverStatus()

		data := struct {
			Time, LastErrorMsg, Progress, LsyncdStatus string
			Healthy, ReceiverHealthy                   bool
			State                                      string
			Queued                                     int
			History                                    []database.HistoryItem
			TrafficToday, TrafficTotal                 string
			TrafficYesterday                           string
			TrafficDelta                               int
			TrafficDeltaPositive                       bool
			CurrentSpeed                               string
			ETA                                        string
			SyncMode                                   string
			AutoApproveDeletions                       string
			Engines                                    []EngineView
			ReceiverVersion, ReceiverUptime            string
			SenderOverride                             bool
			Timestamp                                  int64
		}{
			Time: time.Now().Format("2006-01-02 15:04:05"), Healthy: healthy, State: state, LastErrorMsg: lastErr, Progress: progress, LsyncdStatus: status, Queued: queued, History: history,
			TrafficToday: database.FormatBytes(traffic.Today), TrafficTotal: database.FormatBytes(traffic.Total), TrafficYesterday: database.FormatBytes(yesterday),
			TrafficDelta: deltaPct, TrafficDeltaPositive: deltaPct >= 0,
			CurrentSpeed: currentSpeed, ETA: eta, SyncMode: database.GetSetting("sync_mode", "dry"), AutoApproveDeletions: database.GetSetting("auto_approve", "off"),
			Engines: engineViews, ReceiverHealthy: h_rec,
			ReceiverVersion: rVer, ReceiverUptime: rUp, SenderOverride: h.healthState.IsOverrideEnabled(),
			Timestamp: time.Now().Unix(),
		}

		funcMap := template.FuncMap{"lower": strings.ToLower}
		t, err := template.New("index.html").Funcs(funcMap).ParseFS(ui.TemplateFS, "web/templates/index.html")
		if err != nil {
			http.Error(w, "Template Error: "+err.Error(), 500)
			return
		}
		if err := t.Execute(w, data); err != nil {
			log.Printf("Template Execute Error: %v", err)
		}
	})(w, r)
}

func (h *Handlers) History(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		limit := 50
		offset := (page - 1) * limit
		history, _ := database.GetHistory(limit, offset, query)
		totalCount, _ := database.GetHistoryCount(query)
		totalPages := (totalCount + limit - 1) / limit
		data := struct {
			History                                     []database.HistoryItem
			Query                                       string
			CurrentPage, TotalPages, PrevPage, NextPage int
		}{
			History: history, Query: query, CurrentPage: page, TotalPages: totalPages, PrevPage: page - 1, NextPage: page + 1,
		}
		funcMap := template.FuncMap{"lower": strings.ToLower, "add": func(a, b int) int { return a + b }, "sub": func(a, b int) int { return a - b }}
		t, err := template.New("history.html").Funcs(funcMap).ParseFS(ui.TemplateFS, "web/templates/history.html")
		if err != nil {
			http.Error(w, "Template Error: "+err.Error(), 500)
			return
		}
		if err := t.Execute(w, data); err != nil {
			log.Printf("Template Error: %v", err)
		}
	})(w, r)
}
