package storage

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReceiverFolderTokens(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "movies")
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	r := NewReceiver(root, filepath.Join(t.TempDir(), "tokens.json"))
	token := strings.Repeat("a", 64)
	marker := filepath.Join(folder, MarkerName)
	ctx := context.Background()
	if err := r.CheckTarget(ctx, "movies/new.mkv", false); err == nil {
		t.Fatal("unregistered transfer accepted")
	}
	if err := r.Verify(ctx, "movies", token); err == nil {
		t.Fatal("missing token accepted")
	}
	if err := os.WriteFile(marker, []byte(token+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := r.Verify(ctx, "movies", strings.Repeat("b", 64)); err == nil {
		t.Fatal("wrong engine token accepted")
	}
	if err := r.Verify(ctx, "movies", token); err != nil {
		t.Fatal(err)
	}
	// A fresh process must retain expected identity after the share changes.
	r = NewReceiver(root, r.Registry)
	if err := r.CheckTarget(ctx, "movies/nested/new.mkv", false); err != nil {
		t.Fatal(err)
	}
	if err := r.CheckTarget(ctx, "other/new.mkv", false); err == nil {
		t.Fatal("unregistered sibling accepted")
	}
	if err := r.CheckTarget(ctx, "movies", true); !errors.Is(err, ErrProtected) {
		t.Fatalf("root deletion: %v", err)
	}
	if err := os.WriteFile(marker, []byte(strings.Repeat("b", 64)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := r.CheckTarget(ctx, "movies/new.mkv", false); err == nil {
		t.Fatal("changed token accepted by transfer hook")
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if err := r.CheckTarget(ctx, "movies/new.mkv", false); err == nil {
		t.Fatal("disconnected folder accepted")
	}
	if err := os.WriteFile(marker, []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	if err := r.CheckTarget(ctx, "movies/new.mkv", false); err != nil {
		t.Fatal(err)
	}
}

func TestReceiverTokenEndpoint(t *testing.T) {
	root := t.TempDir()
	folder := "TV shows"
	if err := os.Mkdir(filepath.Join(root, folder), 0700); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("c", 64)
	if err := os.WriteFile(filepath.Join(root, folder, MarkerName), []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	r := NewReceiver(root, filepath.Join(t.TempDir(), "tokens.json"))
	server := httptest.NewServer(r)
	defer server.Close()
	endpoint := server.URL + "?path=" + url.QueryEscape(folder)
	if err := CheckRemote(context.Background(), endpoint); err == nil {
		t.Fatal("request without token accepted")
	}
	if err := CheckRemote(context.Background(), endpoint, token); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../outside", "movies/../../outside", "/outside", `..\outside`} {
		if _, err := r.Resolve(path); err == nil {
			t.Fatalf("unsafe path accepted: %s", path)
		}
	}
}

func TestReceiverTokenEndpointHidesVerificationDetails(t *testing.T) {
	r := NewReceiver(t.TempDir(), filepath.Join(t.TempDir(), "tokens.json"))
	req := httptest.NewRequest(http.MethodGet, "/?path=private-folder", nil)
	req.Header.Set("X-Schnorarr-Shared-Token", strings.Repeat("a", 64))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", w.Code)
	}
	var result map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["status"] != "unavailable" || result["message"] != "storage unavailable" {
		t.Fatalf("unexpected readiness response: %s", w.Body.String())
	}
}

func TestLegacyMarkerDoesNotSatisfyTokenCheck(t *testing.T) {
	root := t.TempDir()
	token := strings.Repeat("d", 64)
	if err := os.WriteFile(filepath.Join(root, ".schnorarr-share-id"), []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	checker, err := New([]Share{{Path: root, ID: token}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := checker.Check(context.Background()); err == nil {
		t.Fatal("legacy marker still accepted")
	}
}

func TestReceiverRejectsSymlinkOutsideModule(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	token := strings.Repeat("e", 64)
	if err := os.WriteFile(filepath.Join(outside, MarkerName), []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	r := NewReceiver(root, filepath.Join(t.TempDir(), "tokens.json"))
	if err := r.Verify(context.Background(), "escape", token); err == nil {
		t.Fatal("receiver accepted a folder outside its module")
	}
}
