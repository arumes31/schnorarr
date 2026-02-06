package sync

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEngine_SafetyLock(t *testing.T) {
	// Setup temp directories
	sourceDir := t.TempDir()
	targetDir := t.TempDir()

	// Create a file in target that should be deleted (not in source)
	deletePath := filepath.Join(targetDir, "to_delete.txt")
	if err := os.WriteFile(deletePath, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Configure engine
	cfg := SyncConfig{
		ID:           "test-safety",
		SourceDir:    sourceDir,
		TargetDir:    targetDir,
		Rule:         "flat",
		PollInterval: 100 * time.Millisecond,
		DryRun:       false,
	}

	engine := NewEngine(cfg)

	// manually waiting for start might be tricky as Start() blocks or runs loops.
	// We can call RunSync directly.

	// 1. Trigger Sync
	// Since we are calling RunSync directly, we can check the result.
	err := engine.RunSync(nil)
	if err != nil {
		t.Fatalf("RunSync failed: %v", err)
	}

	// 2. Verify Safety Lock
	if !engine.IsWaitingForApproval() {
		t.Fatal("Engine should be waiting for approval due to pending deletions")
	}

	pending := engine.GetPendingDeletions()
	if len(pending) != 1 || pending[0] != "to_delete.txt" {
		t.Errorf("Expected 1 pending deletion (to_delete.txt), got: %v", pending)
	}

	// Verify file still exists
	if _, err := os.Stat(deletePath); os.IsNotExist(err) {
		t.Fatal("File was deleted before approval!")
	}

	// 3. Approve Deletions
	// We call ApproveDeletions which triggers a goroutine.
	// But for deterministic testing, we can just set the flag and run Sync again synchronously ?
	// engine.ApproveDeletions() runs `go e.RunSync(nil)`.
	// Let's manually approve to test the logic, then run Sync manually.

	engine.pausedMu.Lock()
	engine.deletionAllowed = true
	engine.waitingForApproval = false
	engine.pausedMu.Unlock()

	err = engine.RunSync(nil)
	if err != nil {
		t.Fatalf("RunSync failed after approval: %v", err)
	}

	// 4. Verify Deletion
	if engine.IsWaitingForApproval() {
		t.Fatal("Engine should NOT be waiting for approval after sync")
	}

	if _, err := os.Stat(deletePath); !os.IsNotExist(err) {
		t.Fatal("File should have been deleted after approval")
	}
}
