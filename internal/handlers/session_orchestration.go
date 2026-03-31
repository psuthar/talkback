package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/orchestration"
)

func parseSessionOrchestrationPath(path string) (sessionID uuid.UUID, action string, tail string, ok bool) {
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	// /api/sessions/:id/orchestration/:action(/:tail)
	if len(parts) >= 5 && parts[0] == "api" && parts[1] == "sessions" && parts[3] == "orchestration" {
		sid, err := uuid.Parse(parts[2])
		if err != nil {
			return uuid.UUID{}, "", "", false
		}
		a := parts[4]
		t := ""
		if len(parts) >= 6 {
			t = parts[5]
		}
		return sid, a, t, true
	}
	// /sessions/:id/orchestration/:action(/:tail)
	if len(parts) >= 4 && parts[0] == "sessions" && parts[2] == "orchestration" {
		sid, err := uuid.Parse(parts[1])
		if err != nil {
			return uuid.UUID{}, "", "", false
		}
		a := parts[3]
		t := ""
		if len(parts) >= 5 {
			t = parts[4]
		}
		return sid, a, t, true
	}
	return uuid.UUID{}, "", "", false
}

func (h *Handlers) requireCreatorOrAdminForSession(w http.ResponseWriter, r *http.Request, sessionID uuid.UUID) (*models.User, *models.Session, bool) {
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return nil, nil, false
	}
	session, err := h.DB.GetSession(r.Context(), sessionID)
	if err != nil || session == nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return nil, nil, false
	}
	isCreator := session.CreatedBy != nil && *session.CreatedBy == user.Email
	isAdmin := user.GlobalRole == models.GlobalRoleAdmin
	if !isCreator && !isAdmin {
		http.Error(w, "Only the session creator or an admin can access orchestration recommendations", http.StatusForbidden)
		return nil, nil, false
	}
	return user, session, true
}

// ListOrchestrationRecommendations handles GET /api/sessions/:id/orchestration/recommendations.
func (h *Handlers) ListOrchestrationRecommendations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sessionID, action, _, ok := parseSessionOrchestrationPath(r.URL.Path)
	if !ok || action != "recommendations" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	if _, _, ok := h.requireCreatorOrAdminForSession(w, r, sessionID); !ok {
		return
	}
	recs, err := h.DB.ListOrchestrationRecommendationsBySessionID(r.Context(), sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list recommendations: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"recommendations": recs})
}

// SyncOrchestrationRecommendations handles POST /api/sessions/:id/orchestration/recommendations/sync.
func (h *Handlers) SyncOrchestrationRecommendations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sessionID, action, tail, ok := parseSessionOrchestrationPath(r.URL.Path)
	if !ok || action != "recommendations" || tail != "sync" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	if _, _, ok := h.requireCreatorOrAdminForSession(w, r, sessionID); !ok {
		return
	}
	ev := orchestration.NewEvaluator(h.DB)
	recs, err := ev.SyncSessionRecommendations(r.Context(), sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to sync recommendations: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"recommendations": recs})
}

// UpdateOrchestrationRecommendationStatus handles PATCH/PUT /api/sessions/:id/orchestration/recommendations/:recommendation_id.
func (h *Handlers) UpdateOrchestrationRecommendationStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch && r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sessionID, action, recIDStr, ok := parseSessionOrchestrationPath(r.URL.Path)
	if !ok || action != "recommendations" || recIDStr == "" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	if _, _, ok := h.requireCreatorOrAdminForSession(w, r, sessionID); !ok {
		return
	}
	recID, err := uuid.Parse(recIDStr)
	if err != nil {
		http.Error(w, "Invalid recommendation ID", http.StatusBadRequest)
		return
	}
	recs, err := h.DB.ListOrchestrationRecommendationsBySessionID(r.Context(), sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list recommendations: %v", err), http.StatusInternalServerError)
		return
	}
	var found *models.OrchestrationRecommendation
	for _, r := range recs {
		if r.ID == recID {
			found = r
			break
		}
	}
	if found == nil {
		http.Error(w, "Recommendation not found in this session", http.StatusNotFound)
		return
	}
	var req struct {
		Status models.OrchestrationRecommendationStatus `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	switch req.Status {
	case models.RecommendationStatusNew,
		models.RecommendationStatusReviewed,
		models.RecommendationStatusApproved,
		models.RecommendationStatusDismissed,
		models.RecommendationStatusCompleted:
	default:
		http.Error(w, "Invalid status", http.StatusBadRequest)
		return
	}
	if err := h.DB.UpdateOrchestrationRecommendationStatus(r.Context(), recID, req.Status); err != nil {
		http.Error(w, fmt.Sprintf("Failed to update recommendation status: %v", err), http.StatusInternalServerError)
		return
	}
	found.Status = req.Status
	writeJSON(w, http.StatusOK, found)
}

