package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionHub(t *testing.T) {
	t.Parallel()
	hub := NewSessionHub()
	go hub.Run()
	defer func() {
		// Give hub time to process any pending messages
		time.Sleep(100 * time.Millisecond)
	}()

	t.Run("registers and unregisters connections", func(t *testing.T) {
		sessionID := uuid.New()
		conn := &Connection{
			ws:        nil, // Not needed for this test
			sessionID: sessionID,
			send:      make(chan []byte, 256),
		}

		// Register connection
		hub.register <- conn
		time.Sleep(50 * time.Millisecond) // Give hub time to process

		hub.mu.RLock()
		connections, exists := hub.sessions[sessionID]
		hub.mu.RUnlock()

		assert.True(t, exists, "Session should exist after registration")
		assert.True(t, connections[conn], "Connection should be registered")

		// Unregister connection
		hub.unregister <- conn
		time.Sleep(50 * time.Millisecond) // Give hub time to process

		hub.mu.RLock()
		_, exists = hub.sessions[sessionID]
		hub.mu.RUnlock()

		assert.False(t, exists, "Session should be removed when last connection unregisters")
	})

	t.Run("broadcasts messages to all connections in session", func(t *testing.T) {
		sessionID := uuid.New()

		// Create mock connections
		conn1 := &Connection{
			ws:        nil,
			sessionID: sessionID,
			send:      make(chan []byte, 256),
		}
		conn2 := &Connection{
			ws:        nil,
			sessionID: sessionID,
			send:      make(chan []byte, 256),
		}

		// Register both connections
		hub.register <- conn1
		hub.register <- conn2
		time.Sleep(50 * time.Millisecond)

		// Broadcast a message
		message := &SessionMessage{
			SessionID: sessionID,
			Type:      "test_message",
			Data:      map[string]string{"test": "data"},
		}
		hub.broadcast <- message
		time.Sleep(50 * time.Millisecond)

		// Check that both connections received the message
		select {
		case msg1 := <-conn1.send:
			var receivedMsg SessionMessage
			err := json.Unmarshal(msg1, &receivedMsg)
			require.NoError(t, err)
			assert.Equal(t, "test_message", receivedMsg.Type)
		case <-time.After(1 * time.Second):
			t.Fatal("Connection 1 did not receive message")
		}

		select {
		case msg2 := <-conn2.send:
			var receivedMsg SessionMessage
			err := json.Unmarshal(msg2, &receivedMsg)
			require.NoError(t, err)
			assert.Equal(t, "test_message", receivedMsg.Type)
		case <-time.After(1 * time.Second):
			t.Fatal("Connection 2 did not receive message")
		}

		// Cleanup
		hub.unregister <- conn1
		hub.unregister <- conn2
		time.Sleep(50 * time.Millisecond)
	})

	t.Run("does not broadcast to connections in different sessions", func(t *testing.T) {
		sessionID1 := uuid.New()
		sessionID2 := uuid.New()

		conn1 := &Connection{
			ws:        nil,
			sessionID: sessionID1,
			send:      make(chan []byte, 256),
		}
		conn2 := &Connection{
			ws:        nil,
			sessionID: sessionID2,
			send:      make(chan []byte, 256),
		}

		// Register both connections
		hub.register <- conn1
		hub.register <- conn2
		time.Sleep(50 * time.Millisecond)

		// Broadcast to session 1 only
		message := &SessionMessage{
			SessionID: sessionID1,
			Type:      "test_message",
			Data:      map[string]string{"test": "data"},
		}
		hub.broadcast <- message
		time.Sleep(50 * time.Millisecond)

		// Connection 1 should receive the message
		select {
		case <-conn1.send:
			// Good
		case <-time.After(1 * time.Second):
			t.Fatal("Connection 1 should have received message")
		}

		// Connection 2 should NOT receive the message
		select {
		case <-conn2.send:
			t.Fatal("Connection 2 should not have received message for different session")
		case <-time.After(100 * time.Millisecond):
			// Good - no message received
		}

		// Cleanup
		hub.unregister <- conn1
		hub.unregister <- conn2
		time.Sleep(50 * time.Millisecond)
	})

	t.Run("BroadcastQuestionCreated sends correct message type", func(t *testing.T) {
		sessionID := uuid.New()
		question := &models.Question{
			ID:           uuid.New(),
			SessionID:    sessionID,
			QuestionText: "Test question",
		}

		conn := &Connection{
			ws:        nil,
			sessionID: sessionID,
			send:      make(chan []byte, 256),
		}

		hub.register <- conn
		time.Sleep(50 * time.Millisecond)

		hub.BroadcastQuestionCreated(sessionID, question)
		time.Sleep(50 * time.Millisecond)

		select {
		case msg := <-conn.send:
			var receivedMsg SessionMessage
			err := json.Unmarshal(msg, &receivedMsg)
			require.NoError(t, err)
			assert.Equal(t, "question_created", receivedMsg.Type)
			assert.Equal(t, sessionID, receivedMsg.SessionID)
		case <-time.After(1 * time.Second):
			t.Fatal("Connection did not receive broadcast")
		}

		hub.unregister <- conn
		time.Sleep(50 * time.Millisecond)
	})

	t.Run("BroadcastAnswerCreated sends correct message type", func(t *testing.T) {
		sessionID := uuid.New()
		answer := &models.Answer{
			ID:          uuid.New(),
			QuestionID:  uuid.New(),
			AnswerText:  "Test answer",
			AnswerStatus: models.AnswerStatusAnswered,
		}

		conn := &Connection{
			ws:        nil,
			sessionID: sessionID,
			send:      make(chan []byte, 256),
		}

		hub.register <- conn
		time.Sleep(50 * time.Millisecond)

		hub.BroadcastAnswerCreated(sessionID, answer)
		time.Sleep(50 * time.Millisecond)

		select {
		case msg := <-conn.send:
			var receivedMsg SessionMessage
			err := json.Unmarshal(msg, &receivedMsg)
			require.NoError(t, err)
			assert.Equal(t, "answer_created", receivedMsg.Type)
			assert.Equal(t, sessionID, receivedMsg.SessionID)
		case <-time.After(1 * time.Second):
			t.Fatal("Connection did not receive broadcast")
		}

		hub.unregister <- conn
		time.Sleep(50 * time.Millisecond)
	})

	t.Run("BroadcastAnswerUpdated sends correct message type", func(t *testing.T) {
		sessionID := uuid.New()
		answer := &models.Answer{
			ID:          uuid.New(),
			QuestionID:  uuid.New(),
			AnswerText:  "Updated answer",
			AnswerStatus: models.AnswerStatusAnswered,
		}

		conn := &Connection{
			ws:        nil,
			sessionID: sessionID,
			send:      make(chan []byte, 256),
		}

		hub.register <- conn
		time.Sleep(50 * time.Millisecond)

		hub.BroadcastAnswerUpdated(sessionID, answer)
		time.Sleep(50 * time.Millisecond)

		select {
		case msg := <-conn.send:
			var receivedMsg SessionMessage
			err := json.Unmarshal(msg, &receivedMsg)
			require.NoError(t, err)
			assert.Equal(t, "answer_updated", receivedMsg.Type)
			assert.Equal(t, sessionID, receivedMsg.SessionID)
		case <-time.After(1 * time.Second):
			t.Fatal("Connection did not receive broadcast")
		}

		hub.unregister <- conn
		time.Sleep(50 * time.Millisecond)
	})
}

