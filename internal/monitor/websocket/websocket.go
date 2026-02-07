package websocket

import (

	"fmt"

	"strings"

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

			fmt.Printf("WS Error: %v\n", err)

			client.Close()

			delete(h.clients, client)

		}

	}

}

// LogWriter is an io.Writer that broadcasts logs to WebSocket
type LogWriter struct {
	hub *Hub
}

// NewLogWriter creates a new log writer
func NewLogWriter(hub *Hub) *LogWriter {
	return &LogWriter{hub: hub}
}

func (w *LogWriter) Write(p []byte) (n int, err error) {
	msg := string(p)
	level := "info"
	upperMsg := strings.ToUpper(msg)
	if strings.Contains(upperMsg, "ERROR") || strings.Contains(upperMsg, "FAILED") {
		level = "error"
	} else if strings.Contains(upperMsg, "WARN") || strings.Contains(upperMsg, "WARNING") {
		level = "warn"
	}

	w.hub.Broadcast("log", map[string]string{
		"msg":   msg,
		"level": level,
	})
	return len(p), nil
}
