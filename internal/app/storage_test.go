package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"schnorarr/internal/storage"
	syncpkg "schnorarr/internal/sync"
)

func TestDeleteProtectsShareAndRejectsDisconnect(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SOURCE_DIR", root)
	marker := filepath.Join(root, storage.MarkerName)
	token := strings.Repeat("a", 64)
	if err := os.WriteFile(marker, []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "movie.mkv"), []byte("video"), 0600); err != nil {
		t.Fatal(err)
	}
	receiver := storage.NewReceiver(root, filepath.Join(t.TempDir(), "tokens.json"))
	if err := receiver.Verify(context.Background(), "", token); err != nil {
		t.Fatal(err)
	}
	a := &App{Storage: receiver}
	for _, path := range []string{storage.MarkerName, "."} {
		response := httptest.NewRecorder()
		a.DeleteHandler(response, httptest.NewRequest(http.MethodDelete, "/api/delete?dir=true&path="+url.QueryEscape(path), nil))
		if response.Code != http.StatusForbidden {
			t.Fatalf("delete %s returned %d", path, response.Code)
		}
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	a.DeleteHandler(response, httptest.NewRequest(http.MethodDelete, "/api/delete?path=movie.mkv", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("disconnected delete returned %d", response.Code)
	}
	if _, err := os.Stat(filepath.Join(root, "movie.mkv")); err != nil {
		t.Fatal("file was removed during outage")
	}
}

func TestEngineAuthRequiresSharedKey(t *testing.T) {
	called := false
	handler := engineAuth(func(w http.ResponseWriter, r *http.Request) { called = true })
	request := func(key string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/delete?path=x", nil)
		if key != "" {
			req.Header.Set(syncpkg.EngineAPIHeader, key)
		}
		response := httptest.NewRecorder()
		handler(response, req)
		return response.Code
	}

	t.Setenv("ENGINE_API_KEY", "")
	t.Setenv("RSYNC_PASSWORD", "")
	if code := request("anything"); code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured key returned %d", code)
	}
	t.Setenv("RSYNC_PASSWORD", "rsync-secret")
	if code := request(""); code != http.StatusUnauthorized {
		t.Fatalf("missing key returned %d", code)
	}
	if code := request("wrong"); code != http.StatusUnauthorized {
		t.Fatalf("wrong key returned %d", code)
	}
	if called {
		t.Fatal("handler ran without a valid key")
	}
	if code := request("rsync-secret"); code != http.StatusOK || !called {
		t.Fatalf("RSYNC_PASSWORD fallback returned %d", code)
	}
	called = false
	t.Setenv("ENGINE_API_KEY", "engine-secret")
	if code := request("rsync-secret"); code != http.StatusUnauthorized {
		t.Fatalf("ENGINE_API_KEY should take precedence, got %d", code)
	}
	if code := request("engine-secret"); code != http.StatusOK || !called {
		t.Fatalf("valid key returned %d", code)
	}
}
