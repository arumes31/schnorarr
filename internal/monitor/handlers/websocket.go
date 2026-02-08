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

	client := h.wsHub.RegisterClient(wsConn)
	defer h.wsHub.UnregisterClient(client)

	// Send initial state
	client.SendDirect("init", "Connected")

	// Keep alive / Read loop
	wsConn.SetReadLimit(512)
	_ = wsConn.SetReadDeadline(time.Now().Add(60 * time.Second))
	wsConn.SetPongHandler(func(string) error { _ = wsConn.SetReadDeadline(time.Now().Add(60 * time.Second)); return nil })
	for {
		_, _, err := wsConn.ReadMessage()
		if err != nil {
			break
		}
	}
}
