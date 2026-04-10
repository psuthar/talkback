package database

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
)

// MaxMCPAccessibleSessions caps how many session IDs are considered for MCP cross-session tools (SCRUM-63).
// Prevents unbounded work for global admins and large tenants. Scope rules: docs/cross-session-intelligence.md (SCRUM-65).
const MaxMCPAccessibleSessions = 5000

// ListAccessibleSessionIDsForUser returns session IDs the user may read (same rules as MCP/HTTP:
// global admin → all sessions, up to MaxMCPAccessibleSessions by recency; otherwise creator email
// union session_memberships). Ordered by updated_at DESC when possible.
func (db *DB) ListAccessibleSessionIDsForUser(ctx context.Context, u *models.User) ([]uuid.UUID, error) {
	if u == nil {
		return nil, fmt.Errorf("nil user")
	}
	if u.GlobalRole == models.GlobalRoleAdmin {
		rows, err := db.Pool.Query(ctx, `
			SELECT id FROM sessions
			ORDER BY updated_at DESC
			LIMIT $1
		`, MaxMCPAccessibleSessions)
		if err != nil {
			return nil, fmt.Errorf("list sessions for admin: %w", err)
		}
		defer rows.Close()
		return scanUUIDRows(rows)
	}
	email := ""
	if u.Email != "" {
		email = u.Email
	}
	rows, err := db.Pool.Query(ctx, `
		SELECT s.id
		FROM sessions s
		WHERE s.id IN (
			SELECT id FROM sessions WHERE created_by = $1
			UNION
			SELECT session_id FROM session_memberships WHERE user_id = $2
		)
		ORDER BY s.updated_at DESC
		LIMIT $3
	`, email, u.ID, MaxMCPAccessibleSessions)
	if err != nil {
		return nil, fmt.Errorf("list accessible sessions: %w", err)
	}
	defer rows.Close()
	return scanUUIDRows(rows)
}

func scanUUIDRows(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]uuid.UUID, error) {
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SessionMetadataMin is title and updated_at for MCP cross-session hit payloads (SCRUM-63).
type SessionMetadataMin struct {
	Title     string
	UpdatedAt time.Time
}

// ListSessionMetadataByIDs returns metadata for the given session IDs (missing rows omitted).
func (db *DB) ListSessionMetadataByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]SessionMetadataMin, error) {
	if len(ids) == 0 {
		return map[uuid.UUID]SessionMetadataMin{}, nil
	}
	rows, err := db.Pool.Query(ctx, `
		SELECT id, title, updated_at FROM sessions WHERE id = ANY($1::uuid[])
	`, ids)
	if err != nil {
		return nil, fmt.Errorf("list session metadata by ids: %w", err)
	}
	defer rows.Close()
	out := make(map[uuid.UUID]SessionMetadataMin)
	for rows.Next() {
		var id uuid.UUID
		var title string
		var updatedAt time.Time
		if err := rows.Scan(&id, &title, &updatedAt); err != nil {
			return nil, err
		}
		out[id] = SessionMetadataMin{Title: title, UpdatedAt: updatedAt}
	}
	return out, rows.Err()
}
