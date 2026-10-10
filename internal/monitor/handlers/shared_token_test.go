package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"schnorarr/internal/sync"
)

func TestEngineSharedToken(t *testing.T) {
	previous := AuthEnabled
	t.Cleanup(func() { AuthEnabled = previous })
	AuthEnabled = false
	token := strings.Repeat("a", 64)
	e := sync.NewEngine(sync.SyncConfig{ID: "1", SourceDir: "/data/movies", TargetDir: "host::video-sync/movies", SharedToken: token})
	h := &Handlers{engineProvider: func() []*sync.Engine { return []*sync.Engine{e} }}
	response := httptest.NewRecorder()
	h.EngineSharedToken(response, httptest.NewRequest(http.MethodGet, "/api/engine/1/shared-token", nil))
	if response.Code != http.StatusOK {
		t.Fatal(response.Code)
	}
	var data map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if data["token"] != token || data["filename"] != ".schnorarr-shared-token" {
		t.Fatalf("unexpected popup data: %v", data)
	}
	AuthEnabled = true
	response = httptest.NewRecorder()
	h.EngineSharedToken(response, httptest.NewRequest(http.MethodGet, "/api/engine/1/shared-token", nil))
	if response.Code != http.StatusSeeOther || strings.Contains(response.Body.String(), token) {
		t.Fatal("token exposed without dashboard authentication")
	}
}
