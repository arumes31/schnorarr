package app

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"schnorarr/internal/sync"
)

// ManifestHandler handles requests for the file manifest of a specific path
func (a *App) ManifestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Security check: Only allow if configured as receiver or if we want to allow this?
	// The user requested receiver scans itself.
	// We might want to protect this endpoint? For now open as internal API.

	queryPath := r.URL.Query().Get("path")
	if queryPath == "" {
		http.Error(w, "Missing path parameter", http.StatusBadRequest)
		return
	}

	// Resolve the path based on modules or default data dir
	// Assumption: queryPath is relative to /data or is a module path
	// E.g. path=video-sync/movies
	// We need to resolve this to absolute path on the container
	// For now, let's assume it maps to /data/<path> or handle module logic?
	// The Sender sends "video-sync/movies", which is [Manifest Module]/[Subpath]
	// If RSYNC defaults to /data, then video-sync might map to /data.
	// Let's assume the path passed is the one configured in rsyncd.conf or similar.
	// But the APP doesn't parse rsyncd.conf.
	// Simple fix: Assume /data is the root for everything for now, or use os.Getenv("SOURCE_DIR")

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

	// If the path is absolute, check if it starts with rootDir?
	// If relative, join with rootDir.
	// Let's assume the sender sends the module path which maps to /data
	// E.g. "video-sync" -> /data
	// "video-sync/movies" -> /data/movies
	// This mapping is tricky without knowing the rsyncd config.
	// However, usually we mount /data as the root.

	// If the path contains the module name, stripping it might be necessary if module=root
	// But let's just Try scanning rootDir/cleanPath

	fullPath := filepath.Join(rootDir, cleanPath)

	// Special case: If path is just the module name and module maps to root
	// We might need to handle mappings.
	// For this specific user case: DEST_MODULE=video-sync.
	// If video-sync maps to /data, and sender asks for "video-sync/movies",
	// fullPath = /data/video-sync/movies.
	// BUT if the receiver expects /data/movies...
	// Let's check how Resolving works in Sender.
	// Sender resolves "video-sync/movies" -> "192.168.1.5::video-sync/movies"
	// The receiver rsync daemon handles the mapping.
	// The API doesn't know about rsync modules.

	// Workaround: Allow config to specify module mapping or just use relative path from /data?
	// Let's try to strip the first component?
	// Or better: The sender should send the RELATIVE path it expects.
	// Sender currently uses "video-sync/movies".
	// If we assume the container has /data mapped to video-sync.
	// Then we should strip "video-sync" if it's the prefix?
	// Let's implement a heuristic: if fullPath doesn't exist, try stripping first component.

	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		parts := strings.SplitN(cleanPath, "/", 2)
		if len(parts) > 1 {
			fullPath = filepath.Join(rootDir, parts[1])
		} else {
			// If cleanPath is just "video-sync", maybe it maps to rootDir
			fullPath = rootDir
		}
	}

	// Check cache
	a.cacheMu.Lock()
	entry, exists := a.manifestCache[fullPath]
	if exists && time.Now().Before(entry.expiry) {
		a.cacheMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(entry.manifest); err != nil {
			log.Printf("Failed to encode cached manifest: %v", err)
		}
		return
	}
	a.cacheMu.Unlock()

	// Scan!
	scanner := sync.NewScanner()
	manifest, err := scanner.ScanLocal(fullPath)
	if err != nil {
		http.Error(w, "Scan failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Update cache
	a.cacheMu.Lock()
	a.manifestCache[fullPath] = manifestCacheEntry{
		manifest: manifest,
		expiry:   time.Now().Add(5 * time.Minute),
	}
	a.cacheMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(manifest); err != nil {
		log.Printf("Failed to encode manifest: %v", err)
	}
}
