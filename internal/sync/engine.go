package sync

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	stdsync "sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// SyncConfig configures the sync engine
type SyncConfig struct {
	// ID is a unique identifier for this sync engine (used for caching)
	ID string
	// SourceDir is the directory to watch and sync from
	SourceDir string
	// TargetDir is the destination directory
	TargetDir string
	// Rule describes the sync strategy (e.g., "flat", "series")
	Rule string
	// ExcludePatterns are glob patterns to exclude from syncing
	ExcludePatterns []string
	// BandwidthLimit in bytes per second (0 = unlimited)
	BandwidthLimit int64
	// WatchInterval is how often to perform full scans (0 = only on file changes)
	WatchInterval time.Duration
	// PollInterval is how often to poll the source directory for changes (for Docker/Windows compatibility)
	PollInterval time.Duration
	// DryRun when true, logs what would be synced without actually syncing
	DryRun bool
	// DryRunFunc optional callback to check dry run status dynamically
	DryRunFunc func() bool
	// OnSyncEvent callback for sync events (timestamp, action, path, size)
	OnSyncEvent func(timestamp, action, path string, size int64)
	// OnError callback for errors
	OnError func(msg string)
}

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
}

// GetConfig returns the engine configuration
func (e *Engine) GetConfig() SyncConfig {
	return e.config
}

// NewEngine creates a new sync engine
func NewEngine(config SyncConfig) *Engine {
	scanner := NewScanner()
	scanner.ExcludePatterns = config.ExcludePatterns

	transferer := NewTransferer(TransferOptions{
		BandwidthLimit: config.BandwidthLimit,
	})

	return &Engine{
		config:     config,
		scanner:    scanner,
		transferer: transferer,
		stopCh:     make(chan struct{}),
	}
}

// Start begins the sync engine in continuous watch mode
func (e *Engine) Start() error {
	// Initial full sync
	if err := e.RunSync(nil); err != nil {
		return fmt.Errorf("initial sync failed: %w", err)
	}

	// Set up file watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create watcher: %w", err)
	}
	e.watcher = watcher

	// Watch source directory recursively
	if err := e.addWatchRecursive(e.config.SourceDir); err != nil {
		return fmt.Errorf("failed to add watches: %w", err)
	}

	// Start watch loop
	go e.watchLoop()

	// Start periodic full sync if configured
	if e.config.WatchInterval > 0 {
		go e.periodicSyncLoop()
	}

	// Start source polling if configured
	if e.config.PollInterval > 0 {
		go e.sourcePollLoop()
	}

	log.Printf("Sync engine started: %s -> %s", e.config.SourceDir, e.config.TargetDir)
	return nil
}

// Stop stops the sync engine
func (e *Engine) Stop() {
	close(e.stopCh)
	if e.watcher != nil {
		e.watcher.Close()
	}
	log.Println("Sync engine stopped")
}

// IsBusy returns true if a sync is currently in progress
func (e *Engine) IsBusy() bool {
	if !e.syncMu.TryLock() {
		return true
	}
	e.syncMu.Unlock()
	return false
}

// PreviewSync calculates a sync plan without executing it
func (e *Engine) PreviewSync() (*SyncPlan, error) {
	e.pausedMu.RLock()
	isPaused := e.paused
	e.pausedMu.RUnlock()

	if isPaused {
		return nil, fmt.Errorf("sync is paused")
	}

	// Scan source
	sourceManifest, err := e.scanner.ScanLocal(e.config.SourceDir)
	if err != nil {
		return nil, fmt.Errorf("failed to scan source: %w", err)
	}

	// Get target manifest (prefer in-memory cache)
	if e.targetManifest == nil {
		cachePath := e.getCachePath()
		var err error
		e.targetManifest, err = LoadFromFile(cachePath)
		if err != nil {
			e.targetManifest, err = e.scanner.ScanLocal(e.config.TargetDir)
			if err != nil {
				e.targetManifest = NewManifest(e.config.TargetDir)
			}
		}
	}

	// Compare
	return CompareManifests(sourceManifest, e.targetManifest), nil
}

