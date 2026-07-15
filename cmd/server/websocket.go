package server

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/alex/codegateway/internal/config"
	"github.com/alex/codegateway/internal/db"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for development
	},
}

type WSClient struct {
	conn      *websocket.Conn
	sessionID string
	accountID int64
	send      chan []byte
}

type WSHub struct {
	clients    map[*WSClient]bool
	broadcast  chan []byte
	register   chan *WSClient
	unregister chan *WSClient
	mu         sync.RWMutex
}

func newWSHub() *WSHub {
	return &WSHub{
		clients:    make(map[*WSClient]bool),
		broadcast:  make(chan []byte),
		register:   make(chan *WSClient),
		unregister: make(chan *WSClient),
	}
}

func (h *WSHub) run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			log.Printf("WebSocket client connected: %s", client.sessionID)

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			log.Printf("WebSocket client disconnected: %s", client.sessionID)

		case message := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.mu.RUnlock()
		}
	}
}

func handleWebSocket(database *db.DB, cfg *config.Config, hub *WSHub) gin.HandlerFunc {
	return func(c *gin.Context) {
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			log.Printf("Failed to upgrade connection: %v", err)
			return
		}

		sessionID := c.Query("session_id")
		if sessionID == "" {
			sessionID = generateSessionID()
		}

		accountID := getAccountID(c)

		client := &WSClient{
			conn:      conn,
			sessionID: sessionID,
			accountID: accountID,
			send:      make(chan []byte, 256),
		}

		hub.register <- client

		// Send welcome message
		welcome := map[string]interface{}{
			"type":       "connected",
			"session_id": sessionID,
			"message":    "Connected to CodeGateway",
		}
		welcomeData, _ := json.Marshal(welcome)
		client.send <- welcomeData

		go client.writePump()
		go client.readPump(database, cfg, hub)
	}
}

func (c *WSClient) readPump(database *db.DB, cfg *config.Config, hub *WSHub) {
	defer func() {
		hub.unregister <- c
		c.conn.Close()
	}()

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		// Parse message
		var msg struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(message, &msg); err != nil {
			log.Printf("Failed to parse message: %v", err)
			continue
		}

		// Process message and get response
		log.Printf("[chat/ws] recv account=%d session=%s bytes=%d", c.accountID, c.sessionID, len(msg.Content))
		response := processMessage(database, cfg, c.sessionID, msg.Content, c.accountID)

		// Send response
		respData, _ := json.Marshal(map[string]interface{}{
			"role":       "assistant",
			"content":    response,
			"session_id": c.sessionID,
		})
		c.send <- respData
	}
}

func (c *WSClient) writePump() {
	defer c.conn.Close()

	for message := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
			log.Printf("Failed to write message: %v", err)
			break
		}
	}
}

func generateSessionID() string {
	return "ws-" + randomString(8)
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[i%len(letters)]
	}
	return string(b)
}
