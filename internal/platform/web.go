package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Message represents a platform message
type Message struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Platform  string    `json:"platform"`
	Timestamp time.Time `json:"timestamp"`
}

// MessageHandler handles incoming messages
type MessageHandler func(msg *Message) (*Message, error)

// PlatformAdapter defines the interface for platform adapters
type PlatformAdapter interface {
	// Name returns the platform name
	Name() string

	// Start starts the platform adapter
	Start(ctx context.Context) error

	// Stop stops the platform adapter
	Stop() error

	// SendMessage sends a message to the platform
	SendMessage(msg *Message) error

	// OnMessage registers a message handler
	OnMessage(handler MessageHandler)
}

// WebAdapter implements the Web platform adapter with WebSocket
type WebAdapter struct {
	upgrader  websocket.Upgrader
	clients   map[*websocket.Conn]string // conn -> session_id
	mu        sync.RWMutex
	handler   MessageHandler
	stopCh    chan struct{}
	port      int
	corsOrigins []string
}

// NewWebAdapter creates a new Web adapter
func NewWebAdapter(port int, corsOrigins []string) *WebAdapter {
	return &WebAdapter{
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				if len(corsOrigins) == 0 {
					return true
				}
				origin := r.Header.Get("Origin")
				for _, allowed := range corsOrigins {
					if allowed == "*" || allowed == origin {
						return true
					}
				}
				return false
			},
		},
		clients:     make(map[*websocket.Conn]string),
		stopCh:      make(chan struct{}),
		port:        port,
		corsOrigins: corsOrigins,
	}
}

// Name returns the platform name
func (a *WebAdapter) Name() string {
	return "web"
}

// Start starts the Web adapter
func (a *WebAdapter) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", a.handleWebSocket)
	mux.HandleFunc("/health", a.handleHealth)

	addr := fmt.Sprintf(":%d", a.port)
	log.Printf("Web adapter starting on %s", addr)

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Web adapter error: %v", err)
		}
	}()

	go func() {
		<-ctx.Done()
		server.Shutdown(context.Background())
	}()

	return nil
}

// Stop stops the Web adapter
func (a *WebAdapter) Stop() error {
	close(a.stopCh)

	a.mu.Lock()
	defer a.mu.Unlock()

	for conn := range a.clients {
		conn.Close()
	}

	return nil
}

// SendMessage sends a message to a WebSocket client
func (a *WebAdapter) SendMessage(msg *Message) error {
	a.mu.RLock()
	defer a.mu.RUnlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	for conn, sessionID := range a.clients {
		if sessionID == msg.SessionID || sessionID == "" {
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				log.Printf("Failed to send message to client: %v", err)
				conn.Close()
				delete(a.clients, conn)
			}
		}
	}

	return nil
}

// OnMessage registers a message handler
func (a *WebAdapter) OnMessage(handler MessageHandler) {
	a.handler = handler
}

// handleWebSocket handles WebSocket connections
func (a *WebAdapter) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := a.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection: %v", err)
		return
	}
	defer conn.Close()

	// Get session ID from query
	sessionID := r.URL.Query().Get("session_id")

	a.mu.Lock()
	a.clients[conn] = sessionID
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		delete(a.clients, conn)
		a.mu.Unlock()
	}()

	// Send welcome message
	welcome := map[string]interface{}{
		"type":    "connected",
		"message": "Connected to CodeGateway",
	}
	welcomeData, _ := json.Marshal(welcome)
	conn.WriteMessage(websocket.TextMessage, welcomeData)

	// Read messages
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		// Parse message
		var msg Message
		if err := json.Unmarshal(message, &msg); err != nil {
			log.Printf("Failed to parse message: %v", err)
			continue
		}

		msg.Platform = "web"
		msg.Timestamp = time.Now()

		// Handle message
		if a.handler != nil {
			resp, err := a.handler(&msg)
			if err != nil {
				log.Printf("Failed to handle message: %v", err)
				continue
			}

			if resp != nil {
				respData, _ := json.Marshal(resp)
				conn.WriteMessage(websocket.TextMessage, respData)
			}
		}
	}
}

// handleHealth handles health check requests
func (a *WebAdapter) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"platform": "web",
		"clients": len(a.clients),
	})
}