// RunSync performs a one-time sync operation
// If sourceManifest is provided, it uses it instead of scanning the source directory
func (e *Engine) RunSync(sourceManifest *Manifest) error {
	e.pausedMu.RLock()
	isPaused := e.paused
	e.pausedMu.RUnlock()

	if isPaused {
		return fmt.Errorf("sync is paused")
	}

	// Prevent concurrent syncs on the same engine
	if !e.syncMu.TryLock() {
		return fmt.Errorf("sync already in progress")
	}
	defer e.syncMu.Unlock()

	startTime := time.Now()

	// 1. Scan source if not provided
	if sourceManifest == nil {
		var err error
		sourceManifest, err = e.scanner.ScanLocal(e.config.SourceDir)
		if err != nil {
			return fmt.Errorf("failed to scan source: %w", err)
		}
	}

	// 2. Get target manifest (prefer in-memory cache)
	if e.targetManifest == nil {
		cachePath := e.getCachePath()
		var err error
		e.targetManifest, err = LoadFromFile(cachePath)
		if err != nil {
			log.Printf("[%s] No valid cache found, performing full target scan: %v", e.config.ID, err)
			e.targetManifest, err = e.scanner.ScanLocal(e.config.TargetDir)
			if err != nil {
				e.targetManifest = NewManifest(e.config.TargetDir)
			}
		} else {
			log.Printf("[%s] Loaded receiver manifest from disk (%d files)", e.config.ID, len(e.targetManifest.Files))
		}
	}

	// 3. Compare
	plan := CompareManifests(sourceManifest, e.targetManifest)
	if len(plan.FilesToSync) == 0 && len(plan.FilesToDelete) == 0 && len(plan.DirsToCreate) == 0 && len(plan.DirsToDelete) == 0 && len(plan.Renames) == 0 {
		log.Printf("[%s] Sync skipped: No changes detected", e.config.ID)
		e.lastSyncTime = time.Now()
		e.lastSourceManifest = sourceManifest
		return nil
	}

	log.Printf("[%s] Starting sync: %s -> %s", e.config.ID, e.config.SourceDir, e.config.TargetDir)

	// 4. Execute
	if err := e.executePlan(plan, e.targetManifest); err != nil {
		return fmt.Errorf("failed to execute sync plan: %w", err)
	}

	// 5. Save cache
	cachePath := e.getCachePath()
	if err := e.targetManifest.SaveToFile(cachePath); err != nil {
		log.Printf("[%s] Warning: failed to save target manifest cache: %v", e.config.ID, err)
	} else {
		log.Printf("[%s] Receiver cache updated on disk", e.config.ID)
	}

	e.lastSyncTime = time.Now()
	e.lastSourceManifest = sourceManifest
	log.Printf("[%s] Sync completed in %v", e.config.ID, time.Since(startTime))
	return nil
}

func (e *Engine) getCachePath() string {
	configDir := os.Getenv("CONFIG_DIR")
	if configDir == "" {
		configDir = "/config"
	}
	return filepath.Join(configDir, fmt.Sprintf("receiver_cache_%s.json", e.config.ID))
}

