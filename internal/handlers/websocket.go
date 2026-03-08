package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/psuthar/talkback/internal/models"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// Allow all origins for development
		// In production, you should validate the origin
		return true
	},
}

// SessionHub manages WebSocket connections for a session
type SessionHub struct {
	// Map of session ID to set of connections
	sessions map[uuid.UUID]map[*Connection]bool
	// Mutex to protect the sessions map
	mu sync.RWMutex
	// Channel for broadcasting messages
	broadcast chan *SessionMessage
	// Channel for registering connections
	register chan *Connection
	// Channel for unregistering connections
	unregister chan *Connection
}

// Connection represents a WebSocket connection
type Connection struct {
	// The WebSocket connection
	ws *websocket.Conn
	// The session ID this connection is subscribed to
	sessionID uuid.UUID
	// Buffered channel of outbound messages
	send chan []byte
}

// SessionMessage represents a message to be broadcast to a session
type SessionMessage struct {
	SessionID uuid.UUID
	Type      string      `json:"type"` // "question_created", "answer_created", "answer_updated"
	Data      interface{} `json:"data"`
}

// NewSessionHub creates a new SessionHub
func NewSessionHub() *SessionHub {
	return &SessionHub{
		sessions:   make(map[uuid.UUID]map[*Connection]bool),
		broadcast:  make(chan *SessionMessage, 256),
		register:   make(chan *Connection),
		unregister: make(chan *Connection),
	}
}

// Run starts the hub's main loop
func (h *SessionHub) Run() {
	for {
		select {
		case conn := <-h.register:
			h.mu.Lock()
			if h.sessions[conn.sessionID] == nil {
				h.sessions[conn.sessionID] = make(map[*Connection]bool)
			}
			h.sessions[conn.sessionID][conn] = true
			h.mu.Unlock()
			log.Printf("WebSocket connection registered for session %s (total connections: %d)", conn.sessionID, len(h.sessions[conn.sessionID]))

		case conn := <-h.unregister:
			h.mu.Lock()
			if connections, ok := h.sessions[conn.sessionID]; ok {
				if _, ok := connections[conn]; ok {
					delete(connections, conn)
					close(conn.send)
					if len(connections) == 0 {
						delete(h.sessions, conn.sessionID)
					}
				}
			}
			h.mu.Unlock()
			log.Printf("WebSocket connection unregistered for session %s", conn.sessionID)

		case message := <-h.broadcast:
			h.mu.RLock()
			connections := h.sessions[message.SessionID]
			connectionCount := len(connections)
			h.mu.RUnlock()

			log.Printf("WebSocket: Broadcasting %s to session %s (%d connections)", message.Type, message.SessionID, connectionCount)

			if connections != nil {
				// Marshal the message to JSON
				data, err := json.Marshal(message)
				if err != nil {
					log.Printf("Error marshaling WebSocket message: %v", err)
					continue
				}

				// Send to all connections for this session
				sentCount := 0
				for conn := range connections {
					select {
					case conn.send <- data:
						sentCount++
					default:
						// Connection is blocked, close it
						log.Printf("WebSocket: Connection blocked, closing connection for session %s", message.SessionID)
						close(conn.send)
						h.mu.Lock()
						delete(connections, conn)
						if len(connections) == 0 {
							delete(h.sessions, message.SessionID)
						}
						h.mu.Unlock()
					}
				}
				log.Printf("WebSocket: Sent message to %d/%d connections for session %s", sentCount, connectionCount, message.SessionID)
			} else {
				log.Printf("WebSocket: No connections found for session %s", message.SessionID)
			}
		}
	}
}

// BroadcastQuestionCreated broadcasts a question created event
func (h *SessionHub) BroadcastQuestionCreated(sessionID uuid.UUID, question *models.Question) {
	h.broadcast <- &SessionMessage{
		SessionID: sessionID,
		Type:      "question_created",
		Data:      question,
	}
}

// BroadcastAnswerCreated broadcasts an answer created/updated event
func (h *SessionHub) BroadcastAnswerCreated(sessionID uuid.UUID, answer *models.Answer) {
	h.broadcast <- &SessionMessage{
		SessionID: sessionID,
		Type:      "answer_created",
		Data:      answer,
	}
}

// BroadcastAnswerUpdated broadcasts an answer updated event
func (h *SessionHub) BroadcastAnswerUpdated(sessionID uuid.UUID, answer *models.Answer) {
	h.broadcast <- &SessionMessage{
		SessionID: sessionID,
		Type:      "answer_updated",
		Data:      answer,
	}
}

// BroadcastSessionProcessingReady notifies clients that Zoom import (or other processing) for the session reached ready.
func (h *SessionHub) BroadcastSessionProcessingReady(sessionID uuid.UUID) {
	h.broadcast <- &SessionMessage{
		SessionID: sessionID,
		Type:      "session_processing_ready",
		Data:      map[string]string{"state": "ready"},
	}
}

// BroadcastInvitationAccepted notifies clients that an invitation was accepted so they can refetch the invitations list.
func (h *SessionHub) BroadcastInvitationAccepted(sessionID uuid.UUID) {
	h.broadcast <- &SessionMessage{
		SessionID: sessionID,
		Type:      "invitation_accepted",
		Data:      map[string]string{"session_id": sessionID.String()},
	}
}

// BroadcastSessionUpdated notifies clients that session data changed (e.g. materials added/updated/deleted, transcript ready) so they refetch the session.
func (h *SessionHub) BroadcastSessionUpdated(sessionID uuid.UUID) {
	h.broadcast <- &SessionMessage{
		SessionID: sessionID,
		Type:      "session_updated",
		Data:      map[string]string{"session_id": sessionID.String()},
	}
}

// HandleWebSocket handles WebSocket connections for session updates
func (h *Handlers) HandleWebSocket(hub *SessionHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract session ID from query parameter
		sessionIDStr := r.URL.Query().Get("session")
		if sessionIDStr == "" {
			http.Error(w, "session parameter is required", http.StatusBadRequest)
			return
		}

		sessionID, err := uuid.Parse(sessionIDStr)
		if err != nil {
			http.Error(w, "Invalid session ID", http.StatusBadRequest)
			return
		}

		// Verify session exists
		_, err = h.DB.GetSession(r.Context(), sessionID)
		if err != nil {
			http.Error(w, "Session not found", http.StatusNotFound)
			return
		}

		// Upgrade connection to WebSocket
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket upgrade error: %v", err)
			return
		}

		conn := &Connection{
			ws:        ws,
			sessionID: sessionID,
			send:      make(chan []byte, 256),
		}

		hub.register <- conn

		// Start goroutines for reading and writing
		go conn.writePump()
		go conn.readPump(hub)
	}
}

// readPump pumps messages from the WebSocket connection
func (c *Connection) readPump(hub *SessionHub) {
	defer func() {
		hub.unregister <- c
		c.ws.Close()
	}()

	// Set read deadline and pong handler
	c.ws.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.ws.SetPongHandler(func(string) error {
		c.ws.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, _, err := c.ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}
		// Reset read deadline after receiving a message
		c.ws.SetReadDeadline(time.Now().Add(60 * time.Second))
	}
}

// writePump pumps messages to the WebSocket connection
func (c *Connection) writePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		c.ws.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.ws.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.ws.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued messages to the current message
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			// Send ping to keep connection alive
			c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