func TestHandleWebSocket(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	// Create a test session
	session := &models.Session{
		ID:        uuid.New(),
		Title:     "Test Session",
		Status:     models.SessionStatusOpen,
		CreatedBy: stringPtr("test-user"),
	}
	err := h.DB.CreateSession(context.Background(), session)
	require.NoError(t, err)

	hub := h.Hub

	t.Run("requires session parameter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ws/session", nil)
		w := httptest.NewRecorder()

		h.HandleWebSocket(hub)(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "session parameter is required")
	})

	t.Run("rejects invalid session ID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ws/session?session=invalid", nil)
		w := httptest.NewRecorder()

		h.HandleWebSocket(hub)(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Invalid session ID")
	})

	t.Run("rejects non-existent session", func(t *testing.T) {
		nonExistentID := uuid.New()
		req := httptest.NewRequest(http.MethodGet, "/ws/session?session="+nonExistentID.String(), nil)
		w := httptest.NewRecorder()

		h.HandleWebSocket(hub)(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "Session not found")
	})

	t.Run("upgrades valid HTTP request to WebSocket", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(h.HandleWebSocket(hub)))
		defer server.Close()

		// Convert http:// to ws://
		wsURL := "ws" + server.URL[4:] + "/ws/session?session=" + session.ID.String()

		// Dial WebSocket connection
		conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
		require.NoError(t, err)
		require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
		defer conn.Close()

		// Give hub time to register the connection
		time.Sleep(100 * time.Millisecond)

		// Verify connection is registered
		hub.mu.RLock()
		connections, exists := hub.sessions[session.ID]
		hub.mu.RUnlock()

		assert.True(t, exists, "Session should be registered")
		assert.Greater(t, len(connections), 0, "Should have at least one connection")

		// Test that we can receive broadcasts
		hub.BroadcastQuestionCreated(session.ID, &models.Question{
			ID:           uuid.New(),
			SessionID:    session.ID,
			QuestionText: "Test question",
		})

		// Read message from WebSocket
		_, message, err := conn.ReadMessage()
		require.NoError(t, err)

		var receivedMsg SessionMessage
		err = json.Unmarshal(message, &receivedMsg)
		require.NoError(t, err)
		assert.Equal(t, "question_created", receivedMsg.Type)
		assert.Equal(t, session.ID, receivedMsg.SessionID)
	})
}
