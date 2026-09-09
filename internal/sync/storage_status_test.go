package sync

import (
	"errors"
	"testing"
	"time"
)

func TestStorageStatusTracksFailureAndRecovery(t *testing.T) {
	problem := errors.New("Destination storage: shared token does not match")
	e := &Engine{config: SyncConfig{CheckStorage: func() error { return problem }}, storageBlocked: true}
	blocked, message, checked := e.GetStorageStatus()
	if !blocked || message != "" || !checked.IsZero() {
		t.Fatalf("unchecked storage should remain unknown: %v, %q, %v", blocked, message, checked)
	}
	before := time.Now()
	if err := e.checkStorage(); !errors.Is(err, problem) {
		t.Fatalf("storage error chain lost: %v", err)
	}
	blocked, message, checked = e.GetStorageStatus()
	if !blocked || message != problem.Error() || checked.Before(before) {
		t.Fatalf("missing failure evidence: %v, %q, %v", blocked, message, checked)
	}
	problem = nil
	if err := e.checkStorage(); err != nil {
		t.Fatal(err)
	}
	blocked, message, recovered := e.GetStorageStatus()
	if blocked || message != "" || recovered.Before(checked) {
		t.Fatalf("stale failure after recovery: %v, %q, %v", blocked, message, recovered)
	}
}
