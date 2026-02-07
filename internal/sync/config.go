package sync

import (
	"time"
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
	// AutoApproveDeletions when true, deletions are executed without waiting for manual approval
	AutoApproveDeletions bool
	// OnSyncEvent callback for sync events (timestamp, action, path, size)
	OnSyncEvent func(timestamp, action, path string, size int64)
	// OnError callback for errors
	OnError func(msg string)
}

// GetConfig returns the engine configuration
func (e *Engine) GetConfig() SyncConfig {
	return e.config
}
