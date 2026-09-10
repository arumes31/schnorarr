package storage

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestShareIdentityAndRecovery(t *testing.T) {
	path := t.TempDir()
	checker, err := New([]Share{{Path: path, ID: "nas-movies"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(path, MarkerName)
	for _, tc := range []struct {
		name, content  string
		present, ready bool
	}{
		{"unmounted empty directory", "", false, false},
		{"wrong share", "other-nas", true, false},
		{"mounted share", "nas-movies\n", true, true},
		{"disconnect after success", "", false, false},
		{"reconnect", "nas-movies\n", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.present {
				if err := os.WriteFile(marker, []byte(tc.content), 0600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Remove(marker); err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			err := checker.Check(context.Background())
			if (err == nil) != tc.ready {
				t.Fatalf("ready=%v, error=%v", tc.ready, err)
			}
			if err != nil && !errors.Is(err, ErrUnavailable) {
				t.Fatalf("unexpected error: %v", err)
			}
			entries, err := os.ReadDir(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if entry.Name() != MarkerName {
					t.Errorf("probe leaked: %s", entry.Name())
				}
			}
			if !tc.present {
				if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("check created its own marker")
				}
			}
		})
	}
}

func TestEveryConfiguredShareIsRequired(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(a, MarkerName), []byte("a"), 0600); err != nil {
		t.Fatal(err)
	}
	checker, err := New([]Share{{Path: a, ID: "a"}, {Path: b, ID: "b"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := checker.Check(context.Background()); err == nil {
		t.Fatal("missing second share accepted")
	}
}

func TestReadinessHTTPRequiresExplicitSuccess(t *testing.T) {
	for _, tc := range []struct {
		name  string
		code  int
		body  string
		ready bool
	}{
		{"ready", 200, `{"status":"ready"}`, true},
		{"unavailable", 503, `{"status":"ready"}`, false},
		{"liveness is insufficient", 200, `{"status":"healthy"}`, false},
		{"missing endpoint", 404, `{}`, false},
		{"malformed", 200, `not json`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.code)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			err := CheckRemote(context.Background(), server.URL)
			if (err == nil) != tc.ready {
				t.Fatalf("ready=%v, error=%v", tc.ready, err)
			}
		})
	}
}

func TestReadinessEndpointReflectsDisconnect(t *testing.T) {
	path := t.TempDir()
	marker := filepath.Join(path, MarkerName)
	if err := os.WriteFile(marker, []byte("receiver"), 0600); err != nil {
		t.Fatal(err)
	}
	checker, err := New([]Share{{Path: path, ID: "receiver"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(checker)
	defer server.Close()
	if err := CheckRemote(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if err := CheckRemote(context.Background(), server.URL); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("disconnect: %v", err)
	}
}

func TestProbeWaitHonorsDeadline(t *testing.T) {
	checker, err := New([]Share{{Path: t.TempDir(), ID: "test"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	checker.slot <- struct{}{} // Simulate a probe still blocked in filesystem I/O.
	defer func() { <-checker.slot }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := checker.Check(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline: %v", err)
	}
}

func TestConfigurationFailsClosed(t *testing.T) {
	for _, shares := range [][]Share{nil, {{Path: "relative", ID: "a"}}, {{Path: t.TempDir()}}} {
		if _, err := New(shares, false); err == nil {
			t.Fatalf("invalid configuration accepted: %+v", shares)
		}
	}
}
