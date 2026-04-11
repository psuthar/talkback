package database

import (
	"context"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
)

// SessionListRow is one session plus the current user's role for it (GET /api/sessions and MCP list_sessions).
type SessionListRow struct {
	Session *models.Session `json:"session"`
	MyRole  string          `json:"my_role"` // "creator" | "participant" | "admin"
}

// ListSessionsWithRolesForUser returns all sessions the user may access, with my_role per session.
// Rules match HTTP ListSessions: admins see all (my_role "admin"); GlobalRoleParticipant sees only invited;
// creator/default sees created ∪ invited with dedup.
func (db *DB) ListSessionsWithRolesForUser(ctx context.Context, user *models.User) ([]SessionListRow, error) {
	if user == nil {
		return nil, fmt.Errorf("nil user")
	}
	var out []SessionListRow

	switch user.GlobalRole {
	case models.GlobalRoleAdmin:
		all, err := db.ListAllSessions(ctx)
		if err != nil {
			return nil, err
		}
		for _, s := range all {
			out = append(out, SessionListRow{Session: s, MyRole: "admin"})
		}
	case models.GlobalRoleParticipant:
		invited, err := db.ListSessionsForInvitedUser(ctx, user.ID)
		if err != nil {
			return nil, err
		}
		for _, s := range invited {
			out = append(out, SessionListRow{Session: s, MyRole: "participant"})
		}
		sort.Slice(out, func(i, j int) bool {
			return out[i].Session.UpdatedAt.After(out[j].Session.UpdatedAt)
		})
	default:
		created, err := db.ListSessionsByCreatedBy(ctx, user.Email)
		if err != nil {
			return nil, err
		}
		invited, err := db.ListSessionsForInvitedUser(ctx, user.ID)
		if err != nil {
			return nil, err
		}
		roleByID := make(map[uuid.UUID]string)
		sessionByID := make(map[uuid.UUID]*models.Session)
		for _, s := range created {
			roleByID[s.ID] = "creator"
			sessionByID[s.ID] = s
		}
		for _, s := range invited {
			if _, exists := roleByID[s.ID]; !exists {
				roleByID[s.ID] = "participant"
				sessionByID[s.ID] = s
			}
		}
		for id, s := range sessionByID {
			out = append(out, SessionListRow{Session: s, MyRole: roleByID[id]})
		}
		sort.Slice(out, func(i, j int) bool {
			return out[i].Session.UpdatedAt.After(out[j].Session.UpdatedAt)
		})
	}

	if out == nil {
		out = []SessionListRow{}
	}
	return out, nil
}
