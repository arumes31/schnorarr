package sync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"schnorarr/internal/storage"
	"schnorarr/internal/sync/pool"
)

func TestDestinationScanFailureAborts(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "movie.mkv"), []byte("video"), 0600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "unmounted")
	e := NewEngine(SyncConfig{ID: "scan-failure", SourceDir: source, TargetDir: target})
	if _, err := e.PreviewSync(); err == nil {
		t.Error("preview treated failed destination scan as empty")
	}
	if err := e.RunSync(nil); err == nil {
		t.Error("sync treated failed destination scan as empty")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("sync created the missing destination")
	}
}

func TestCopyChecksStorageForEachFile(t *testing.T) {
	source, target := t.TempDir(), t.TempDir()
	unavailable := errors.New("share disconnected")
	ready := true
	checks := 0
	tr := NewTransferer(TransferOptions{CheckStorage: func() error {
		if len(pool.GlobalTransferPool) == 0 {
			t.Error("storage check ran before acquiring a transfer slot")
		}
		checks++
		if !ready {
			return unavailable
		}
		return nil
	}})
	for _, name := range []string{"first.mkv", "second.mkv"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte("video"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := tr.CopyFile(filepath.Join(source, "first.mkv"), filepath.Join(target, "first.mkv")); err != nil {
		t.Fatal(err)
	}
	ready = false
	if err := tr.CopyFile(filepath.Join(source, "second.mkv"), filepath.Join(target, "second.mkv")); !errors.Is(err, unavailable) {
		t.Fatalf("copy after disconnect: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "second.mkv")); !os.IsNotExist(err) {
		t.Fatal("second copy started while storage was unavailable")
	}
	if checks < 2 {
		t.Fatalf("only %d storage checks for two copies", checks)
	}
	ready = true
	if err := tr.CopyFile(filepath.Join(source, "second.mkv"), filepath.Join(target, "second.mkv")); err != nil {
		t.Fatalf("copy did not recover: %v", err)
	}
}

func TestCopyRemovesTemporaryFileWhenRetryStorageCheckFails(t *testing.T) {
	// Reading a directory fails after CopyFile has created the temporary file.
	source := t.TempDir()
	target := filepath.Join(t.TempDir(), "movie.mkv")
	unavailable := errors.New("share disconnected before retry")
	checks := 0
	tr := NewTransferer(TransferOptions{CheckStorage: func() error {
		checks++
		if checks > 1 {
			if _, err := os.Stat(target + ".tmp"); err != nil {
				t.Fatalf("first attempt did not leave a temporary file: %v", err)
			}
			return unavailable
		}
		return nil
	}})
	if err := tr.CopyFile(source, target); err != unavailable {
		t.Fatalf("retry lost the storage error: %v", err)
	}
	if _, err := os.Stat(target + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary file remains after storage failure: %v", err)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed copy created the destination: %v", err)
	}
}

func TestEngineStopsPlanWhenShareDisconnectsBetweenFiles(t *testing.T) {
	source, target := t.TempDir(), t.TempDir()
	marker := filepath.Join(source, storage.MarkerName)
	if err := os.WriteFile(marker, []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	checker, err := storage.New([]storage.Share{{Path: source, ID: "source"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"first.mkv", "second.mkv"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte("video"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	e := NewEngine(SyncConfig{
		ID: "disconnect-between-copies", SourceDir: source, TargetDir: target,
		CheckStorage: func() error { return checker.Check(context.Background()) },
		OnSyncEvent: func(_, action, path string, _ int64) {
			if action == "Added" && path == "first.mkv" {
				if err := os.Remove(marker); err != nil {
					t.Error(err)
				}
			}
		},
	})
	plan := &SyncPlan{FilesToSync: []*FileInfo{{Path: "first.mkv", Size: 5}, {Path: "second.mkv", Size: 5}}}
	_, err = e.executeSyncPhase(plan, NewManifest(target))
	if !errors.Is(err, storage.ErrUnavailable) {
		t.Fatalf("expected stopped plan, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "first.mkv")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "second.mkv")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("second file was copied")
	}
	if !e.storageBlocked {
		t.Fatal("blocked work will not be retried")
	}
	if len(e.failedFiles) != 0 {
		t.Fatal("storage outage incorrectly entered one-hour file failure backoff")
	}
	if err := os.WriteFile(marker, []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	// A queued empty snapshot from the outage must not replace a fresh scan.
	if err := e.RunSync(NewManifest(source)); err != nil {
		t.Fatalf("recovery failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "second.mkv")); err != nil {
		t.Fatalf("pending copy was not recovered: %v", err)
	}
}

func TestIdleSyncDoesNotProbeStorage(t *testing.T) {
	checks := 0
	e := NewEngine(SyncConfig{
		ID: "idle-storage", SourceDir: t.TempDir(), TargetDir: t.TempDir(),
		CheckStorage: func() error { checks++; return nil },
	})
	for i := 0; i < 3; i++ {
		if err := e.RunSync(nil); err != nil {
			t.Fatal(err)
		}
	}
	if checks != 1 {
		t.Fatalf("expected startup check only, got %d", checks)
	}
	if err := os.WriteFile(filepath.Join(e.config.SourceDir, "new.mkv"), []byte("video"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := e.RunSync(nil); err != nil {
		t.Fatal(err)
	}
	if checks != 2 {
		t.Fatalf("expected a fresh check for the new copy, got %d", checks)
	}
}

func TestScannerNeverIncludesStorageMarkers(t *testing.T) {
	path := t.TempDir()
	for _, name := range []string{storage.MarkerName, storage.ProbePrefix + "123", "movie.mkv"} {
		if err := os.WriteFile(filepath.Join(path, name), []byte("data"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	scanner := NewScanner()
	scanner.ExcludePatterns = nil
	manifest, err := scanner.ScanLocal(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) != 1 || !manifest.HasFile("movie.mkv") {
		t.Fatalf("reserved storage files entered manifest: %+v", manifest.Files)
	}
}
