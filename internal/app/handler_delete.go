package app

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// DeleteHandler handles requests to delete files or directories
func (a *App) DeleteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" && r.Method != "DELETE" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	queryPath := r.URL.Query().Get("path")
	if queryPath == "" {
		http.Error(w, "Missing path parameter", http.StatusBadRequest)
		return
	}

	isDir := r.URL.Query().Get("dir") == "true"

	rootDir := os.Getenv("SOURCE_DIR")
	if rootDir == "" {
		rootDir = "/data"
	}

	// Sanitize path to prevent traversal
	cleanPath := filepath.Clean(queryPath)
	if strings.Contains(cleanPath, "..") {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	fullPath := filepath.Join(rootDir, cleanPath)

	// Heuristic for module mapping (same as ManifestHandler)
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		parts := strings.SplitN(cleanPath, "/", 2)
		if len(parts) > 1 {
			fullPath = filepath.Join(rootDir, parts[1])
		} else {
			fullPath = rootDir
		}
	}

	log.Printf("[DeleteHandler] Request to delete %s (isDir=%v) resolved to %s", queryPath, isDir, fullPath)

	var err error
	if isDir {
		err = os.RemoveAll(fullPath)
	} else {
		err = os.Remove(fullPath)
	}

	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("[DeleteHandler] Path does not exist: %s", fullPath)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		log.Printf("[DeleteHandler] Delete failed for %s: %v", fullPath, err)
		http.Error(w, "Delete failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("[DeleteHandler] Successfully deleted %s", fullPath)
	w.WriteHeader(http.StatusOK)
}
