package handlers

import (
	"log"
	"net/http"
	"time"
)

// WebSocket handler
func (h *Handlers) WebSocket(w http.ResponseWriter, r *http.Request) {
	if AuthEnabled {
		cookie, err := r.Cookie("schnorarr_session")
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		h.sessionMu.RLock()
		session, ok := h.sessions[cookie.Value]
		h.sessionMu.RUnlock()
		if !ok || time.Now().After(session.Expires) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	wsConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}

	h.wsHub.Register(wsConn)
	defer func() {
		_ = wsConn.Close()
		h.wsHub.Unregister(wsConn)
	}()

	// Send initial state
	if err := wsConn.WriteJSON(struct {
		Type string
		Data string
	}{Type: "init", Data: "Connected"}); err != nil {
		log.Printf("WS Init Error: %v", err)
	}

	// Keep alive / Read loop
	for {
		_, _, err := wsConn.ReadMessage()
		if err != nil {
			break
		}
	}
}
