package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/psuthar/talkback/internal/models"
)

// ErrMembershipNotFound is returned when no session_memberships row matches.
var ErrMembershipNotFound = errors.New("session membership not found")

// ErrLastCreator is returned when a role change would leave the session with
// zero members in the creator role. Callers should map this to HTTP 409.
var ErrLastCreator = errors.New("session would have no creators after this change")

// SessionMembership represents a user's membership in a session (joined via invite or creator).
type SessionMembership struct {
	ID               uuid.UUID  `json:"id"`
	SessionID        uuid.UUID  `json:"session_id"`
	UserID           uuid.UUID  `json:"user_id"`
	Role             string     `json:"role"` // participant | creator | decision_maker
	InvitedByUserID  *uuid.UUID `json:"invited_by_user_id,omitempty"`
	JoinedAt         string     `json:"joined_at"` // timestamptz
	CreatedAt        string     `json:"created_at"`
	UpdatedAt        string     `json:"updated_at"`
}

// CreateSessionMembership inserts a membership; returns error if (session_id, user_id) already exists.
func (db *DB) CreateSessionMembership(ctx context.Context, sessionID, userID uuid.UUID, role string, invitedByUserID *uuid.UUID) error {
	query := `
		INSERT INTO session_memberships (session_id, user_id, role, invited_by_user_id)
		VALUES ($1, $2, $3, $4)
	`
	_, err := db.Pool.Exec(ctx, query, sessionID, userID, role, invitedByUserID)
	if err != nil {
		return fmt.Errorf("create session membership: %w", err)
	}
	return nil
}

// GetSessionMemberships returns all membership rows for a session, ordered by created_at.
func (db *DB) GetSessionMemberships(ctx context.Context, sessionID uuid.UUID) ([]*SessionMembership, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT id, session_id, user_id, role, invited_by_user_id, joined_at, created_at, updated_at
		FROM session_memberships
		WHERE session_id = $1
		ORDER BY created_at ASC
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("get session memberships: %w", err)
	}
	defer rows.Close()
	var memberships []*SessionMembership
	for rows.Next() {
		m := &SessionMembership{}
		var joinedAt, createdAt, updatedAt time.Time
		if err := rows.Scan(&m.ID, &m.SessionID, &m.UserID, &m.Role, &m.InvitedByUserID, &joinedAt, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan session membership: %w", err)
		}
		m.JoinedAt = joinedAt.Format(time.RFC3339)
		m.CreatedAt = createdAt.Format(time.RFC3339)
		m.UpdatedAt = updatedAt.Format(time.RFC3339)
		memberships = append(memberships, m)
	}
	return memberships, rows.Err()
}

