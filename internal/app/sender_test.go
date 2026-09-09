package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"schnorarr/internal/monitor/health"
)

func TestStartSyncEngines_LoopCapture(t *testing.T) {
	// Create temp directories for sources to ensure engines start successfully
	tmpDir := t.TempDir()
	src1 := filepath.Join(tmpDir, "src1")
	src2 := filepath.Join(tmpDir, "src2")
	if err := os.Mkdir(src1, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(src2, 0755); err != nil {
		t.Fatal(err)
	}

	// Setup Env
	t.Setenv("DEST_HOST", "")
	t.Setenv("SYNC_1_SOURCE", src1)
	t.Setenv("SYNC_1_TARGET", t.TempDir())
	t.Setenv("SYNC_1_RULE", "flat")

	t.Setenv("SYNC_2_SOURCE", src2)
	t.Setenv("SYNC_2_TARGET", t.TempDir())
	t.Setenv("SYNC_2_RULE", "series")

	// Mock health state
	healthState := &health.State{}

	// We pass nil for wsHub, notifier and bwManager as they are only used in callbacks
	engines := startSyncEngines(nil, healthState, nil, nil, func(id string) (string, error) { return strings.Repeat(id, 64), nil })

	// Cleanup engines (stop watchers)
	defer func() {
		for _, e := range engines {
			e.Stop()
		}
	}()

	if len(engines) != 2 {
		t.Errorf("Expected 2 engines, got %d", len(engines))
	}

	// Verify IDs
	ids := make(map[string]bool)
	for _, e := range engines {
		cfg := e.GetConfig()
		ids[cfg.ID] = true
		switch cfg.ID {
		case "1":
			if cfg.SourceDir != src1 {
				t.Errorf("Engine 1 source mismatch: %s", cfg.SourceDir)
			}
		case "2":
			if cfg.SourceDir != src2 {
				t.Errorf("Engine 2 source mismatch: %s", cfg.SourceDir)
			}
		}
	}

	if !ids["1"] {
		t.Error("Engine 1 not found")
	}
	if !ids["2"] {
		t.Error("Engine 2 not found")
	}

	// Give some time for background goroutines to settle if needed, though not strictly required for this test
	time.Sleep(10 * time.Millisecond)
}
