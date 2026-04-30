package database

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
)

// SessionListRow is one session plus the current user's role for it (GET /api/sessions and MCP list_sessions).
//
// MemberCount is the number of session_memberships rows for this session at fetch
// time. CreatedByDisplayName is the display_name of the user whose email matches
// session.created_by, or nil if no users row matches (legacy email-string field).
// Both fields are populated by the post-switch enrichment in
// ListSessionsWithRolesForUser; see SCRUM-221.
type SessionListRow struct {
	Session              *models.Session `json:"session"`
	MyRole               string          `json:"my_role"` // "creator" | "participant" | "admin"
	MemberCount          int             `json:"member_count"`
	CreatedByDisplayName *string         `json:"created_by_display_name,omitempty"`
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

	// Enrich with member counts and creator display names. Two batched queries
	// regardless of list size; applies uniformly across all three role branches
	// above so admin / participant / creator-default all receive the new fields.
	if len(out) > 0 {
		sessionIDs := make([]uuid.UUID, 0, len(out))
		emailSet := make(map[string]struct{})
		for _, row := range out {
			if row.Session == nil {
				continue
			}
			sessionIDs = append(sessionIDs, row.Session.ID)
			if row.Session.CreatedBy != nil && *row.Session.CreatedBy != "" {
				emailSet[strings.ToLower(*row.Session.CreatedBy)] = struct{}{}
			}
		}
		counts, err := db.CountMembersBySessionIDs(ctx, sessionIDs)
		if err != nil {
			return nil, fmt.Errorf("enrich list: count members: %w", err)
		}
		emails := make([]string, 0, len(emailSet))
		for e := range emailSet {
			emails = append(emails, e)
		}
		names, err := db.GetDisplayNamesByEmails(ctx, emails)
		if err != nil {
			return nil, fmt.Errorf("enrich list: display names: %w", err)
		}
		for i := range out {
			if out[i].Session == nil {
				continue
			}
			out[i].MemberCount = counts[out[i].Session.ID]
			if out[i].Session.CreatedBy != nil && *out[i].Session.CreatedBy != "" {
				if name, ok := names[strings.ToLower(*out[i].Session.CreatedBy)]; ok && name != "" {
					n := name
					out[i].CreatedByDisplayName = &n
				}
			}
		}
	}

	return out, nil
}

// ApplySessionListPagination applies limit/offset when limit > 0 (same rules as GET /api/sessions ?limit=&offset=).
func ApplySessionListPagination(rows []SessionListRow, limit int, offset int) []SessionListRow {
	if limit <= 0 {
		return rows
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(rows) {
		return []SessionListRow{}
	}
	rows = rows[offset:]
	if limit < len(rows) {
		rows = rows[:limit]
	}
	return rows
}
