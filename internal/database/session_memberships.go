package database

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/psuthar/talkback/internal/models"
)

// SessionMembership represents a user's membership in a session (joined via invite or creator).
type SessionMembership struct {
	ID               uuid.UUID  `json:"id"`
	SessionID        uuid.UUID  `json:"session_id"`
	UserID           uuid.UUID  `json:"user_id"`
	Role             string     `json:"role"` // participant | creator
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
