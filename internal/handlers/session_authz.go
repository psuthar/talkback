package handlers

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
)

// userIsSessionEditor reports whether the supplied user may edit session
// metadata (premise, primary_decision, decision_outcome, title, status) and
// add/remove session content (links, materials, artifacts, orchestration
// drafts, question approvals).
//
// Editor authority is granted by either:
//   - global admin role, or
//   - per-session role 'creator' in session_memberships.
//
// Original session creators (sessions.created_by email match) qualify
// implicitly because SCRUM-226's transactional CreateSession plus the
// creator-membership backfill migration ensures every original creator has a
// corresponding session_memberships row with role='creator'. Promoted creators
// (via PATCH /api/sessions/:id/memberships/:userId) qualify by the same path.
//
// SCRUM-227 — replaces the per-handler 'session.CreatedBy == user.Email' email
// match that previously gated these operations and which (post-SCRUM-225/226)
// silently denied promoted creators from acting on the session even though
// the Members panel showed them as Creator.
func (h *Handlers) userIsSessionEditor(ctx context.Context, sessionID uuid.UUID, user *models.User) (bool, error) {
	if user == nil {
		return false, nil
	}
	if user.GlobalRole == models.GlobalRoleAdmin {
		return true, nil
	}
	m, err := h.DB.GetSessionMembership(ctx, sessionID, user.ID)
	if err != nil {
		if errors.Is(err, database.ErrMembershipNotFound) {
			return false, nil
		}
		return false, err
	}
	return m != nil && m.Role == "creator", nil
}
