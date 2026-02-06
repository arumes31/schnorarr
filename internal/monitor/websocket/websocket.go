package websocket

import (
	"log"
	"sync"

	"github.com/gorilla/websocket"
)

// Message represents a WebSocket message
type Message struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// Hub manages WebSocket clients
type Hub struct {
	clients   map[*websocket.Conn]bool
	clientsMu sync.Mutex
}

// New creates a new WebSocket hub
func New() *Hub {
	return &Hub{
		clients: make(map[*websocket.Conn]bool),
	}
}

// Register adds a client to the hub
func (h *Hub) Register(conn *websocket.Conn) {
	h.clientsMu.Lock()
	defer h.clientsMu.Unlock()
	h.clients[conn] = true
}

// Unregister removes a client from the hub
func (h *Hub) Unregister(conn *websocket.Conn) {
	h.clientsMu.Lock()
	defer h.clientsMu.Unlock()
	delete(h.clients, conn)
}

// Broadcast sends a message to all connected clients
func (h *Hub) Broadcast(msgType string, data interface{}) {
	h.clientsMu.Lock()
	defer h.clientsMu.Unlock()

	msg := Message{Type: msgType, Data: data}
	for client := range h.clients {
		err := client.WriteJSON(msg)
		if err != nil {
			log.Printf("WS Error: %v", err)
			client.Close()
			delete(h.clients, client)
		}
	}
}
