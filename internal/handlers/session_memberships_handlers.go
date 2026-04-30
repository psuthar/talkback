package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/invitations"
)

// updateMembershipRequest is the body for PATCH /api/sessions/:id/memberships/:userId.
type updateMembershipRequest struct {
	Role string `json:"role"`
}

// UpdateSessionMembership handles PATCH /api/sessions/:id/memberships/:userId.
// Authorization: creator (by email match on session.created_by) or admin only.
// Body: {"role": "<participant|creator|decision_maker>"}.
//
// Returns:
//   - 200 with the updated membership row.
//   - 400 when the role is missing or invalid.
//   - 403 for non-creator/non-admin callers.
//   - 404 when the path session id is bad or no membership exists.
//   - 409 when the change would leave the session with zero creators.
func (h *Handlers) UpdateSessionMembership(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch && r.Method != http.MethodPut {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	sessionID, userID, err := parseSessionMembershipPath(r.URL.Path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	user := UserFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	ctx := r.Context()
	session, err := h.DB.GetSession(ctx, sessionID)
	if err != nil || session == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}

	// SCRUM-227: editor authority is granted by global admin OR per-session
	// session_memberships.role='creator', mirroring UpdateSessionStatus.
	canEdit, err := h.userIsSessionEditor(ctx, sessionID, user)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to check authorization"})
		return
	}
	if !canEdit {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only the session creator or an admin can change member roles"})
		return
	}

	var req updateMembershipRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	role := invitations.Role(strings.TrimSpace(req.Role))
	if !invitations.IsValidRole(role) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "role must be participant, creator, or decision_maker"})
		return
	}

	if err := h.DB.UpdateSessionMembershipRole(ctx, sessionID, userID, string(role)); err != nil {
		switch {
		case errors.Is(err, database.ErrMembershipNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "membership not found"})
		case errors.Is(err, database.ErrLastCreator):
			writeJSON(w, http.StatusConflict, map[string]string{"error": "session must keep at least one creator"})
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update membership"})
		}
		return
	}

	updated, err := h.DB.GetSessionMembership(ctx, sessionID, userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read updated membership"})
		return
	}

	if h.Hub != nil {
		h.Hub.BroadcastMembershipRoleUpdated(sessionID, userID, updated.Role)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"membership": updated})
}

// parseSessionMembershipPath extracts (sessionID, userID) from a path like
// /api/sessions/<uuid>/memberships/<uuid>.
func parseSessionMembershipPath(path string) (uuid.UUID, uuid.UUID, error) {
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	if len(parts) != 5 || parts[0] != "api" || parts[1] != "sessions" || parts[3] != "memberships" {
		return uuid.Nil, uuid.Nil, errors.New("invalid path")
	}
	sessionID, err := uuid.Parse(parts[2])
	if err != nil {
		return uuid.Nil, uuid.Nil, errors.New("invalid session id")
	}
	userID, err := uuid.Parse(parts[4])
	if err != nil {
		return uuid.Nil, uuid.Nil, errors.New("invalid user id")
	}
	return sessionID, userID, nil
}
