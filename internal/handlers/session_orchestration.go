package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/database"
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
	canEdit, err := h.userIsSessionEditor(r.Context(), sessionID, user)
	if err != nil {
		log.Printf("requireCreatorOrAdminForSession userIsSessionEditor: %v", err)
		http.Error(w, "Failed to check authorization", http.StatusInternalServerError)
		return nil, nil, false
	}
	if !canEdit {
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

// ListOrchestrationRecommendationStatusAudit handles GET /api/sessions/:id/orchestration/recommendations/audit.
func (h *Handlers) ListOrchestrationRecommendationStatusAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sessionID, action, tail, ok := parseSessionOrchestrationPath(r.URL.Path)
	if !ok || action != "recommendations" || tail != "audit" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	if _, _, ok := h.requireCreatorOrAdminForSession(w, r, sessionID); !ok {
		return
	}
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 500 {
			http.Error(w, "Invalid limit", http.StatusBadRequest)
			return
		}
		limit = n
	}
	rows, err := h.DB.ListOrchestrationRecommendationStatusAuditBySessionID(r.Context(), sessionID, limit)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list recommendation status audit: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"status_audit": rows})
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
	user, _, ok := h.requireCreatorOrAdminForSession(w, r, sessionID)
	if !ok {
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
		Status        models.OrchestrationRecommendationStatus `json:"status"`
		ClientSurface *string                                  `json:"client_surface,omitempty"`
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
	if err := h.DB.UpdateOrchestrationRecommendationStatusWithAudit(r.Context(), sessionID, recID, req.Status, user.ID, req.ClientSurface); err != nil {
		if errors.Is(err, database.ErrOrchestrationRecommendationNotFound) {
			http.Error(w, "Recommendation not found in this session", http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("Failed to update recommendation status: %v", err), http.StatusInternalServerError)
		return
	}
	found.Status = req.Status
	writeJSON(w, http.StatusOK, found)
}
