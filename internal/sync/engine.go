package sync

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	stdsync "sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"schnorarr/internal/monitor/database"
	"schnorarr/internal/monitor/health"
)

// Engine is the main sync orchestrator
type Engine struct {
	config             SyncConfig
	scanner            *Scanner
	transferer         *Transferer
	watcher            *fsnotify.Watcher
	stopCh             chan struct{}
	pausedMu           stdsync.RWMutex
	paused             bool
	lastSyncTime       time.Time
	lastSourceManifest *Manifest // Cached source manifest for quick polling comparison
	targetManifest     *Manifest // In-memory cache of receiver state
	syncMu             stdsync.Mutex
	syncQueued         bool      // True if a sync is requested while one is running
	queuedManifest     *Manifest // Store provided manifest for the queued run

	// Progress Tracking
	currentSpeed    int64
	currentFile     string
	currentProgress int64 // Bytes
	totalFileSize   int64
	lastUpdate      time.Time
	lastBytes       int64
	lastLogTime     time.Time
	lastLogBytes    int64
	planRemainingBytes int64 // Sum of sizes of files in current plan yet to complete
	isScanning      bool
	
	// Transfer Detail Tracking
	fileStartTime   time.Time
	avgSpeed        int64

	// UX Features
	alias           string
	speedHistory    []int64 // Last 60 seconds of speed samples
	healthState     *health.State // Reference to global health state for override settings

	// Deletion Approval Safety Lock
	pendingDeletions   []string
	waitingForApproval bool
	deletionAllowed    bool
}

// NewEngine creates a new sync engine
func NewEngine(config SyncConfig) *Engine {
	scanner := NewScanner()
	scanner.ExcludePatterns = config.ExcludePatterns

	e := &Engine{
		config:       config,
		scanner:      scanner,
		stopCh:       make(chan struct{}),
		alias:        database.GetSetting("alias_"+config.ID, "Engine #"+config.ID),
		speedHistory: make([]int64, 60),
	}

	transferer := NewTransferer(TransferOptions{
		BandwidthLimit: config.BandwidthLimit,
		CheckPaused: func() bool {
			return e.IsPaused()
		},
		OnProgress: func(path string, bytesTransferred, totalBytes int64) {
			e.pausedMu.Lock()
			defer e.pausedMu.Unlock()

			e.currentFile = path
			e.currentProgress = bytesTransferred
			e.totalFileSize = totalBytes

			now := time.Now()
			if e.fileStartTime.IsZero() {
				e.fileStartTime = now
			} else {
				elapsed := now.Sub(e.fileStartTime).Seconds()
				if elapsed > 0.5 {
					e.avgSpeed = int64(float64(bytesTransferred) / elapsed)
				}
			}

			if e.lastUpdate.IsZero() {
				e.lastUpdate = now; e.lastBytes = bytesTransferred; return
			}

			if now.Sub(e.lastUpdate) >= time.Second {
				diff := bytesTransferred - e.lastBytes
				e.currentSpeed = diff; e.lastUpdate = now; e.lastBytes = bytesTransferred
				_ = database.AddTraffic(e.config.ID, diff)
				if len(e.speedHistory) < 60 { e.speedHistory = make([]int64, 60) }
				e.speedHistory = append(e.speedHistory[1:], diff)
			}

			if now.Sub(e.lastLogTime) >= 5*time.Second {
				percent := 0.0
				if totalBytes > 0 { percent = float64(bytesTransferred) / float64(totalBytes) * 100 }
				speedStr := database.FormatBytes(e.currentSpeed)
				log.Printf("[%s] Syncing %s: %.1f%% (%s/s)", e.config.ID, filepath.Base(path), percent, speedStr)
				e.lastLogTime = now; e.lastLogBytes = bytesTransferred
			}
		},
		OnComplete: func(path string, size int64, err error) {
			e.pausedMu.Lock(); defer e.pausedMu.Unlock()
			e.currentSpeed = 0; e.currentFile = ""; e.currentProgress = 0; e.totalFileSize = 0
			e.fileStartTime = time.Time{}
			e.avgSpeed = 0
		},
	})

	e.transferer = transferer
	return e
}

func (e *Engine) SetHealthState(s *health.State) { e.healthState = s }

func (e *Engine) Start() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil { return fmt.Errorf("failed to create watcher: %w", err) }
	e.watcher = watcher
	if err := e.addWatchRecursive(e.config.SourceDir); err != nil { return fmt.Errorf("failed to add watches: %w", err) }
	go func() { _ = e.RunSync(nil) }()
	go e.watchLoop()
	if e.config.WatchInterval > 0 { go e.periodicSyncLoop() }
	if e.config.PollInterval > 0 { go e.sourcePollLoop() }
	log.Printf("Sync engine started: %s -> %s", e.config.SourceDir, e.config.TargetDir)
	return nil
}

