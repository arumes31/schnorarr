package sync

import (
	"sync"
)

var (
	// globalSyncMu coordinates scanning and transferring across all engines.
	// We use an RWMutex:
	// - Multiple engines can scan simultaneously (RLock).
	// - Only one engine can transfer at a time (Lock).
	// - No engine can scan while any engine is transferring.
	// - No engine can transfer while any engine is scanning.
	globalSyncMu sync.RWMutex
)

// AcquireScanLock acquires the global lock for scanning.
// It allows multiple concurrent scans but blocks if a transfer is in progress.
func AcquireScanLock() {
	globalSyncMu.RLock()
}

// ReleaseScanLock releases the global lock for scanning.
func ReleaseScanLock() {
	globalSyncMu.RUnlock()
}

// AcquireTransferLock acquires the global lock for transferring.
// It ensures only one transfer happens at a time and blocks if any scan is in progress.
func AcquireTransferLock() {
	globalSyncMu.Lock()
}

// ReleaseTransferLock releases the global lock for transferring.
func ReleaseTransferLock() {
	globalSyncMu.Unlock()
}
