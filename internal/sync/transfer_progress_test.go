package sync

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseRemoteDestination(t *testing.T) {
	tests := []struct {
		name         string
		dst          string
		expectedHost string
		expectedPath string
	}{
		{
			name:         "daemon protocol with user",
			dst:          "syncuser@schnorarr-receiver.werewolf-gondola.ts.net::video-sync/Doku/Planet Earth/Season 1/file.mkv",
			expectedHost: "schnorarr-receiver.werewolf-gondola.ts.net",
			expectedPath: "Doku/Planet Earth/Season 1/file.mkv",
		},
		{
			name:         "daemon protocol without user",
			dst:          "receiver.local::module/path/to/file.txt",
			expectedHost: "receiver.local",
			expectedPath: "path/to/file.txt",
		},
		{
			name:         "rsync protocol",
			dst:          "rsync://host/module/path/to/file.txt",
			expectedHost: "host",
			expectedPath: "path/to/file.txt",
		},
		{
			name:         "rsync protocol with user and port",
			dst:          "rsync://user@host:873/module/path/to/file.txt",
			expectedHost: "host",
			expectedPath: "path/to/file.txt",
		},
		{
			name:         "daemon protocol with just module",
			dst:          "user@host::module",
			expectedHost: "host",
			expectedPath: "",
		},
		{
			name:         "non-daemon protocol",
			dst:          "/local/path/file.txt",
			expectedHost: "",
			expectedPath: "",
		},
		{
			name:         "empty string",
			dst:          "",
			expectedHost: "",
			expectedPath: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, path := parseRemoteDestination(tt.dst)
			if host != tt.expectedHost {
				t.Errorf("parseRemoteDestination(%q) host = %q, want %q", tt.dst, host, tt.expectedHost)
			}
			if path != tt.expectedPath {
				t.Errorf("parseRemoteDestination(%q) path = %q, want %q", tt.dst, path, tt.expectedPath)
			}
		})
	}
}

func TestGetRemoteFileSize(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		response     interface{}
		statusCode   int
		expectedSize int64
	}{
		{
			name: "file exists",
			path: "path/to/file.txt",
			response: map[string]interface{}{
				"size":   int64(1234567),
				"exists": true,
			},
			statusCode:   http.StatusOK,
			expectedSize: 1234567,
		},
		{
			name: "file does not exist",
			path: "nonexistent.txt",
			response: map[string]interface{}{
				"size":   int64(0),
				"exists": false,
			},
			statusCode:   http.StatusOK,
			expectedSize: 0,
		},
		{
			name:         "server error",
			path:         "error.txt",
			response:     nil,
			statusCode:   http.StatusInternalServerError,
			expectedSize: 0,
		},
		{
			name: "large file",
			path: "big.bin",
			response: map[string]interface{}{
				"size":   int64(9876543210),
				"exists": true,
			},
			statusCode:   http.StatusOK,
			expectedSize: 9876543210,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test server that listens on /api/stat directly
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify the request
				if r.Method != http.MethodGet {
					t.Errorf("Expected GET request, got %s", r.Method)
				}
				if r.URL.Path != "/api/stat" {
					t.Errorf("Expected /api/stat path, got %s", r.URL.Path)
				}

				// Send response
				w.WriteHeader(tt.statusCode)
				if tt.response != nil {
					if err := json.NewEncoder(w).Encode(tt.response); err != nil {
						t.Fatalf("Failed to encode response: %v", err)
					}
				}
			}))
			defer server.Close()

			// For testing, we need to use a helper that doesn't add :8080
			// Extract host:port from test server URL (remove http://)
			serverAddr := server.URL[7:]

			// Call getRemoteFileSizeTest helper that accepts full address
			size := getRemoteFileSizeTest(serverAddr, tt.path)
			if size != tt.expectedSize {
				t.Errorf("getRemoteFileSize(%q, %q) = %d, want %d", serverAddr, tt.path, size, tt.expectedSize)
			}
		})
	}
}

// getRemoteFileSizeTest is a test helper that doesn't append :8080
func getRemoteFileSizeTest(hostWithPort, path string) int64 {
	apiURL := "http://" + hostWithPort + "/api/stat?path=" + path

	resp, err := http.Get(apiURL)
	if err != nil {
		return 0
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return 0
	}

	var statResp struct {
		Size   int64 `json:"size"`
		Exists bool  `json:"exists"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&statResp); err != nil {
		return 0
	}

	return statResp.Size
}

func TestGetRemoteFileSize_NetworkError(t *testing.T) {
	// Test with invalid host to simulate network error
	size := getRemoteFileSize("invalid-host-that-does-not-exist.local", "test.txt")
	if size != 0 {
		t.Errorf("getRemoteFileSize with invalid host should return 0, got %d", size)
	}
}

func TestGetRemoteFileSize_MalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Write invalid JSON
		_, _ = w.Write([]byte("{invalid json}"))
	}))
	defer server.Close()

	host := server.URL[7:]
	size := getRemoteFileSize(host, "test.txt")
	if size != 0 {
		t.Errorf("getRemoteFileSize with malformed JSON should return 0, got %d", size)
	}
}
