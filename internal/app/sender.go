package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"schnorarr/internal/monitor/database"
	"schnorarr/internal/monitor/health"
	"schnorarr/internal/monitor/notification"
	"schnorarr/internal/monitor/websocket"
	"schnorarr/internal/sync"
)

func (a *App) startSenderServices() {
	// Shared latency variable
	var latency int64
	engines := startSyncEngines(a.WSHub, a.HealthState, a.Notifier)

	a.engineMu.Lock()
	a.SyncEngines = engines
	a.engineMu.Unlock()

	go startSyncStatusBroadcaster(a.WSHub, engines, a.HealthState, &latency)
	go checkReceiverHealth(a.HealthState, engines, &latency)
}

func startSyncEngines(wsHub *websocket.Hub, healthState *health.State, notifier *notification.Service) []*sync.Engine {
	var engines []*sync.Engine
	for i := 1; i <= 10; i++ {
		id := strconv.Itoa(i) // Capture loop variable
		prefix := "SYNC_" + id
		src, tgt, rule := os.Getenv(prefix+"_SOURCE"), os.Getenv(prefix+"_TARGET"), os.Getenv(prefix+"_RULE")
		if src == "" || tgt == "" {
			continue
		}
		var resolvedTgt string
		destHost := os.Getenv("DEST_HOST")
		destModule := os.Getenv("DEST_MODULE")

		if destHost != "" {
			// Check if target is already a full rsync URI
			if strings.Contains(tgt, "::") || strings.HasPrefix(tgt, "rsync://") {
				resolvedTgt = sync.UpdateTargetHost(tgt, destHost)
			} else if destModule != "" {
				// Construct Rsync URI: user@host::module/path
				// e.g. syncuser@192.168.1.50::video-sync/movies
				rsyncUser := os.Getenv("RSYNC_USER")
				if rsyncUser == "" {
					rsyncUser = "syncuser" // Default
				}
				// Using rsync:// syntax is sometimes safer for parsing, but :: is standard for daemon
				resolvedTgt = fmt.Sprintf("%s@%s::%s/%s", rsyncUser, destHost, destModule, tgt)
			}
		} else {
			// Local fallback (for testing or local-only mode)
			resolvedTgt = sync.ResolveTargetPath(tgt, "", "")
		}

		bwlimitBytes := int64(0)
		if bwStr := os.Getenv("BWLIMIT_MBPS"); bwStr != "" {
			if bw, err := strconv.ParseInt(bwStr, 10, 64); err == nil {
				bwlimitBytes = bw * 125000
			}
		}

		// Determine include patterns
		// 1. Default
		includePatterns := []string{"*.mkv", "*.mp4", "*.avi"}
		// 2. Global Override
		if env := os.Getenv("SYNC_INCLUDE"); env != "" {
			includePatterns = strings.Split(env, ",")
		}
		// 3. Per-Engine Override
		if env := os.Getenv(prefix + "_INCLUDE"); env != "" {
			includePatterns = strings.Split(env, ",")
		}
		// Clean up patterns
		for i := range includePatterns {
			includePatterns[i] = strings.TrimSpace(includePatterns[i])
		}

		pollInterval := 60 * time.Second
		if env := os.Getenv("POLL_INTERVAL"); env != "" {
			if val, err := strconv.Atoi(env); err == nil && val > 0 {
				pollInterval = time.Duration(val) * time.Second
			}
		}

		watchInterval := 12 * time.Hour
		if env := os.Getenv("WATCH_INTERVAL"); env != "" {
			if val, err := strconv.Atoi(env); err == nil && val > 0 {
				watchInterval = time.Duration(val) * time.Second
			}
		}

		engine := sync.NewEngine(sync.SyncConfig{
			ID: id, SourceDir: src, TargetDir: resolvedTgt, Rule: rule,
			ExcludePatterns: []string{".git", ".DS_Store", "Thumbs.db"},
			IncludePatterns: includePatterns,
			BandwidthLimit:  bwlimitBytes,
			PollInterval:    pollInterval, WatchInterval: watchInterval, AutoApproveDeletions: database.GetSetting("auto_approve", "off") == "on",
			DryRunFunc: func() bool { return database.GetSetting("sync_mode", "dry") == "dry" },
			OnSyncEvent: func(ts, act, p string, sz int64) {
				_ = database.LogEvent(ts, act, p, sz, id)
				item := database.HistoryItem{Time: ts, Action: act, Path: p, Size: database.FormatBytes(sz)}
				wsHub.Broadcast("history", item)
				wsHub.Broadcast("stats", database.GetTrafficStats())
				wsHub.Broadcast("daily", database.GetDailyTraffic(7))
				healthState.ReportSuccess(notifier.Send)
			},
			OnError: func(msg string) { healthState.ReportError(msg, notifier.Send) },
		})

		if err := engine.Start(); err == nil {
			engine.SetHealthState(healthState)
			engines = append(engines, engine)
			// Only pause if successfully started
			if database.GetSetting("engine_paused_"+id, "false") == "true" {
				engine.Pause()
			}
		} else {
			fmt.Printf("Failed to start engine %s: %v\n", id, err)
		}
	}
	return engines
}

