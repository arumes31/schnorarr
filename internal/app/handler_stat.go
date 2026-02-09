package app

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// StatResponse contains file size information
type StatResponse struct {
	Size   int64 `json:"size"`
	Exists bool  `json:"exists"`
}

// StatHandler returns the size of a file on the receiver
func (a *App) StatHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	queryPath := r.URL.Query().Get("path")
	if queryPath == "" {
		http.Error(w, "path parameter required", http.StatusBadRequest)
		return
	}

	// Sanitize the path
	cleanPath := filepath.Clean(queryPath)
	if strings.HasPrefix(cleanPath, "..") {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	// Get the root directory from environment
	rootDir := os.Getenv("RSYNC_MODULE_PATH")
	if rootDir == "" {
		rootDir = "/data"
	}

	fullPath := filepath.Join(rootDir, cleanPath)

	// Get file info
	info, err := os.Stat(fullPath)
	response := StatResponse{}

	if err != nil {
		if os.IsNotExist(err) {
			response.Exists = false
			response.Size = 0
		} else {
			log.Printf("[StatHandler] Error stating file %s: %v", fullPath, err)
			http.Error(w, "failed to stat file", http.StatusInternalServerError)
			return
		}
	} else {
		response.Exists = true
		response.Size = info.Size()
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("[StatHandler] Error encoding response: %v", err)
	}
}
