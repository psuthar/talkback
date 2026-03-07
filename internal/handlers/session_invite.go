package handlers

import (
	"net/http"
)

// SessionInviteRequest is the body for legacy POST /api/sessions/:id/invite (deprecated).
type SessionInviteRequest struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
}

// SessionInvite is deprecated. Use POST /api/sessions/:id/invitations with email and role instead. Returns 410 Gone.
func (h *Handlers) SessionInvite(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusGone, map[string]string{"error": "use POST /api/sessions/:id/invitations with email and role"})
}
