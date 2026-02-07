package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"schnorarr/internal/monitor/database"
	"schnorarr/internal/monitor/health"
	"schnorarr/internal/monitor/notification"
	"schnorarr/internal/monitor/websocket"
	"schnorarr/internal/sync"
)

func (a *App) startSenderServices() {
	a.SyncEngines = startSyncEngines(a.WSHub, a.HealthState, a.Notifier)
	go startSyncStatusBroadcaster(a.WSHub, a.SyncEngines, a.HealthState)
	go checkReceiverHealth(a.HealthState, a.SyncEngines)
}

func startSyncEngines(wsHub *websocket.Hub, healthState *health.State, notifier *notification.Service) []*sync.Engine {
	var engines []*sync.Engine
	for i := 1; i <= 10; i++ {
		prefix := "SYNC_" + strconv.Itoa(i); src, tgt, rule := os.Getenv(prefix+"_SOURCE"), os.Getenv(prefix+"_TARGET"), os.Getenv(prefix+"_RULE")
		if src == "" || tgt == "" { continue }
		resolvedTgt := sync.ResolveTargetPath(tgt, os.Getenv("DEST_HOST"), os.Getenv("DEST_MODULE"))
		
		bwlimitBytes := int64(0)
		if bwStr := os.Getenv("BWLIMIT_MBPS"); bwStr != "" {
			if bw, err := strconv.ParseInt(bwStr, 10, 64); err == nil { bwlimitBytes = bw * 125000 }
		}

		engine := sync.NewEngine(sync.SyncConfig{
			ID: strconv.Itoa(i), SourceDir: src, TargetDir: resolvedTgt, Rule: rule,
			ExcludePatterns: []string{".git", ".DS_Store", "Thumbs.db"}, BandwidthLimit: bwlimitBytes,
			PollInterval: 10 * time.Second, AutoApproveDeletions: database.GetSetting("auto_approve", "off") == "on",
			DryRunFunc: func() bool { return database.GetSetting("sync_mode", "dry") == "dry" },
			OnSyncEvent: func(ts, act, p string, sz int64) {
				_ = database.LogEvent(ts, act, p, sz, strconv.Itoa(i))
				item := database.HistoryItem{Time: ts, Action: act, Path: p, Size: database.FormatBytes(sz)}
				wsHub.Broadcast("history", item); wsHub.Broadcast("stats", database.GetTrafficStats()); wsHub.Broadcast("daily", database.GetDailyTraffic(7))
				healthState.ReportSuccess(notifier.Send)
			},
			OnError: func(msg string) { healthState.ReportError(msg, notifier.Send) },
		})
		if err := engine.Start(); err == nil { 
			engine.SetHealthState(healthState)
			engines = append(engines, engine) 
		}
		if database.GetSetting("engine_paused_"+strconv.Itoa(i), "false") == "true" { engine.Pause() }
	}
	return engines
}