func startSyncStatusBroadcaster(wsHub *websocket.Hub, syncEngines []*sync.Engine, healthState *health.State, latency *int64) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		var totalSpeed int64
		var totalRemaining int64
		allPaused := true
		atomicLatency := atomic.LoadInt64(latency)
		type EngineProgress struct {
			ID                string  `json:"id"`
			File              string  `json:"file"`
			Percent           float64 `json:"percent"`
			Speed             string  `json:"speed"`
			Today             string  `json:"today"`
			Total             string  `json:"total"`
			IsActive          bool    `json:"is_active"`
			ETA               string  `json:"eta"`
			QueueCount        int     `json:"queue_count"`
			IsScanning        bool    `json:"is_scanning"`
			AvgSpeed          string  `json:"avg_speed"`
			Elapsed           string  `json:"elapsed"`
			SpeedHistory      []int64 `json:"speed_history"`
			IsPaused          bool    `json:"is_paused"`
			LastSync          string  `json:"last_sync"`
			IsRemoteScan      bool    `json:"is_remote_scan"`
			IsWaitingApproval bool    `json:"is_waiting_approval"`
		}
		engineStats := make([]EngineProgress, 0)
		for _, engine := range syncEngines {
			isPaused := engine.IsPaused()
			if !isPaused {
				allPaused = false
			}
			file, transferredBytes, totalBytes, speed, avgSpeed, startTs := engine.GetTransferStatsExtended()
			totalSpeed += speed

			elapsedStr := "0s"
			if !startTs.IsZero() {
				dur := time.Since(startTs)
				if dur.Hours() >= 1 {
					elapsedStr = fmt.Sprintf("%dh %dm", int(dur.Hours()), int(dur.Minutes())%60)
				} else if dur.Minutes() >= 1 {
					elapsedStr = fmt.Sprintf("%dm %ds", int(dur.Minutes()), int(dur.Seconds())%60)
				} else {
					elapsedStr = fmt.Sprintf("%ds", int(dur.Seconds()))
				}
			}

			totalRemaining += (engine.GetPlanRemainingBytes() - transferredBytes)
			queuedCount, queuedSize := engine.GetQueuedStats()
			totalRemaining += queuedSize

			percent := 0.0
			if totalBytes > 0 {
				percent = float64(transferredBytes) / float64(totalBytes) * 100
			}
			stats := database.GetEngineTrafficStats(engine.GetConfig().ID)
			etaStr := "Done"
			if speed > 0 && totalBytes > transferredBytes {
				rem := totalBytes - transferredBytes
				sec := rem / speed
				if sec > 3600 {
					etaStr = fmt.Sprintf("%dh %dm", sec/3600, (sec%3600)/60)
				} else if sec > 60 {
					etaStr = fmt.Sprintf("%dm %ds", sec/60, sec%60)
				} else {
					etaStr = fmt.Sprintf("%ds", sec)
				}
			}
			engineStats = append(engineStats, EngineProgress{
				ID: engine.GetConfig().ID, File: filepath.Base(file), Percent: percent, Speed: database.FormatBytes(speed) + "/s", Today: database.FormatBytes(stats.Today), Total: database.FormatBytes(stats.Total), IsActive: speed > 0, ETA: etaStr, QueueCount: queuedCount, IsScanning: engine.IsScanning(),
				AvgSpeed: database.FormatBytes(avgSpeed) + "/s", Elapsed: elapsedStr, SpeedHistory: engine.GetSpeedHistory(), IsPaused: isPaused, LastSync: engine.GetLastSyncTime().Format(time.RFC3339), IsRemoteScan: engine.IsRemoteScan(),
				IsWaitingApproval: engine.IsWaitingForApproval(),
			})
		}
		state := "ACTIVE"
		progress := "Monitoring..."
		if len(syncEngines) > 0 {
			if allPaused {
				state = "PAUSED"
				progress = "Sync Paused"
			} else if totalSpeed > 0 {
				state = "SYNCING"
				progress = "Transferring..."
			}
		}
		globalEta := "Done"
		if totalSpeed > 0 && totalRemaining > 0 {
			sec := totalRemaining / totalSpeed
			if sec > 3600 {
				globalEta = fmt.Sprintf("%dh %dm", sec/3600, (sec%3600)/60)
			} else if sec > 60 {
				globalEta = fmt.Sprintf("%dm %ds", sec/60, sec%60)
			} else {
				globalEta = fmt.Sprintf("%ds", sec)
			}
		}

		latency := atomicLatency

		receiverHealthy, receiverMsg, receiverVersion, receiverUptime := healthState.GetReceiverStatus()
		traffic := database.GetTrafficStats()
		wsHub.Broadcast("progress", map[string]interface{}{
			"speed": database.FormatBytes(totalSpeed) + "/s", "state": state, "engines": engineStats, "eta": globalEta, "latency": latency,
			"top_files":        database.GetTopFiles(),
			"receiver_healthy": receiverHealthy,
			"receiver_msg":     receiverMsg,
			"receiver_version": receiverVersion,
			"receiver_uptime":  receiverUptime,
			"traffic_today":    database.FormatBytes(traffic.Today),
			"traffic_total":    database.FormatBytes(traffic.Total),
		})
		wsHub.Broadcast("sync_status", map[string]interface{}{"status": progress, "engines": len(syncEngines)})
	}
}

