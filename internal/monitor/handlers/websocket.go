package handlers

import (
	"log"
	"net/http"
)

// WebSocket handler
func (h *Handlers) WebSocket(w http.ResponseWriter, r *http.Request) {
	wsConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}

	h.wsHub.Register(wsConn)

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
			h.wsHub.Unregister(wsConn)
			break
		}
	}
}
