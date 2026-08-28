package internalapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestRequireToken(t *testing.T) {
	token := strings.Repeat("a", 32)
	handler := RequireToken(token, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, tt := range []struct {
		name   string
		header string
		status int
	}{
		{name: "anonymous", status: http.StatusUnauthorized},
		{name: "wrong", header: "Bearer " + strings.Repeat("b", 32), status: http.StatusUnauthorized},
		{name: "valid", header: "Bearer " + token, status: http.StatusNoContent},
	} {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/manifest", nil)
			request.Header.Set("Authorization", tt.header)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tt.status {
				t.Fatalf("status = %d, want %d", response.Code, tt.status)
			}
		})
	}
}

func TestNewRequestRequiresHTTPSAndUsesHeader(t *testing.T) {
	token := strings.Repeat("c", 32)
	getenv := env(map[string]string{
		"INTERNAL_API_TOKEN": token,
		"RECEIVER_API_URL":   "https://receiver.example:8443",
	})
	request, err := NewRequest(context.Background(), getenv, http.MethodGet, "/api/stat", url.Values{"path": {"movie.mkv"}})
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	if got := request.Header.Get("Authorization"); got != "Bearer "+token {
		t.Fatalf("Authorization = %q", got)
	}
	if strings.Contains(request.URL.RawQuery, token) {
		t.Fatal("token leaked into URL")
	}

	_, err = ReceiverURL(env(map[string]string{"RECEIVER_API_URL": "http://receiver:8080"}), "/api/stat", nil)
	if err == nil {
		t.Fatal("ReceiverURL() accepted plaintext HTTP")
	}
}