func checkReceiverHealth(healthState *health.State, engines []*sync.Engine, latency *int64) {
	destHost := os.Getenv("DEST_HOST")
	if destHost == "" {
		return
	}
	targetURL := fmt.Sprintf("http://%s:8080/health", destHost)
	client := http.Client{Timeout: 5 * time.Second}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		start := time.Now()
		resp, err := client.Get(targetURL)
		if err == nil {
			atomic.StoreInt64(latency, time.Since(start).Milliseconds())
		}
		var version, uptime string
		healthy := false
		msg := ""
		if err == nil {
			var data struct {
				Status  string `json:"status"`
				Version string `json:"version"`
				Uptime  string `json:"uptime"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&data); err == nil {
				healthy = true
				version = data.Version
				uptime = data.Uptime
			}
			if err := resp.Body.Close(); err != nil {
				fmt.Printf("Error closing receiver health body: %v\n", err)
			}
		}
		if !healthy && len(engines) > 0 {
			if _, err := os.Stat(engines[0].GetConfig().TargetDir); err == nil {
				healthy = true
				msg = "Storage Access OK (Agent Offline)"
			}
		}
		if !healthy {
			msg = fmt.Sprintf("Agent Unreachable (%s)", destHost)
		}
		healthState.ReportReceiverStatus(healthy, msg, version, uptime)
	}
}