// executePlan executes the sync plan and updates targetManifest to reflect changes
func (e *Engine) executePlan(plan *SyncPlan, targetManifest *Manifest) error {
	timestamp := time.Now().Format("2006/01/02 15:04:05")

	// Log dry-run status
	isDryRun := e.config.DryRun
	if e.config.DryRunFunc != nil {
		isDryRun = e.config.DryRunFunc()
	}

	if isDryRun {
		log.Printf("[%s] === DRY RUN MODE - No changes will be made ===", e.config.ID)
		log.Printf("[%s] Sync plan: %d dirs to create, %d renames, %d files to sync, %d files to delete, %d dirs to delete",
			e.config.ID, len(plan.DirsToCreate), len(plan.Renames), len(plan.FilesToSync), len(plan.FilesToDelete), len(plan.DirsToDelete))
	}

	// Create directories first
	for _, dirPath := range plan.DirsToCreate {
		fullPath := filepath.Join(e.config.TargetDir, dirPath)
		if isDryRun {
			log.Printf("[%s] [DRY RUN] Would create directory: %s", e.config.ID, dirPath)
			if e.config.OnSyncEvent != nil {
				e.config.OnSyncEvent(timestamp, "DRY-Created", dirPath, 0)
			}
		} else {
			if err := e.transferer.CreateDir(fullPath); err != nil {
				if e.config.OnError != nil {
					e.config.OnError(fmt.Sprintf("Failed to create dir %s: %v", dirPath, err))
				}
				continue
			}
			log.Printf("[%s] Created directory: %s", e.config.ID, dirPath)

			// Update in-memory manifest
			targetManifest.Add(&FileInfo{
				Path:  filepath.ToSlash(dirPath),
				IsDir: true,
			})
		}
	}

	// Handle Renames
	for oldPath, newPath := range plan.Renames {
		if isDryRun {
			log.Printf("[%s] [DRY RUN] Would rename: %s -> %s", e.config.ID, oldPath, newPath)
			if e.config.OnSyncEvent != nil {
				e.config.OnSyncEvent(timestamp, "DRY-Renamed", fmt.Sprintf("%s -> %s", oldPath, newPath), 0)
			}
		} else {
			oldFullPath := filepath.Join(e.config.TargetDir, oldPath)
			newFullPath := filepath.Join(e.config.TargetDir, newPath)

			if err := e.transferer.RenameFile(oldFullPath, newFullPath); err != nil {
				log.Printf("[%s] Failed to rename %s to %s: %v", e.config.ID, oldPath, newPath, err)
				// If rename fails, we should really add it back to FilesToSync, but for now we just log
				continue
			}

			log.Printf("[%s] Renamed: %s -> %s", e.config.ID, oldPath, newPath)

			// Update in-memory manifest
			if file, exists := targetManifest.Files[oldPath]; exists {
				delete(targetManifest.Files, oldPath)
				file.Path = newPath
				targetManifest.Files[newPath] = file
			}

			// Report sync event
			if e.config.OnSyncEvent != nil {
				e.config.OnSyncEvent(timestamp, "Renamed", fmt.Sprintf("%s -> %s", oldPath, newPath), 0)
			}
		}
	}

	// Copy/update files
	for _, file := range plan.FilesToSync {
		if isDryRun {
			log.Printf("[%s] [DRY RUN] Would sync file: %s (%d bytes)", e.config.ID, file.Path, file.Size)
			if e.config.OnSyncEvent != nil {
				e.config.OnSyncEvent(timestamp, "DRY-Added", file.Path, file.Size)
			}
		} else {
			srcPath := filepath.Join(e.config.SourceDir, file.Path)
			dstPath := filepath.Join(e.config.TargetDir, file.Path)

			if err := e.transferer.CopyFile(srcPath, dstPath); err != nil {
				if e.config.OnError != nil {
					e.config.OnError(fmt.Sprintf("Failed to copy %s: %v", file.Path, err))
				}
				continue
			}

			log.Printf("[%s] Synced file: %s (%d bytes)", e.config.ID, file.Path, file.Size)

			// Update in-memory manifest
			targetManifest.Add(&FileInfo{
				Path:    file.Path,
				Size:    file.Size,
				ModTime: file.ModTime,
				IsDir:   false,
			})

			// Report sync event
			if e.config.OnSyncEvent != nil {
				e.config.OnSyncEvent(timestamp, "Added", file.Path, file.Size)
			}
		}
	}

	// Delete files
	for _, filePath := range plan.FilesToDelete {
		if isDryRun {
			log.Printf("[%s] [DRY RUN] Would delete file: %s", e.config.ID, filePath)
			if e.config.OnSyncEvent != nil {
				e.config.OnSyncEvent(timestamp, "DRY-Deleted", filePath, 0)
			}
		} else {
			fullPath := filepath.Join(e.config.TargetDir, filePath)
			if err := e.transferer.DeleteFile(fullPath); err != nil {
				if e.config.OnError != nil {
					e.config.OnError(fmt.Sprintf("Failed to delete file %s: %v", filePath, err))
				}
				continue
			}
			log.Printf("[%s] Deleted file: %s", e.config.ID, filePath)

			// Update in-memory manifest
			delete(targetManifest.Files, filePath)

			// Report sync event
			if e.config.OnSyncEvent != nil {
				e.config.OnSyncEvent(timestamp, "Deleted", filePath, 0)
			}
		}
	}

	// Delete directories (in reverse order to handle nested dirs)
	for i := len(plan.DirsToDelete) - 1; i >= 0; i-- {
		dirPath := plan.DirsToDelete[i]
		if isDryRun {
			log.Printf("[%s] [DRY RUN] Would delete directory: %s", e.config.ID, dirPath)
			if e.config.OnSyncEvent != nil {
				e.config.OnSyncEvent(timestamp, "DRY-Deleted", dirPath, 0)
			}
		} else {
			fullPath := filepath.Join(e.config.TargetDir, dirPath)
			if err := e.transferer.DeleteDir(fullPath); err != nil {
				if e.config.OnError != nil {
					e.config.OnError(fmt.Sprintf("Failed to delete dir %s: %v", dirPath, err))
				}
				continue
			}
			log.Printf("[%s] Deleted directory: %s", e.config.ID, dirPath)

			// Update in-memory manifest
			delete(targetManifest.Dirs, dirPath)
			delete(targetManifest.Files, dirPath)
		}
	}

	if isDryRun {
		log.Printf("[%s] === DRY RUN COMPLETE - No changes were made ===", e.config.ID)
	}

	return nil
}

