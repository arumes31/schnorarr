package app

import (
	"errors"
	"log"
	"net/http"
	"os"
	"schnorarr/internal/storage"
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

	if a.Storage == nil {
		http.Error(w, "storage readiness is not configured", http.StatusServiceUnavailable)
		return
	}
	if storage.Reserved(queryPath) {
		http.Error(w, "storage markers and share roots are protected", http.StatusForbidden)
		return
	}
	fullPath, pathErr := a.Storage.Resolve(queryPath)
	if pathErr != nil {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	if err := a.Storage.CheckTarget(r.Context(), queryPath, true); err != nil {
		status := http.StatusServiceUnavailable
		if errors.Is(err, storage.ErrProtected) {
			status = http.StatusForbidden
		}
		http.Error(w, err.Error(), status)
		return
	}

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
