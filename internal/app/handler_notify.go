package app

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// NotifyHandler is called by the sender when a file operation is completed
// to invalidate/update the receiver's manifest cache.
func (a *App) NotifyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	queryPath := r.URL.Query().Get("path")
	if queryPath == "" {
		http.Error(w, "path parameter required", http.StatusBadRequest)
		return
	}

	rootDir := os.Getenv("SOURCE_DIR")
	if rootDir == "" {
		rootDir = "/data"
	}

	// Sanitize the path
	cleanPath := filepath.Clean(queryPath)
	if strings.Contains(cleanPath, "..") {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	fullPath := filepath.Join(rootDir, cleanPath)

	// Invalidate the cache for this directory and its parents
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()

	// Heuristic: The path might be a module path. 
	// We should probably just clear all caches if we want to be safe, 
	// but let's try to be specific.
	
	// Clear all caches for now as a single file change affects the whole manifest 
	// of the module it belongs to, and we don't know which cache entry exactly 
	// corresponds to the module root.
	
	log.Printf("[NotifyHandler] Invalidation request for %s. Clearing manifest cache.", fullPath)
	a.manifestCache = make(map[string]manifestCacheEntry)

	w.WriteHeader(http.StatusOK)
}
