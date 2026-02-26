package database

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/psuthar/talkback/internal/models"
)

// CreateSessionInvitation inserts an invitation; returns error if (session_id, user_id) already exists.
func (db *DB) CreateSessionInvitation(ctx context.Context, inv *models.SessionInvitation) error {
	query := `
		INSERT INTO session_invitations (id, session_id, user_id, invited_by_user_id, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`
	_, err := db.Pool.Exec(ctx, query, inv.ID, inv.SessionID, inv.UserID, inv.InvitedByUserID, inv.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to create session invitation: %w", err)
	}
	return nil
}

// GetSessionIDsForUser returns session IDs the user is invited to.
func (db *DB) GetSessionIDsForUser(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := db.Pool.Query(ctx, `SELECT session_id FROM session_invitations WHERE user_id = $1`, userID)
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

// InvitationExists returns true if (session_id, user_id) already exists.
func (db *DB) InvitationExists(ctx context.Context, sessionID, userID uuid.UUID) (bool, error) {
	var n int
	err := db.Pool.QueryRow(ctx, `SELECT 1 FROM session_invitations WHERE session_id = $1 AND user_id = $2`, sessionID, userID).Scan(&n)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// ListInvitationsBySession returns invitations for a session (for listing who is invited).
func (db *DB) ListInvitationsBySession(ctx context.Context, sessionID uuid.UUID) ([]*models.SessionInvitation, error) {
	query := `SELECT id, session_id, user_id, invited_by_user_id, created_at FROM session_invitations WHERE session_id = $1 ORDER BY created_at`
	rows, err := db.Pool.Query(ctx, query, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.SessionInvitation
	for rows.Next() {
		inv := &models.SessionInvitation{}
		err := rows.Scan(&inv.ID, &inv.SessionID, &inv.UserID, &inv.InvitedByUserID, &inv.CreatedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// UserInvitedToSession returns true if the user has an invitation to the session.
func (db *DB) UserInvitedToSession(ctx context.Context, sessionID, userID uuid.UUID) (bool, error) {
	return db.InvitationExists(ctx, sessionID, userID)
}

// DeleteAllSessionInvitations deletes all session invitations (for reset).
func (db *DB) DeleteAllSessionInvitations(ctx context.Context) (int, error) {
	res, err := db.Pool.Exec(ctx, `DELETE FROM session_invitations`)
	if err != nil {
		return 0, err
	}
	return int(res.RowsAffected()), nil
}
