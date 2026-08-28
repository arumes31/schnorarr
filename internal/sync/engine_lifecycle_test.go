package sync

import (
	"path/filepath"
	stdsync "sync"
	"testing"
	"time"
)

func TestEngineStopIsConcurrentAndJoined(t *testing.T) {
	engine := NewEngine(SyncConfig{
		ID:        "lifecycle",
		SourceDir: t.TempDir(),
		TargetDir: t.TempDir(),
		DryRun:    true,
	})
	if err := engine.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	var group stdsync.WaitGroup
	for range 10 {
		group.Add(1)
		go func() {
			defer group.Done()
			engine.Stop()
		}()
	}
	done := make(chan struct{})
	go func() {
		group.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent Stop calls did not join owned workers")
	}
	if err := engine.Start(); err == nil {
		t.Fatal("Start() succeeded after Stop()")
	}
}

func TestEngineStartCleansWatcherOnFailure(t *testing.T) {
	engine := NewEngine(SyncConfig{
		ID:        "startup-failure",
		SourceDir: filepath.Join(t.TempDir(), "missing"),
		TargetDir: t.TempDir(),
	})
	if err := engine.Start(); err == nil {
		t.Fatal("Start() succeeded with a missing source")
	}
	if engine.watcher != nil {
		t.Fatal("Start() retained a watcher after partial failure")
	}
	engine.Stop()
	engine.Stop()
}