func (e *Engine) Stop() {
	close(e.stopCh)
	if e.watcher != nil { e.watcher.Close() }
}

func (e *Engine) IsBusy() bool {
	e.pausedMu.RLock(); queued := e.syncQueued; e.pausedMu.RUnlock()
	if queued { return true }
	if !e.syncMu.TryLock() { return true }
	e.syncMu.Unlock(); return false
}

func (e *Engine) PreviewSync() (*SyncPlan, error) {
	e.pausedMu.RLock(); isPaused := e.paused; e.pausedMu.RUnlock()
	if isPaused { return nil, fmt.Errorf("sync is paused") }
	sourceManifest, err := e.scanner.ScanLocal(e.config.SourceDir)
	if err != nil { return nil, fmt.Errorf("failed to scan source: %w", err) }
	if e.targetManifest == nil {
		cachePath := e.getCachePath(); e.targetManifest, err = LoadFromFile(cachePath)
		if err != nil {
			e.targetManifest, err = e.scanner.ScanLocal(e.config.TargetDir)
			if err != nil { e.targetManifest = NewManifest(e.config.TargetDir) }
		}
	}
	plan := CompareManifests(sourceManifest, e.targetManifest, e.config.Rule)
	return plan, nil
}

func (e *Engine) RunSync(sourceManifest *Manifest) error {
	e.pausedMu.RLock(); isPaused := e.paused; e.pausedMu.RUnlock()
	if isPaused { return fmt.Errorf("sync is paused") }
	if !e.syncMu.TryLock() {
		e.pausedMu.Lock(); e.syncQueued = true
		if sourceManifest != nil { e.queuedManifest = sourceManifest }
		e.pausedMu.Unlock(); return nil
	}
	defer func() {
		e.syncMu.Unlock(); e.pausedMu.Lock(); wasQueued := e.syncQueued; nextManifest := e.queuedManifest
		e.syncQueued = false; e.queuedManifest = nil; e.pausedMu.Unlock()
		if wasQueued { time.Sleep(1 * time.Second); go func() { _ = e.RunSync(nextManifest) }() }
	}()

	start := time.Now()
	if sourceManifest == nil {
		e.pausedMu.Lock(); e.isScanning = true; e.pausedMu.Unlock()
		var err error; sourceManifest, err = e.scanner.ScanLocal(e.config.SourceDir)
		e.pausedMu.Lock(); e.isScanning = false; e.pausedMu.Unlock()
		if err != nil { return fmt.Errorf("failed to scan source: %w", err) }
	}
	if e.targetManifest == nil {
		cachePath := e.getCachePath(); var err error; e.targetManifest, err = LoadFromFile(cachePath)
		if err != nil {
			e.targetManifest, err = e.scanner.ScanLocal(e.config.TargetDir)
			if err != nil { e.targetManifest = NewManifest(e.config.TargetDir) }
		}
	}

	plan := CompareManifests(sourceManifest, e.targetManifest, e.config.Rule)

	if len(plan.FilesToSync) == 0 && len(plan.FilesToDelete) == 0 && len(plan.DirsToCreate) == 0 && len(plan.DirsToDelete) == 0 && len(plan.Renames) == 0 {
		e.lastSyncTime = time.Now(); e.lastSourceManifest = sourceManifest
		return nil
	}

	var totalPlanSize int64
	for _, f := range plan.FilesToSync { totalPlanSize += f.Size }
	e.pausedMu.Lock(); e.planRemainingBytes = totalPlanSize; e.pausedMu.Unlock()

	log.Printf("[%s] Sync Plan: %d syncs, %d deletes, %d renames, %d mkdirs, %d conflicts",
		e.config.ID, len(plan.FilesToSync), len(plan.FilesToDelete), len(plan.Renames), len(plan.DirsToCreate), len(plan.Conflicts))

	hasChanges := len(plan.FilesToSync) > 0 || len(plan.FilesToDelete) > 0 || len(plan.Renames) > 0 || len(plan.DirsToCreate) > 0
	syncMode := database.GetSetting("sync_mode", "dry")
	
	e.pausedMu.Lock()
	if hasChanges && syncMode == "manual" && !e.deletionAllowed {
		e.waitingForApproval = true; e.pendingDeletions = nil
		for _, f := range plan.FilesToSync { e.pendingDeletions = append(e.pendingDeletions, f.Path) }
		e.pendingDeletions = append(e.pendingDeletions, plan.FilesToDelete...)
		for oldP := range plan.Renames { e.pendingDeletions = append(e.pendingDeletions, oldP) }
		e.pausedMu.Unlock(); return nil
	}
	if len(plan.Conflicts) > 0 && !e.deletionAllowed && e.healthState != nil && !e.healthState.IsOverrideEnabled() {
		e.waitingForApproval = true; e.pendingDeletions = nil
		for _, c := range plan.Conflicts { e.pendingDeletions = append(e.pendingDeletions, c.Path) }
		e.pausedMu.Unlock(); return nil
	}
	hasDeletions := len(plan.FilesToDelete) > 0 || len(plan.DirsToDelete) > 0
	if hasDeletions && !e.config.AutoApproveDeletions && !e.deletionAllowed {
		e.waitingForApproval = true; e.pendingDeletions = append(plan.FilesToDelete, plan.DirsToDelete...); e.pausedMu.Unlock(); return nil
	}

	if e.deletionAllowed {
		if len(e.pendingDeletions) > 0 {
			allowed := make(map[string]bool)
			for _, f := range e.pendingDeletions { allowed[f] = true }
			var filteredSyncs []*FileInfo
			for _, f := range plan.FilesToSync { if allowed[f.Path] { filteredSyncs = append(filteredSyncs, f) } }
			plan.FilesToSync = filteredSyncs
			var filteredFiles []string
			for _, f := range plan.FilesToDelete { if allowed[f] { filteredFiles = append(filteredFiles, f) } }
			plan.FilesToDelete = filteredFiles
			filteredRenames := make(map[string]string)
			for oldP, newP := range plan.Renames { if allowed[oldP] || allowed[newP] { filteredRenames[oldP] = newP } }
			plan.Renames = filteredRenames
		}
		e.deletionAllowed = false; e.waitingForApproval = false; e.pendingDeletions = nil
	}
	e.pausedMu.Unlock()

	touchedDirs, err := e.executeSyncPhase(plan, e.targetManifest)
	if err != nil { 
		database.ReportEngineError(e.config.ID, err.Error())
		return fmt.Errorf("sync failed: %w", err) 
	}
	if err := e.executeCleanupPhase(plan, e.targetManifest, touchedDirs); err != nil {
		database.ReportEngineError(e.config.ID, err.Error())
		return fmt.Errorf("cleanup failed: %w", err)
	}

	database.ReportEngineSuccess(e.config.ID)
	_ = e.targetManifest.SaveToFile(e.getCachePath())
	e.lastSyncTime = time.Now(); e.lastSourceManifest = sourceManifest
	log.Printf("[%s] Sync completed in %v", e.config.ID, time.Since(start))
	return nil
}

