package database

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// SessionPrimaryHistoryRow is one row of the sessions_primary_history audit
// table written on every successful PATCH primary (SCRUM-283).
//
// PrevID / NewID may be nil for a "clear primary" or initial-set transition.
// PrevKind / NewKind use the closed-set string ("video"/"document"/"link"/"")
// with empty meaning "no explicit primary".
type SessionPrimaryHistoryRow struct {
	ID          uuid.UUID
	SessionID   uuid.UUID
	ActorUserID *uuid.UUID
	PrevKind    string
	PrevID      *uuid.UUID
	NewKind     string
	NewID       *uuid.UUID
}

// InsertSessionPrimaryHistory writes one audit row capturing the
// before/after state of a primary change. Returns the row id (already
// populated on the input). Errors are wrapped so the caller can decide
// whether to fail the request or log-and-continue (handler choice in
// SCRUM-283: log-and-continue, since the broadcast still fires and the
// session row is already updated).
func (db *DB) InsertSessionPrimaryHistory(ctx context.Context, row *SessionPrimaryHistoryRow) error {
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO sessions_primary_history (
			id, session_id, actor_user_id, prev_kind, prev_id, new_kind, new_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, row.ID, row.SessionID, row.ActorUserID, row.PrevKind, row.PrevID, row.NewKind, row.NewID)
	if err != nil {
		return fmt.Errorf("insert sessions_primary_history: %w", err)
	}
	return nil
}

// ListSessionPrimaryHistory returns the audit rows for a session in
// most-recent-first order. Used in tests and (eventually) an admin/debug
// view; the regular GET session response does not include history.
func (db *DB) ListSessionPrimaryHistory(ctx context.Context, sessionID uuid.UUID) ([]*SessionPrimaryHistoryRow, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT id, session_id, actor_user_id, prev_kind, prev_id, new_kind, new_id
		FROM sessions_primary_history
		WHERE session_id = $1
		ORDER BY created_at DESC, id DESC
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list sessions_primary_history: %w", err)
	}
	defer rows.Close()
	var out []*SessionPrimaryHistoryRow
	for rows.Next() {
		r := &SessionPrimaryHistoryRow{}
		if err := rows.Scan(&r.ID, &r.SessionID, &r.ActorUserID, &r.PrevKind, &r.PrevID, &r.NewKind, &r.NewID); err != nil {
			return nil, fmt.Errorf("scan sessions_primary_history: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sessions_primary_history: %w", err)
	}
	return out, nil
}
