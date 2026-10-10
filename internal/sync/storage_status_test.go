package sync

import (
	"errors"
	"testing"
	"time"

	"schnorarr/internal/monitor/database"
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

func TestStorageErrorsReportedOnlyWhenChanged(t *testing.T) {
	previousDB, previousPath := database.DB, database.DBPath
	database.DBPath = ":memory:"
	t.Cleanup(func() {
		if database.DB != nil {
			_ = database.DB.Close()
		}
		database.DB, database.DBPath = previousDB, previousPath
	})
	if err := database.Init(); err != nil {
		t.Fatal(err)
	}
	var problem error
	e := &Engine{config: SyncConfig{
		ID:           "storage-error-reporting",
		CheckStorage: func() error { return problem },
	}}
	for _, tc := range []struct {
		name       string
		problem    error
		wantErrors int
	}{
		{"first outage", errors.New("source disconnected"), 1},
		{"same message", errors.New("source disconnected"), 1},
		{"changed message", errors.New("destination disconnected"), 2},
		{"recovery", nil, 2},
		{"later outage", errors.New("destination disconnected"), 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			problem = tc.problem
			if err := e.checkStorage(); !errors.Is(err, problem) {
				t.Fatalf("storage error chain lost: %v", err)
			}
			var count int
			if err := database.DB.QueryRow("SELECT error_count FROM engine_stats WHERE engine_id = ?", e.config.ID).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != tc.wantErrors {
				t.Errorf("reported %d errors, want %d", count, tc.wantErrors)
			}
		})
	}
}