func (e *Engine) getCachePath() string {
	configDir := os.Getenv("CONFIG_DIR")
	if configDir == "" { configDir = "/config" }
	return filepath.Join(configDir, fmt.Sprintf("receiver_cache_%s.json", e.config.ID))
}

func (e *Engine) isDryRun() bool {
	if e.config.DryRunFunc != nil { return e.config.DryRunFunc() }
	return e.config.DryRun
}

func (e *Engine) reportEvent(timestamp, action, path string, size int64) {
	if e.config.OnSyncEvent != nil { e.config.OnSyncEvent(timestamp, action, path, size) }
}

func (e *Engine) reportError(msg string) {
	if e.config.OnError != nil { e.config.OnError(msg) }
}

func (e *Engine) watchLoop() {
	debounceTimer := time.NewTicker(5 * time.Second); debounceTimer.Stop()
	needsSync := false
	for {
		select {
		case <-e.stopCh: return
		case event, ok := <-e.watcher.Events:
			if !ok { return }
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 { continue }
			if event.Op&fsnotify.Create != 0 { _ = e.addWatchRecursive(event.Name) }
			needsSync = true; debounceTimer.Stop(); debounceTimer = time.NewTicker(5 * time.Second)
		case <-debounceTimer.C:
			if needsSync { needsSync = false; _ = e.RunSync(nil) }
		}
	}
}

func (e *Engine) sourcePollLoop() {
	ticker := time.NewTicker(e.config.PollInterval); defer ticker.Stop()
	for {
		select {
		case <-e.stopCh: return
		case <-ticker.C:
			if e.IsPaused() { continue }
			currentSource, err := e.scanner.ScanLocal(e.config.SourceDir)
			if err != nil { continue }
			if e.lastSourceManifest != nil {
				plan := CompareManifests(currentSource, e.lastSourceManifest, e.config.Rule)
				if len(plan.FilesToSync) > 0 || len(plan.FilesToDelete) > 0 || len(plan.DirsToCreate) > 0 || len(plan.DirsToDelete) > 0 || len(plan.Renames) > 0 {
					go func() { _ = e.RunSync(currentSource) }()
				}
			} else { e.lastSourceManifest = currentSource }
		}
	}
}

