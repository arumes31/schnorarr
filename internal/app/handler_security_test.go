package app

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"schnorarr/internal/internalapi"
)

func testRootedApp(t *testing.T) (*App, string) {
	t.Helper()
	directory := t.TempDir()
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return &App{dataRoot: root}, directory
}

func TestMachineEndpointsRequireAuthentication(t *testing.T) {
	application, directory := testRootedApp(t)
	if err := os.WriteFile(filepath.Join(directory, "movie.mkv"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(directory, "season"), 0o700); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("a", 32)
	endpoints := []struct {
		name    string
		method  string
		path    string
		handler http.HandlerFunc
	}{
		{name: "manifest", method: http.MethodGet, path: "/api/manifest?path=.", handler: application.ManifestHandler},
		{name: "stat", method: http.MethodGet, path: "/api/stat?path=movie.mkv", handler: application.StatHandler},
		{name: "recursive delete", method: http.MethodDelete, path: "/api/delete?path=season&dir=true", handler: application.DeleteHandler},
	}
	for _, endpoint := range endpoints {
		t.Run(endpoint.name, func(t *testing.T) {
			request := httptest.NewRequest(endpoint.method, endpoint.path, nil)
			response := httptest.NewRecorder()
			internalapi.RequireToken(token, endpoint.handler).ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("anonymous status = %d, want 401", response.Code)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(directory, "season")); err != nil {
		t.Fatalf("anonymous recursive delete changed data: %v", err)
	}
}

func TestMachineEndpointsRejectTraversalAndSymlinkDelete(t *testing.T) {
	application, directory := testRootedApp(t)
	token := strings.Repeat("b", 32)
	outside := filepath.Join(t.TempDir(), "important.txt")
	if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "escape")
	if err := os.Symlink(filepath.Dir(outside), link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	for _, path := range []string{"../important.txt", "/etc/passwd", "escape/important.txt", "."} {
		request := httptest.NewRequest(http.MethodDelete, "/api/delete?path="+url.QueryEscape(path)+"&dir=true", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		internalapi.RequireToken(token, http.HandlerFunc(application.DeleteHandler)).ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("delete path %q status = %d, want 400", path, response.Code)
		}
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != "keep" {
		t.Fatalf("outside file changed: content=%q error=%v", got, err)
	}
}
