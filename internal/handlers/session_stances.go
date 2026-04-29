package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
)

type submitStanceRequest struct {
	Stance    string  `json:"stance"`
	Rationale *string `json:"rationale,omitempty"`
}

type stancesResponse struct {
	Aggregate *models.StanceAggregate          `json:"aggregate"`
	MyStance  *models.DecisionStance           `json:"my_stance,omitempty"`
	Responses []*models.DecisionStanceWithUser `json:"responses,omitempty"` // creator only
	Readiness *models.DecisionMakerReadiness   `json:"readiness"`
}

// SessionSubmitStance handles POST /api/sessions/{id}/stance
func (h *Handlers) SessionSubmitStance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(parts[2])
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	session, err := h.DB.GetSession(ctx, sessionID)
	if err != nil || session == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "session not found"})
		return
	}
	if session.PrimaryDecision == nil || *session.PrimaryDecision == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]string{"error": "no primary decision set on this session"})
		return
	}
	if session.DecisionOutcome != nil && *session.DecisionOutcome != "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": "stances are locked after decision outcome is recorded"})
		return
	}

	var req submitStanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	req.Stance = strings.TrimSpace(req.Stance)
	valid := false
	for _, v := range models.ValidStances {
		if req.Stance == v {
			valid = true
			break
		}
	}
	if !valid {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid stance value"})
		return
	}
	// Truncate rationale server-side
	if req.Rationale != nil && len(*req.Rationale) > 500 {
		s := (*req.Rationale)[:500]
		req.Rationale = &s
	}

	user := UserFromContext(r.Context())
	saved, err := h.DB.CreateOrUpdateStance(ctx, sessionID, user.ID, req.Stance, req.Rationale)
	if err != nil {
		log.Printf("SessionSubmitStance CreateOrUpdateStance: %v", err)
		http.Error(w, "Failed to save stance", http.StatusInternalServerError)
		return
	}

	agg, err := h.DB.GetStanceAggregate(ctx, sessionID)
	if err != nil {
		log.Printf("SessionSubmitStance GetStanceAggregate: %v", err)
		agg = &models.StanceAggregate{}
	}

	readiness, err := h.DB.GetDecisionMakerReadiness(ctx, sessionID)
	if err != nil {
		log.Printf("SessionSubmitStance GetDecisionMakerReadiness: %v", err)
		readiness = &models.DecisionMakerReadiness{}
	}

	if h.Hub != nil {
		h.Hub.BroadcastStanceUpdated(sessionID, agg)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(stancesResponse{
		Aggregate: agg,
		MyStance:  saved,
		Readiness: readiness,
	})
}

// SessionGetStances handles GET /api/sessions/{id}/stances
func (h *Handlers) SessionGetStances(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(parts[2])
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	session, err := h.DB.GetSession(ctx, sessionID)
	if err != nil || session == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "session not found"})
		return
	}

	user := UserFromContext(r.Context())

	agg, err := h.DB.GetStanceAggregate(ctx, sessionID)
	if err != nil {
		log.Printf("SessionGetStances GetStanceAggregate: %v", err)
		agg = &models.StanceAggregate{}
	}

	myStance, err := h.DB.GetStanceByUserAndSession(ctx, sessionID, user.ID)
	if err != nil {
		log.Printf("SessionGetStances GetStanceByUserAndSession: %v", err)
	}

	readiness, err := h.DB.GetDecisionMakerReadiness(ctx, sessionID)
	if err != nil {
		log.Printf("SessionGetStances GetDecisionMakerReadiness: %v", err)
		readiness = &models.DecisionMakerReadiness{}
	}

	resp := stancesResponse{
		Aggregate: agg,
		MyStance:  myStance,
		Responses: []*models.DecisionStanceWithUser{}, // always set so client gets an array
		Readiness: readiness,
	}

	// Provide individual responses (with submitter email) to all authenticated users viewing the session
	responses, err := h.DB.GetStancesBySessionID(ctx, sessionID)
	if err != nil {
		log.Printf("SessionGetStances GetStancesBySessionID: %v", err)
	} else {
		resp.Responses = responses
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// SessionDeleteStance handles DELETE /api/sessions/{id}/stance — clears the current user's stance for the session.
func (h *Handlers) SessionDeleteStance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(parts[2])
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	session, err := h.DB.GetSession(ctx, sessionID)
	if err != nil || session == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "session not found"})
		return
	}
	if session.DecisionOutcome != nil && *session.DecisionOutcome != "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": "stances are locked after decision outcome is recorded"})
		return
	}
	user := UserFromContext(r.Context())
	if err := h.DB.DeleteStance(ctx, sessionID, user.ID); err != nil {
		log.Printf("SessionDeleteStance DeleteStance: %v", err)
		http.Error(w, "Failed to clear stance", http.StatusInternalServerError)
		return
	}
	agg, _ := h.DB.GetStanceAggregate(ctx, sessionID)
	if agg == nil {
		agg = &models.StanceAggregate{}
	}
	readiness, err := h.DB.GetDecisionMakerReadiness(ctx, sessionID)
	if err != nil {
		log.Printf("SessionDeleteStance GetDecisionMakerReadiness: %v", err)
		readiness = &models.DecisionMakerReadiness{}
	}
	if h.Hub != nil {
		h.Hub.BroadcastStanceUpdated(sessionID, agg)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(stancesResponse{
		Aggregate: agg,
		MyStance:  nil,
		Readiness: readiness,
	})
}