// UserIsSessionMember returns true if the user has a session_memberships row for the session.
func (db *DB) UserIsSessionMember(ctx context.Context, sessionID, userID uuid.UUID) (bool, error) {
	var n int
	err := db.Pool.QueryRow(ctx, `SELECT 1 FROM session_memberships WHERE session_id = $1 AND user_id = $2`, sessionID, userID).Scan(&n)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// CountMembersBySessionIDs returns a map of session_id -> total session_memberships
// rows for that session, for the given session IDs. Sessions with no memberships are
// absent from the result; callers should treat absent as zero. One batched aggregate
// covered by the existing session_memberships(session_id) index. Used by
// ListSessionsWithRolesForUser to populate SessionListRow.MemberCount in one
// round-trip regardless of list size (SCRUM-221).
func (db *DB) CountMembersBySessionIDs(ctx context.Context, sessionIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	out := make(map[uuid.UUID]int, len(sessionIDs))
	if len(sessionIDs) == 0 {
		return out, nil
	}
	rows, err := db.Pool.Query(ctx, `
		SELECT session_id, COUNT(*)::int
		FROM session_memberships
		WHERE session_id = ANY($1)
		GROUP BY session_id
	`, sessionIDs)
	if err != nil {
		return nil, fmt.Errorf("count members by session ids: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var sid uuid.UUID
		var n int
		if err := rows.Scan(&sid, &n); err != nil {
			return nil, fmt.Errorf("scan member count: %w", err)
		}
		out[sid] = n
	}
	return out, rows.Err()
}

// ListSessionIDsForUser returns session IDs where the user is a member (session_memberships).
func (db *DB) ListSessionIDsForUser(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := db.Pool.Query(ctx, `SELECT session_id FROM session_memberships WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// UpdateSessionMembershipRole changes the session-scoped role of an existing
// membership. Wraps the last-creator guardrail and the UPDATE in a single
// transaction so two concurrent demotions cannot both pass the count check.
//
// session_invitations.invited_role is intentionally NOT touched here — that
// column is an immutable snapshot of the role at invitation time. The
// invitations listing endpoint (SCRUM-225) overlays the live membership role
// onto accepted rows when serving the response, so role changes show up in
// the Members panel without a dual-write.
//
// Returns:
//   - ErrMembershipNotFound if no membership row matches (session, user).
//   - ErrLastCreator if the change would leave the session with zero
//     members in the creator role.
//
// Stance rows in decision_stances are NOT mutated by this function — a
// demoted decision_maker keeps any stance they already submitted.
func (db *DB) UpdateSessionMembershipRole(ctx context.Context, sessionID, userID uuid.UUID, newRole string) error {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var currentRole string
	err = tx.QueryRow(ctx, `
		SELECT role FROM session_memberships
		WHERE session_id = $1 AND user_id = $2
		FOR UPDATE
	`, sessionID, userID).Scan(&currentRole)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrMembershipNotFound
		}
		return fmt.Errorf("load membership: %w", err)
	}

	if currentRole == newRole {
		return tx.Commit(ctx)
	}

	// Last-creator guardrail: if we're demoting a creator and no other
	// membership in this session has role='creator', reject.
	if currentRole == "creator" && newRole != "creator" {
		var otherCreators int
		err = tx.QueryRow(ctx, `
			SELECT COUNT(*) FROM session_memberships
			WHERE session_id = $1 AND user_id <> $2 AND role = 'creator'
		`, sessionID, userID).Scan(&otherCreators)
		if err != nil {
			return fmt.Errorf("count other creators: %w", err)
		}
		if otherCreators == 0 {
			return ErrLastCreator
		}
	}

	cmd, err := tx.Exec(ctx, `
		UPDATE session_memberships
		SET role = $1, updated_at = now()
		WHERE session_id = $2 AND user_id = $3
	`, newRole, sessionID, userID)
	if err != nil {
		return fmt.Errorf("update membership role: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return ErrMembershipNotFound
	}

	return tx.Commit(ctx)
}

// GetSessionMembership returns a single membership row, or ErrMembershipNotFound.
func (db *DB) GetSessionMembership(ctx context.Context, sessionID, userID uuid.UUID) (*SessionMembership, error) {
	m := &SessionMembership{}
	var joinedAt, createdAt, updatedAt time.Time
	err := db.Pool.QueryRow(ctx, `
		SELECT id, session_id, user_id, role, invited_by_user_id, joined_at, created_at, updated_at
		FROM session_memberships
		WHERE session_id = $1 AND user_id = $2
	`, sessionID, userID).Scan(&m.ID, &m.SessionID, &m.UserID, &m.Role, &m.InvitedByUserID, &joinedAt, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMembershipNotFound
		}
		return nil, fmt.Errorf("get session membership: %w", err)
	}
	m.JoinedAt = joinedAt.Format(time.RFC3339)
	m.CreatedAt = createdAt.Format(time.RFC3339)
	m.UpdatedAt = updatedAt.Format(time.RFC3339)
	return m, nil
}

// UserCanAccessSession returns true if user is session creator (by email) or has session_memberships row.
func (db *DB) UserCanAccessSession(ctx context.Context, sessionID uuid.UUID, user *models.User) (bool, error) {
	if user == nil {
		return false, nil
	}
	session, err := db.GetSession(ctx, sessionID)
	if err != nil || session == nil {
		return false, err
	}
	if session.CreatedBy != nil && *session.CreatedBy == user.Email {
		return true, nil
	}
	return db.UserIsSessionMember(ctx, sessionID, user.ID)
}
