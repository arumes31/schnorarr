package storage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckRemoteDoesNotForwardTokenOnRedirect(t *testing.T) {
	var leaked bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Schnorarr-Shared-Token") != "" {
			leaked = true
		}
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	}))
	defer other.Close()

	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/api/storage-ready", http.StatusFound)
	}))
	defer receiver.Close()

	if err := CheckRemote(context.Background(), receiver.URL+"/api/storage-ready", "token"); err == nil {
		t.Error("a redirected readiness response must not count as ready")
	}
	if leaked {
		t.Error("shared token was forwarded to the redirect target")
	}
}