// watchLoop monitors file system events
func (e *Engine) watchLoop() {
	// Debounce timer to avoid syncing too often
	debounceTimer := time.NewTimer(5 * time.Second)
	debounceTimer.Stop()
	needsSync := false

	for {
		select {
		case <-e.stopCh:
			return

		case event, ok := <-e.watcher.Events:
			if !ok {
				return
			}

			// Ignore events we don't care about
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 {
				continue
			}

			log.Printf("File event: %s %s", event.Op, event.Name)

			// If a new directory was created, add it to watcher
			if event.Op&fsnotify.Create != 0 {
				if err := e.addWatchRecursive(event.Name); err != nil {
					log.Printf("Failed to add watch for %s: %v", event.Name, err)
				}
			}

			// Trigger debounced sync
			needsSync = true
			debounceTimer.Reset(5 * time.Second)

		case err, ok := <-e.watcher.Errors:
			if !ok {
				return
			}
			if e.config.OnError != nil {
				e.config.OnError(fmt.Sprintf("Watcher error: %v", err))
			}

		case <-debounceTimer.C:
			if needsSync {
				needsSync = false
				if err := e.RunSync(nil); err != nil {
					if e.config.OnError != nil {
						e.config.OnError(fmt.Sprintf("Sync error: %v", err))
					}
				}
			}
		}
	}
}

// sourcePollLoop periodically checks the source for changes
func (e *Engine) sourcePollLoop() {
	ticker := time.NewTicker(e.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-e.stopCh:
			return
		case <-ticker.C:
			e.pausedMu.RLock()
			isPaused := e.paused
			e.pausedMu.RUnlock()

			if isPaused {
				continue
			}

			// Scan source only (fast)
			currentSource, err := e.scanner.ScanLocal(e.config.SourceDir)
			if err != nil {
				log.Printf("[%s] Polling error: %v", e.config.ID, err)
				continue
			}

			// If we have a previous manifest, compare them
			if e.lastSourceManifest != nil {
				plan := CompareManifests(currentSource, e.lastSourceManifest)
				if len(plan.FilesToSync) > 0 || len(plan.FilesToDelete) > 0 || len(plan.DirsToCreate) > 0 || len(plan.DirsToDelete) > 0 || len(plan.Renames) > 0 {
					log.Printf("[%s] Polling detected changes on source, triggering sync", e.config.ID)
					if err := e.RunSync(currentSource); err != nil {
						log.Printf("[%s] Polling-triggered sync error: %v", e.config.ID, err)
					}
				}
			} else {
				// First poll, just store the manifest
				e.lastSourceManifest = currentSource
			}
		}
	}
}

// periodicSyncLoop performs periodic full syncs
func (e *Engine) periodicSyncLoop() {
	ticker := time.NewTicker(e.config.WatchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-e.stopCh:
			return
		case <-ticker.C:
			if err := e.RunSync(nil); err != nil {
				if e.config.OnError != nil {
					e.config.OnError(fmt.Sprintf("Periodic sync error: %v", err))
				}
			}
		}
	}
}

// addWatchRecursive adds a directory and all subdirectories to the watcher
func (e *Engine) addWatchRecursive(path string) error {
	return filepath.Walk(path, func(walkPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Check exclusions
			relPath, _ := filepath.Rel(e.config.SourceDir, walkPath)
			if e.scanner.shouldExclude(relPath) {
				return filepath.SkipDir
			}

			if err := e.watcher.Add(walkPath); err != nil {
				return fmt.Errorf("failed to watch %s: %w", walkPath, err)
			}
		}
		return nil
	})
}

// Pause pauses the sync engine
func (e *Engine) Pause() {
	e.pausedMu.Lock()
	defer e.pausedMu.Unlock()
	e.paused = true
	log.Println("Sync engine paused")
}

// Resume resumes the sync engine
func (e *Engine) Resume() {
	e.pausedMu.Lock()
	defer e.pausedMu.Unlock()
	e.paused = false
	log.Println("Sync engine resumed")

	// Trigger immediate sync
	go func() {
		if err := e.RunSync(nil); err != nil {
			if e.config.OnError != nil {
				e.config.OnError(fmt.Sprintf("Resume sync error: %v", err))
			}
		}
	}()
}

// IsPaused returns whether the engine is paused
func (e *Engine) IsPaused() bool {
	e.pausedMu.RLock()
	defer e.pausedMu.RUnlock()
	return e.paused
}

// SetBandwidthLimit updates the bandwidth limit
func (e *Engine) SetBandwidthLimit(limit int64) {
	e.transferer.SetBandwidthLimit(limit)
	log.Printf("Bandwidth limit updated: %d bytes/s", limit)
}

// GetLastSyncTime returns the time of the last successful sync
func (e *Engine) GetLastSyncTime() time.Time {
	return e.lastSyncTime
}

// GetStatus returns a human-readable status of the sync engine
func (e *Engine) GetStatus() string {
	e.pausedMu.RLock()
	defer e.pausedMu.RUnlock()

	status := "Running"
	if e.paused {
		status = "Paused"
	}

	return fmt.Sprintf("[%s] %s: %s -> %s (Last sync: %s)",
		e.config.ID,
		status,
		e.config.SourceDir,
		e.config.TargetDir,
		e.lastSyncTime.Format("15:04:05"),
	)
}
