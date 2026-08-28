package app

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
)

type StatResponse struct {
	Size   int64 `json:"size"`
	Exists bool  `json:"exists"`
}

// StatHandler returns file metadata from beneath the opened data root.
func (a *App) StatHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	relative, err := rootedPath(a.dataRoot, r.URL.Query().Get("path"), true, true)
	if err != nil {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	info, err := a.dataRoot.Stat(relative)
	response := StatResponse{}
	if err != nil {
		if !os.IsNotExist(err) {
			// #nosec G706 -- %q escapes control characters in both request-derived values.
			log.Printf("[StatHandler] Error stating relative path %q: %q", relative, err.Error())
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
