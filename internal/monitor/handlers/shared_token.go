package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"schnorarr/internal/storage"
)

func (h *Handlers) EngineSharedToken(w http.ResponseWriter, r *http.Request) {
	h.auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/engine/"), "/shared-token")
		for _, engine := range h.engineProvider() {
			cfg := engine.GetConfig()
			if cfg.ID != id {
				continue
			}
			if cfg.SharedToken == "" {
				http.Error(w, "Shared token is not available", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"token": cfg.SharedToken, "filename": storage.MarkerName, "engine": engine.GetAlias(),
				"source": cfg.SourceDir, "target": cfg.TargetDir,
			})
			return
		}
		http.Error(w, "Engine not found", http.StatusNotFound)
	})(w, r)
}
