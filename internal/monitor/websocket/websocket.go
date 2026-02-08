package websocket

import (
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Message represents a WebSocket message

type Message struct {
	Type string `json:"type"`

	Data interface{} `json:"data"`
}

// Client represents a connected WebSocket client
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan interface{}
}

// Hub manages WebSocket clients
type Hub struct {
	clients   map[*Client]bool
	broadcast chan Message
	reg       chan *Client
	unreg     chan *Client
	clientsMu sync.Mutex
}

// New creates a new WebSocket hub
func New() *Hub {
	h := &Hub{
		clients:   make(map[*Client]bool),
		broadcast: make(chan Message, 256),
		reg:       make(chan *Client),
		unreg:     make(chan *Client),
	}
	go h.run()
	return h
}

func (h *Hub) run() {
	for {
		select {
		case client := <-h.reg:
			h.clientsMu.Lock()
			h.clients[client] = true
			h.clientsMu.Unlock()
		case client := <-h.unreg:
			h.clientsMu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.clientsMu.Unlock()
		case msg := <-h.broadcast:
			h.clientsMu.Lock()
			for client := range h.clients {
				select {
				case client.send <- msg:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.clientsMu.Unlock()
		}
	}
}

// RegisterClient creates and starts a new client
func (h *Hub) RegisterClient(conn *websocket.Conn) *Client {
	client := &Client{hub: h, conn: conn, send: make(chan interface{}, 256)}
	h.reg <- client
	go client.writePump()
	return client
}

func (h *Hub) UnregisterClient(client *Client) {
	h.unreg <- client
}

func (c *Client) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				// The hub closed the channel.
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.conn.WriteJSON(message); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// Broadcast sends a message to all connected clients
func (h *Hub) Broadcast(msgType string, data interface{}) {
	select {
	case h.broadcast <- Message{Type: msgType, Data: data}:
	default:
		// Drop message if broadcast channel is full to prevent blocking the app
	}
}

// SendDirect sends a message to a specific client
func (c *Client) SendDirect(msgType string, data interface{}) {
	select {
	case c.send <- Message{Type: msgType, Data: data}:
	default:
		// Channel full, client might be slow, hub will eventually unregister it
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
