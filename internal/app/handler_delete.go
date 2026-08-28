package app

import (
	"log"
	"net/http"
	"os"
)

// DeleteHandler removes a file or directory beneath the opened data root.
func (a *App) DeleteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	relative, err := rootedPath(a.dataRoot, r.URL.Query().Get("path"), false, true)
	if err != nil {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	isDir := r.URL.Query().Get("dir") == "true"
	// #nosec G706 -- %q escapes control characters in the authenticated, root-confined path.
	log.Printf("[DeleteHandler] Request to delete relative path %q (isDir=%v)", relative, isDir)

	if isDir {
		err = a.dataRoot.RemoveAll(relative)
	} else {
		err = a.dataRoot.Remove(relative)
	}
	if err != nil {
		if os.IsNotExist(err) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// #nosec G706 -- %q escapes control characters in both request-derived values.
		log.Printf("[DeleteHandler] Delete failed for %q: %q", relative, err.Error())
		http.Error(w, "Delete failed", http.StatusInternalServerError)
		return
	}

	// #nosec G706 -- %q escapes control characters in the authenticated, root-confined path.
	log.Printf("[DeleteHandler] Successfully deleted %q", relative)
	w.WriteHeader(http.StatusOK)
}
