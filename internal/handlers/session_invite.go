package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
)

// SessionInviteRequest is the body for POST /api/sessions/:id/invite.
type SessionInviteRequest struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
}

// SessionInvite handles POST /api/sessions/:id/invite (RequireAuth). Inviter must be session creator or an invited participant.
func (h *Handlers) SessionInvite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	// /api/sessions/:id/invite -> parts [api, sessions, id, invite]
	if len(parts) < 4 || parts[0] != "api" || parts[1] != "sessions" || parts[3] != "invite" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	sessionID, err := uuid.Parse(parts[2])
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid session id"})
		return
	}
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var req SessionInviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	var inviteeUserID uuid.UUID
	if req.UserID != "" {
		inviteeUserID, err = uuid.Parse(req.UserID)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid user_id"})
			return
		}
	} else if strings.TrimSpace(req.Email) != "" {
		invitee, _ := h.DB.GetUserByEmail(r.Context(), strings.TrimSpace(strings.ToLower(req.Email)))
		if invitee == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
			return
		}
		inviteeUserID = invitee.ID
	} else {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user_id or email required"})
		return
	}
	if inviteeUserID == user.ID {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot invite yourself"})
		return
	}
	ctx := r.Context()
	session, err := h.DB.GetSession(ctx, sessionID)
	if err != nil || session == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	// Inviter must be creator (created_by matches current user email) or an invited participant
	isCreator := session.CreatedBy != nil && *session.CreatedBy == user.Email
	invited, _ := h.DB.UserInvitedToSession(ctx, sessionID, user.ID)
	if !isCreator && !invited {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only session creator or an invited participant can invite"})
		return
	}
	exists, _ := h.DB.InvitationExists(ctx, sessionID, inviteeUserID)
	if exists {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "user already invited"})
		return
	}
	inv := &models.SessionInvitation{
		ID:               uuid.New(),
		SessionID:        sessionID,
		UserID:           inviteeUserID,
		InvitedByUserID:  &user.ID,
		CreatedAt:        time.Now(),
	}
	if err := h.DB.CreateSessionInvitation(ctx, inv); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create invitation"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": inv.ID.String(), "session_id": sessionID.String(), "user_id": inviteeUserID.String()})
}
