package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"schnorarr/internal/monitor/config"
	"schnorarr/internal/monitor/database"
	"schnorarr/internal/monitor/health"
	"schnorarr/internal/sync"
)

func TestIndexRendersStorageWaitAndPolicy(t *testing.T) {
	previous := AuthEnabled
	AuthEnabled = false
	t.Cleanup(func() { AuthEnabled = previous })
	previousDB, previousPath := database.DB, database.DBPath
	database.DBPath = filepath.Join(t.TempDir(), "dashboard.db")
	t.Cleanup(func() {
		if database.DB != nil {
			_ = database.DB.Close()
		}
		database.DB, database.DBPath = previousDB, previousPath
	})
	if err := database.Init(); err != nil {
		t.Fatal(err)
	}
	e := sync.NewEngine(sync.SyncConfig{
		ID: "1", SourceDir: "/source", TargetDir: "/destination",
		CheckStorage: func() error { return errors.New("Destination storage: <missing token>") },
	})
	if err := e.RunSync(nil); err == nil {
		t.Fatal("expected storage check to block the fixture")
	}
	h := &Handlers{config: &config.Config{}, healthState: health.New(), engineProvider: func() []*sync.Engine { return []*sync.Engine{e} }}
	w := httptest.NewRecorder()
	h.Index(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("dashboard failed: %d %s", w.Code, w.Body.String())
	}
	for _, expected := range []string{"Waiting for storage", "Destination storage: &lt;missing token&gt;", "Check shared token", "Dry run all", "Sync help", "shared-token-modal"} {
		if !strings.Contains(w.Body.String(), expected) {
			t.Errorf("dashboard missing %q", expected)
		}
	}
}
