package app

import (
	"fmt"
	"log"
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
	go startSyncStatusBroadcaster(a.WSHub, a.SyncEngines)
	go checkReceiverHealth(a.HealthState, a.SyncEngines)
}

func startSyncEngines(wsHub *websocket.Hub, healthState *health.State, notifier *notification.Service) []*sync.Engine {
	var engines []*sync.Engine
	for i := 1; i <= 10; i++ {
		prefix := "SYNC_" + strconv.Itoa(i)
		src := os.Getenv(prefix + "_SOURCE")
		tgt := os.Getenv(prefix + "_TARGET")
		rule := os.Getenv(prefix + "_RULE")
		if src == "" || tgt == "" { continue }

		resolvedTgt := sync.ResolveTargetPath(tgt, os.Getenv("DEST_HOST"), os.Getenv("DEST_MODULE"))
		
		bwlimitBytes := int64(0)
		if bwStr := os.Getenv("BWLIMIT_MBPS"); bwStr != "" {
			if bwlimitMBps, err := strconv.ParseInt(bwStr, 10, 64); err == nil && bwlimitMBps > 0 {
				bwlimitBytes = bwlimitMBps * 125000
			}
		}

		pollInterval := 10 * time.Second
		if pollStr := os.Getenv("POLL_INTERVAL"); pollStr != "" {
			if pi, err := strconv.Atoi(pollStr); err == nil && pi > 0 {
				pollInterval = time.Duration(pi) * time.Second
			}
		}

		autoApprove := database.GetSetting("auto_approve", "off") == "on"
		if envApprove := os.Getenv(prefix + "_AUTO_APPROVE"); envApprove != "" {
			autoApprove = envApprove == "true" || envApprove == "1" || envApprove == "on"
		}

		engine := sync.NewEngine(sync.SyncConfig{
			ID:                   strconv.Itoa(i),
			SourceDir:            src,
			TargetDir:            resolvedTgt,
			Rule:                 rule,
			ExcludePatterns:      []string{".git", ".DS_Store", "Thumbs.db"},
			BandwidthLimit:       bwlimitBytes,
			WatchInterval:        3 * time.Hour,
			PollInterval:         pollInterval,
			AutoApproveDeletions: autoApprove,
			DryRunFunc: func() bool {
				return database.GetSetting("sync_mode", "dry") == "dry"
			},
			OnSyncEvent: func(timestamp, action, path string, size int64) {
				_ = database.LogEvent(timestamp, action, path, size, strconv.Itoa(i))
				item := database.HistoryItem{Time: timestamp, Action: action, Path: path, Size: database.FormatBytes(size)}
				wsHub.Broadcast("history", item)
				wsHub.Broadcast("stats", database.GetTrafficStats())
				wsHub.Broadcast("daily", database.GetDailyTraffic(7))
				healthState.ReportSuccess(notifier.Send)
			},
			OnError: func(msg string) { healthState.ReportError(msg, notifier.Send) },
		})

		if err := engine.Start(); err != nil {
			log.Printf("Failed to start engine %d: %v", i, err)
			continue
		}
		engines = append(engines, engine)
		if database.GetSetting("engine_paused_"+strconv.Itoa(i), "false") == "true" {
			engine.Pause()
		}
	}
	return engines
}

func startSyncStatusBroadcaster(wsHub *websocket.Hub, syncEngines []*sync.Engine) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		var totalSpeed int64
		var totalRemaining int64
		allPaused := true

		type EngineProgress struct {
			ID         string  `json:"id"`
			File       string  `json:"file"`
			Percent    float64 `json:"percent"`
			Speed      string  `json:"speed"`
			Today      string  `json:"today"`
			Total      string  `json:"total"`
			IsActive   bool    `json:"is_active"`
			ETA        string  `json:"eta"`
			QueueCount int     `json:"queue_count"`
			IsScanning bool    `json:"is_scanning"`
		}
		engineStats := make([]EngineProgress, 0)

		for _, engine := range syncEngines {
			if !engine.IsPaused() { allPaused = false }

			file, prog, total, speed := engine.GetTransferStats()
			totalSpeed += speed
			
			planRemaining := engine.GetPlanRemainingBytes()
			totalRemaining += (planRemaining - prog)
			
			qCount, queuedSize := engine.GetQueuedStats()
			totalRemaining += queuedSize

			percent := 0.0
			if total > 0 { percent = float64(prog) / float64(total) * 100 }

			stats := database.GetEngineTrafficStats(engine.GetConfig().ID)
			etaStr := "Done"
			if speed > 0 && total > prog {
				rem := total - prog; sec := rem / speed
				if sec > 3600 { etaStr = fmt.Sprintf("%dh %dm", sec/3600, (sec%3600)/60) } else if sec > 60 { etaStr = fmt.Sprintf("%dm %ds", sec/60, sec%60) } else { etaStr = fmt.Sprintf("%ds", sec) }
			}

			engineStats = append(engineStats, EngineProgress{
				ID:         engine.GetConfig().ID,
				File:       filepath.Base(file),
				Percent:    percent,
				Speed:      database.FormatBytes(speed) + "/s",
				Today:      database.FormatBytes(stats.Today),
				Total:      database.FormatBytes(stats.Total),
				IsActive:   speed > 0,
				ETA:        etaStr,
				QueueCount: qCount,
				IsScanning: engine.IsScanning(),
			})
		}

		state := "ACTIVE"
		progress := "Monitoring..."
		if len(syncEngines) > 0 {
			if allPaused { state = "PAUSED"; progress = "Sync Paused" } else if totalSpeed > 0 { state = "SYNCING"; progress = "Transferring data..." }
		}

		globalEta := "Done"
		if totalSpeed > 0 && totalRemaining > 0 {
			if totalRemaining < 0 { totalRemaining = 0 }
			sec := totalRemaining / totalSpeed
			if sec > 3600 { globalEta = fmt.Sprintf("%dh %dm", sec/3600, (sec%3600)/60) } else if sec > 60 { globalEta = fmt.Sprintf("%dm %ds", sec/60, sec%60) } else { globalEta = fmt.Sprintf("%ds", sec) }
		}

		wsHub.Broadcast("progress", map[string]interface{}{
			"speed": database.FormatBytes(totalSpeed) + "/s", "state": state, "engines": engineStats, "eta": globalEta,
		})
		wsHub.Broadcast("sync_status", map[string]interface{}{ "status": progress, "engines": len(syncEngines) })
	}
}

func checkReceiverHealth(healthState *health.State, engines []*sync.Engine) {
	destHost := os.Getenv("DEST_HOST")
	if destHost == "" { return }
	targetURL := fmt.Sprintf("http://%s:8080/health", destHost)
	client := http.Client{Timeout: 5 * time.Second}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		resp, err := client.Get(targetURL)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK { healthState.ReportReceiverSuccess(); continue }
		}
		if len(engines) > 0 {
			if _, err := os.Stat(engines[0].GetConfig().TargetDir); err == nil { healthState.ReportReceiverSuccess(); continue }
		}
		healthState.ReportReceiverError(fmt.Sprintf("Agent Unreachable (%s)", destHost))
	}
}