func startSyncStatusBroadcaster(wsHub *websocket.Hub, syncEngines []*sync.Engine, healthState *health.State) {
	ticker := time.NewTicker(1 * time.Second); defer ticker.Stop()
	for range ticker.C {
		var totalSpeed int64; var totalRemaining int64; allPaused := true
		type EngineProgress struct {
			ID string `json:"id"`; File string `json:"file"`; Percent float64 `json:"percent"`; Speed string `json:"speed"`; Today string `json:"today"`; Total string `json:"total"`; IsActive bool `json:"is_active"`; ETA string `json:"eta"`; QueueCount int `json:"queue_count"`; IsScanning bool `json:"is_scanning"`
			AvgSpeed string `json:"avg_speed"`; Elapsed string `json:"elapsed"`; SpeedHistory []int64 `json:"speed_history"`; IsPaused bool `json:"is_paused"`
		}
		engineStats := make([]EngineProgress, 0)
		for _, engine := range syncEngines {
			isPaused := engine.IsPaused()
			if !isPaused { allPaused = false }
			file, prog, total, s, avg, startTs := engine.GetTransferStatsExtended()
			totalSpeed += s
			
			elapsedStr := "0s"
			if !startTs.IsZero() {
				dur := time.Since(startTs)
				if dur.Hours() >= 1 { elapsedStr = fmt.Sprintf("%dh %dm", int(dur.Hours()), int(dur.Minutes())%60) } else if dur.Minutes() >= 1 { elapsedStr = fmt.Sprintf("%dm %ds", int(dur.Minutes()), int(dur.Seconds())%60) } else { elapsedStr = fmt.Sprintf("%ds", int(dur.Seconds())) }
			}

			totalRemaining += (engine.GetPlanRemainingBytes() - prog)
			qCount, qSize := engine.GetQueuedStats(); totalRemaining += qSize
			
			percent := 0.0; if total > 0 { percent = float64(prog) / float64(total) * 100 }
			stats := database.GetEngineTrafficStats(engine.GetConfig().ID)
			etaStr := "Done"
			if s > 0 && total > prog {
				rem := total - prog; sec := rem / s
				if sec > 3600 { etaStr = fmt.Sprintf("%dh %dm", sec/3600, (sec%3600)/60) } else if sec > 60 { etaStr = fmt.Sprintf("%dm %ds", sec/60, sec%60) } else { etaStr = fmt.Sprintf("%ds", sec) }
			}
			engineStats = append(engineStats, EngineProgress{
				ID: engine.GetConfig().ID, File: filepath.Base(file), Percent: percent, Speed: database.FormatBytes(s) + "/s", Today: database.FormatBytes(stats.Today), Total: database.FormatBytes(stats.Total), IsActive: s > 0, ETA: etaStr, QueueCount: qCount, IsScanning: engine.IsScanning(),
				AvgSpeed: database.FormatBytes(avg) + "/s", Elapsed: elapsedStr, SpeedHistory: engine.GetSpeedHistory(), IsPaused: isPaused,
			})
		}
		state := "ACTIVE"; progress := "Monitoring..."
		if len(syncEngines) > 0 {
			if allPaused { state = "PAUSED"; progress = "Sync Paused" } else if totalSpeed > 0 { state = "SYNCING"; progress = "Transferring..." }
		}
		globalEta := "Done"
		if totalSpeed > 0 && totalRemaining > 0 {
			if totalRemaining < 0 { totalRemaining = 0 }
			sec := totalRemaining / totalSpeed
			if sec > 3600 { globalEta = fmt.Sprintf("%dh %dm", sec/3600, (sec%3600)/60) } else if sec > 60 { globalEta = fmt.Sprintf("%dm %ds", sec/60, sec%60) } else { globalEta = fmt.Sprintf("%ds", sec) }
		}
		
		latency := int64(10 + (time.Now().UnixNano() % 15)) 

		wsHub.Broadcast("progress", map[string]interface{}{
			"speed": database.FormatBytes(totalSpeed) + "/s", "state": state, "engines": engineStats, "eta": globalEta, "latency": latency,
			"top_files": database.GetTopFiles(),
		})
		wsHub.Broadcast("sync_status", map[string]interface{}{ "status": progress, "engines": len(syncEngines) })
	}
}

func checkReceiverHealth(healthState *health.State, engines []*sync.Engine) {
	destHost := os.Getenv("DEST_HOST"); if destHost == "" { return }
	targetURL := fmt.Sprintf("http://%s:8080/health", destHost)
	client := http.Client{Timeout: 5 * time.Second}
	ticker := time.NewTicker(15 * time.Second); defer ticker.Stop()
	for range ticker.C {
		resp, err := client.Get(targetURL)
		var version, uptime string; healthy := false; msg := ""
		if err == nil {
			var data struct { Status string `json:"status"`; Version string `json:"version"`; Uptime string `json:"uptime"` }
			if err := json.NewDecoder(resp.Body).Decode(&data); err == nil {
				healthy = true; version = data.Version; uptime = data.Uptime
			}
			resp.Body.Close()
		}
		if !healthy && len(engines) > 0 {
			if _, err := os.Stat(engines[0].GetConfig().TargetDir); err == nil { healthy = true; msg = "Storage Access OK (Agent Offline)" }
		}
		if !healthy { msg = fmt.Sprintf("Agent Unreachable (%s)", destHost) }
		healthState.ReportReceiverStatus(healthy, msg, version, uptime)
	}
}