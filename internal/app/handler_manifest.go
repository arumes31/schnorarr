package app

import (
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"

	"schnorarr/internal/sync"
)

// ManifestHandler returns a manifest from beneath the opened data root.
func (a *App) ManifestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	relative, err := rootedPath(a.dataRoot, r.URL.Query().Get("path"), true, false)
	if err != nil {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	fullPath := filepath.Join(a.dataRoot.Name(), relative)

	sync.AcquireScanLock()
	manifest, err := sync.NewScanner().ScanLocal(fullPath)
	sync.ReleaseScanLock()
	if err != nil {
		log.Printf("Manifest scan failed: %v", err)
		http.Error(w, "Scan failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(manifest); err != nil {
		log.Printf("Failed to encode manifest: %v", err)
	}
}
