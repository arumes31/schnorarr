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
			// Check for rsync partial file (--partial creates .filename.XXXXXX temp files)
			// Look in the same directory for files matching .basename.*
			dir := filepath.Dir(fullPath)
			basename := filepath.Base(fullPath)

			entries, readErr := os.ReadDir(dir)
			if readErr != nil {
				// Only log if it's not a "not exist" error for the directory itself
				if !os.IsNotExist(readErr) {
					log.Printf("[StatHandler] Error reading directory %s: %v", dir, readErr)
				}
			} else {
				// Check for rsync temporary files (.basename.XXXXXX) 
				// or local copy temporary files (basename.tmp)
				rsyncPrefix := "." + basename + "."
				localTmpName := basename + ".tmp"
				
				for _, entry := range entries {
					name := entry.Name()
					if entry.IsDir() {
						continue
					}
					
					isMatch := false
					if strings.HasPrefix(name, rsyncPrefix) {
						isMatch = true
					} else if name == localTmpName {
						isMatch = true
					}

					if isMatch {
						// Found a partial file, get its size
						partialPath := filepath.Join(dir, name)
						if partialInfo, statErr := os.Stat(partialPath); statErr == nil {
							log.Printf("[StatHandler] Found partial file: %s (size: %d)", name, partialInfo.Size())
							response.Exists = true
							response.Size = partialInfo.Size()
							w.Header().Set("Content-Type", "application/json")
							if encodeErr := json.NewEncoder(w).Encode(response); encodeErr != nil {
								log.Printf("[StatHandler] Error encoding response: %v", encodeErr)
							}
							return
						}
					}
				}
			}

			// No file or partial file found
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
