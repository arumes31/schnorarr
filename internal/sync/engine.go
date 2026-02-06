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
	// SourceDir is the directory to watch and sync from
	SourceDir string
	// TargetDir is the destination directory
	TargetDir string
	// ExcludePatterns are glob patterns to exclude from syncing
	ExcludePatterns []string
	// BandwidthLimit in bytes per second (0 = unlimited)
	BandwidthLimit int64
	// WatchInterval is how often to perform full scans (0 = only on file changes)
	WatchInterval time.Duration
	// OnSyncEvent callback for sync events (timestamp, action, path, size)
	OnSyncEvent func(timestamp, action, path string, size int64)
	// OnError callback for errors
	OnError func(msg string)
}

// Engine is the main sync orchestrator
type Engine struct {
	config       SyncConfig
	scanner      *Scanner
	transferer   *Transferer
	watcher      *fsnotify.Watcher
	stopCh       chan struct{}
	pausedMu     stdsync.RWMutex
	paused       bool
	lastSyncTime time.Time
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
	if err := e.RunSync(); err != nil {
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

// RunSync performs a one-time sync operation
func (e *Engine) RunSync() error {
	e.pausedMu.RLock()
	if e.paused {
		e.pausedMu.RUnlock()
		return fmt.Errorf("sync is paused")
	}
	e.pausedMu.RUnlock()

	log.Printf("Starting sync: %s -> %s", e.config.SourceDir, e.config.TargetDir)
	startTime := time.Now()

	// Scan source directory
	sourceManifest, err := e.scanner.ScanLocal(e.config.SourceDir)
	if err != nil {
		return fmt.Errorf("failed to scan source: %w", err)
	}

	// Scan target directory
	targetManifest, err := e.scanner.ScanLocal(e.config.TargetDir)
	if err != nil {
		// If target doesn't exist, create it
		targetManifest = NewManifest(e.config.TargetDir)
	}

	// Compare manifests to create sync plan
	plan := CompareManifests(sourceManifest, targetManifest)

	// Execute sync plan
	if err := e.executePlan(plan); err != nil {
		return fmt.Errorf("failed to execute sync plan: %w", err)
	}

	e.lastSyncTime = time.Now()
	elapsed := time.Since(startTime)
	log.Printf("Sync completed in %v", elapsed)
	return nil
}

// executePlan executes the sync plan
func (e *Engine) executePlan(plan *SyncPlan) error {
	timestamp := time.Now().Format("2006/01/02 15:04:05")

	// Create directories first
	for _, dirPath := range plan.DirsToCreate {
		fullPath := filepath.Join(e.config.TargetDir, dirPath)
		if err := e.transferer.CreateDir(fullPath); err != nil {
			if e.config.OnError != nil {
				e.config.OnError(fmt.Sprintf("Failed to create dir %s: %v", dirPath, err))
			}
			continue
		}
		log.Printf("Created directory: %s", dirPath)
	}

	// Copy/update files
	for _, file := range plan.FilesToSync {
		srcPath := filepath.Join(e.config.SourceDir, file.Path)
		dstPath := filepath.Join(e.config.TargetDir, file.Path)

		if err := e.transferer.CopyFile(srcPath, dstPath); err != nil {
			if e.config.OnError != nil {
				e.config.OnError(fmt.Sprintf("Failed to copy %s: %v", file.Path, err))
			}
			continue
		}

		log.Printf("Synced file: %s (%d bytes)", file.Path, file.Size)

		// Report sync event
		if e.config.OnSyncEvent != nil {
			e.config.OnSyncEvent(timestamp, "Added", file.Path, file.Size)
		}
	}

	// Delete files
	for _, filePath := range plan.FilesToDelete {
		fullPath := filepath.Join(e.config.TargetDir, filePath)
		if err := e.transferer.DeleteFile(fullPath); err != nil {
			if e.config.OnError != nil {
				e.config.OnError(fmt.Sprintf("Failed to delete file %s: %v", filePath, err))
			}
			continue
		}
		log.Printf("Deleted file: %s", filePath)

		// Report sync event
		if e.config.OnSyncEvent != nil {
			e.config.OnSyncEvent(timestamp, "Deleted", filePath, 0)
		}
	}

	// Delete directories (in reverse order to handle nested dirs)
	for i := len(plan.DirsToDelete) - 1; i >= 0; i-- {
		dirPath := plan.DirsToDelete[i]
		fullPath := filepath.Join(e.config.TargetDir, dirPath)
		if err := e.transferer.DeleteDir(fullPath); err != nil {
			if e.config.OnError != nil {
				e.config.OnError(fmt.Sprintf("Failed to delete dir %s: %v", dirPath, err))
			}
			continue
		}
		log.Printf("Deleted directory: %s", dirPath)
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
				if err := e.RunSync(); err != nil {
					if e.config.OnError != nil {
						e.config.OnError(fmt.Sprintf("Sync error: %v", err))
					}
				}
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
			if err := e.RunSync(); err != nil {
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
		if err := e.RunSync(); err != nil {
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
