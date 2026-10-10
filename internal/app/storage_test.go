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
