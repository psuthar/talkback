package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/auth"
	"github.com/psuthar/talkback/internal/models"
)

// CreateMockQuestion creates a mock question for testing WebSocket functionality
// without calling OpenAI or performing real integrations
func (h *Handlers) CreateMockQuestion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID from URL path
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 4 || pathParts[0] != "sessions" || pathParts[2] != "questions" || pathParts[3] != "mock" {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}

	sessionID, err := uuid.Parse(pathParts[1])
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	// Verify session exists
	_, err = h.DB.GetSession(r.Context(), sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Session not found: %v", err), http.StatusNotFound)
		return
	}

	// Get artifacts for this session (needed for question structure, but won't persist)
	artifacts, err := h.DB.GetArtifactsBySessionID(r.Context(), sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get artifacts: %v", err), http.StatusInternalServerError)
		return
	}

	if len(artifacts) == 0 {
		http.Error(w, "No artifacts found for this session", http.StatusNotFound)
		return
	}

	// Enforce per-session question limit (mock questions count toward limit conceptually; limit is on persisted count)
	count, err := h.DB.CountQuestionsBySessionID(r.Context(), sessionID)
	if err != nil {
		log.Printf("CreateMockQuestion CountQuestionsBySessionID: %v", err)
		http.Error(w, "Failed to check question limit", http.StatusInternalServerError)
		return
	}
	if count >= auth.Config.MaxQuestionsPerSession {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":          "session question limit reached",
			"max_questions":  auth.Config.MaxQuestionsPerSession,
		})
		return
	}

	// Use first artifact (for structure only, not persisted)
	artifact := artifacts[0]

	// Create mock question text with current timestamp
	mockQuestionText := fmt.Sprintf("Mock question asked on %s", time.Now().Format("2006-01-02 15:04:05"))

	// Create mock question in memory only (NOT persisted to database)
	question := &models.Question{
		ID:             uuid.New(),
		ArtifactID:     artifact.ID,
		SessionID:      sessionID,
		QuestionText:   mockQuestionText,
		QuestionSource: models.QuestionSourceText,
	}

	// Create a mock answer in memory only (NOT persisted to database)
	mockAnswerText := "This is a mock answer for testing WebSocket functionality. No OpenAI API was called. This data is not persisted and will disappear on refresh."
	answer := &models.Answer{
		ID:           uuid.New(),
		QuestionID:   question.ID,
		AnswerText:   mockAnswerText,
		AnswerStatus: models.AnswerStatusAnswered,
		Confidence:   1.0,
		Citations:    []models.Citation{},
		Model:        stringPtr("mock"),
	}

	// Broadcast question and answer created events via WebSocket
	// Note: These are NOT persisted to database - they only exist in memory for WebSocket testing
	if h.Hub != nil {
		h.Hub.BroadcastQuestionCreated(sessionID, question)
		h.Hub.BroadcastAnswerCreated(sessionID, answer)
		log.Printf("Mock question broadcasted via WebSocket for session %s (not persisted to database)", sessionID)
	} else {
		http.Error(w, "WebSocket hub not available", http.StatusInternalServerError)
		return
	}

	// Return response so caller knows it worked
	// Note: This data is not persisted and will disappear on refresh
	response := AskQuestionResponse{
		Question: question,
		Answer:   answer,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}
