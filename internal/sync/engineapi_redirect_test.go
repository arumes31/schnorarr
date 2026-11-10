package sync

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEngineHTTPClientDoesNotForwardKeyOnRedirect(t *testing.T) {
	t.Setenv("ENGINE_API_KEY", "super-secret-engine-key")

	var leaked bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(EngineAPIHeader) != "" {
			leaked = true
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer other.Close()

	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/steal", http.StatusFound)
	}))
	defer receiver.Close()

	req, err := http.NewRequest(http.MethodGet, receiver.URL+"/api/manifest", nil)
	if err != nil {
		t.Fatal(err)
	}
	setEngineAPIKey(req)
	resp, err := engineHTTPClient.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Errorf("expected the redirect to be returned unfollowed, got %d", resp.StatusCode)
	}
	if leaked {
		t.Error("engine API key was forwarded to the redirect target")
	}
}