func (e *Engine) periodicSyncLoop() {
	ticker := time.NewTicker(e.config.WatchInterval); defer ticker.Stop()
	for {
		select {
		case <-e.stopCh: return
		case <-ticker.C: go func() { _ = e.RunSync(nil) }()
		}
	}
}

func (e *Engine) addWatchRecursive(path string) error {
	return filepath.Walk(path, func(walkPath string, info os.FileInfo, err error) error {
		if err != nil { return err }
		if info.IsDir() {
			relPath, _ := filepath.Rel(e.config.SourceDir, walkPath)
			if e.scanner.shouldExclude(relPath) { return filepath.SkipDir }
			_ = e.watcher.Add(walkPath)
		}
		return nil
	})
}

func (e *Engine) Pause() { e.pausedMu.Lock(); e.paused = true; e.pausedMu.Unlock() }
func (e *Engine) Resume() {
	e.pausedMu.Lock(); e.paused = false; e.pausedMu.Unlock()
	go func() { _ = e.RunSync(nil) }()
}
func (e *Engine) IsPaused() bool { e.pausedMu.RLock(); defer e.pausedMu.RUnlock(); return e.paused }
func (e *Engine) IsScanning() bool { e.pausedMu.RLock(); defer e.pausedMu.RUnlock(); return e.isScanning }
func (e *Engine) GetTransferStats() (file string, progress, total, speed int64) {
	e.pausedMu.RLock(); defer e.pausedMu.RUnlock(); return e.currentFile, e.currentProgress, e.totalFileSize, e.currentSpeed
}
func (e *Engine) GetTransferStatsExtended() (file string, prog, total, speed, avg int64, start time.Time) {
	e.pausedMu.RLock(); defer e.pausedMu.RUnlock(); return e.currentFile, e.currentProgress, e.totalFileSize, e.currentSpeed, e.avgSpeed, e.fileStartTime
}
func (e *Engine) GetPlanRemainingBytes() int64 { e.pausedMu.RLock(); defer e.pausedMu.RUnlock(); return e.planRemainingBytes }
func (e *Engine) GetQueuedStats() (count int, size int64) {
	e.pausedMu.RLock(); defer e.pausedMu.RUnlock()
	if !e.syncQueued { return 0, 0 }
	if e.queuedManifest != nil {
		for _, f := range e.queuedManifest.Files { if !f.IsDir { size += f.Size; count++ } }
	} else { count = 1 }
	return count, size
}
func (e *Engine) GetLastSyncTime() time.Time { return e.lastSyncTime }
func (e *Engine) SetAutoApproveDeletions(enabled bool) {
	e.pausedMu.Lock(); defer e.pausedMu.Unlock(); e.config.AutoApproveDeletions = enabled
}
func (e *Engine) GetStatus() string {
	e.pausedMu.RLock(); defer e.pausedMu.RUnlock()
	status := "Running"; if e.paused { status = "Paused" }
	return fmt.Sprintf("[%s] %s: %s -> %s", e.config.ID, status, e.config.SourceDir, e.config.TargetDir)
}
func (e *Engine) ApproveDeletions() {
	e.pausedMu.Lock(); e.deletionAllowed = true; e.waitingForApproval = false; e.pausedMu.Unlock()
	go func() { _ = e.RunSync(nil) }()
}
func (e *Engine) ApproveSpecificChanges(files []string) {
	e.pausedMu.Lock(); e.deletionAllowed = true; e.waitingForApproval = false; e.pendingDeletions = files; e.pausedMu.Unlock()
	go func() { _ = e.RunSync(nil) }()
}
func (e *Engine) IsWaitingForApproval() bool { e.pausedMu.RLock(); defer e.pausedMu.RUnlock(); return e.waitingForApproval }
func (e *Engine) GetPendingDeletions() []string {
	e.pausedMu.RLock(); defer e.pausedMu.RUnlock(); if e.pendingDeletions == nil { return []string{} }; return e.pendingDeletions
}
func (e *Engine) GetAlias() string { return e.alias }
func (e *Engine) SetAlias(alias string) { e.pausedMu.Lock(); defer e.pausedMu.Unlock(); e.alias = alias }
func (e *Engine) GetSpeedHistory() []int64 {
	e.pausedMu.RLock()
	defer e.pausedMu.RUnlock()
	res := make([]int64, len(e.speedHistory))
	copy(res, e.speedHistory)
	return res
}
