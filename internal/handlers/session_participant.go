package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
)

// JoinParticipantRequest body for POST /sessions/:id/participants
type JoinParticipantRequest struct {
	ParticipantRef string `json:"participant_ref"`
}

// CreateEventRequest body for POST /sessions/:id/events
type CreateEventRequest struct {
	ParticipantRef   *string                `json:"participant_ref,omitempty"`
	EventType        string                 `json:"event_type"`
	VideoTimeSeconds *int                   `json:"video_time_seconds,omitempty"`
	Payload          map[string]interface{} `json:"payload,omitempty"`
}

// MarkSessionMaterialsSeenRequest body for POST /sessions/:id/materials/seen
type MarkSessionMaterialsSeenRequest struct {
	ParticipantRef string   `json:"participant_ref"`
	MaterialIDs    []string `json:"material_ids"`
}

// MarkSessionMaterialsSeen records that a participant has viewed the given materials (clears "new" marker).
func (h *Handlers) MarkSessionMaterialsSeen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 4 || pathParts[0] != "sessions" || pathParts[2] != "materials" || pathParts[3] != "seen" {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(pathParts[1])
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	if _, err := h.DB.GetSession(r.Context(), sessionID); err != nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}
	var req MarkSessionMaterialsSeenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.ParticipantRef == "" {
		http.Error(w, "participant_ref is required", http.StatusBadRequest)
		return
	}
	var materialUUIDs []uuid.UUID
	for _, s := range req.MaterialIDs {
		if s == "" {
			continue
		}
		id, err := uuid.Parse(s)
		if err != nil {
			continue
		}
		materialUUIDs = append(materialUUIDs, id)
	}
	if err := h.DB.MarkMaterialsSeenByParticipant(r.Context(), sessionID, req.ParticipantRef, materialUUIDs); err != nil {
		log.Printf("MarkSessionMaterialsSeen: %v", err)
		http.Error(w, "Failed to mark materials as seen", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// JoinSessionParticipant creates or updates a session participant
func (h *Handlers) JoinSessionParticipant(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 3 || pathParts[0] != "sessions" || pathParts[2] != "participants" {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}

	sessionID, err := uuid.Parse(pathParts[1])
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	_, err = h.DB.GetSession(r.Context(), sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Session not found: %v", err), http.StatusNotFound)
		return
	}

	var req JoinParticipantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if req.ParticipantRef == "" {
		http.Error(w, "participant_ref is required", http.StatusBadRequest)
		return
	}

	participant, err := h.DB.UpsertSessionParticipant(r.Context(), sessionID, req.ParticipantRef)
	if err != nil {
		log.Printf("Error upserting participant: %v", err)
		http.Error(w, fmt.Sprintf("Failed to join session: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(participant)
}

// CreateSessionEvent creates a new session event
func (h *Handlers) CreateSessionEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 3 || pathParts[0] != "sessions" || pathParts[2] != "events" {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}

	sessionID, err := uuid.Parse(pathParts[1])
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	_, err = h.DB.GetSession(r.Context(), sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Session not found: %v", err), http.StatusNotFound)
		return
	}

	var req CreateEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	eventType := models.SessionEventType(req.EventType)
	validTypes := []models.SessionEventType{
		models.SessionEventTypeJoin,
		models.SessionEventTypeLeave,
		models.SessionEventTypePlay,
		models.SessionEventTypePause,
		models.SessionEventTypeSeek,
		models.SessionEventTypeQuestion,
	}
	valid := false
	for _, vt := range validTypes {
		if eventType == vt {
			valid = true
			break
		}
	}
	if !valid {
		http.Error(w, fmt.Sprintf("Invalid event_type: %s", req.EventType), http.StatusBadRequest)
		return
	}

	event := &models.SessionEvent{
		ID:               uuid.New(),
		SessionID:        sessionID,
		ParticipantRef:   req.ParticipantRef,
		EventType:        eventType,
		VideoTimeSeconds: req.VideoTimeSeconds,
		Payload:          req.Payload,
	}
	if event.Payload == nil {
		event.Payload = make(map[string]interface{})
	}

	if err := h.DB.CreateSessionEvent(r.Context(), event); err != nil {
		log.Printf("Error creating session event: %v", err)
		http.Error(w, fmt.Sprintf("Failed to create event: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(event)
}
