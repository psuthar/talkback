package database

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/psuthar/talkback/internal/models"
)

// CreateLoginSession inserts a new login session and returns it with ID and ExpiresAt set.
func (db *DB) CreateLoginSession(ctx context.Context, session *models.LoginSession) error {
	query := `
		INSERT INTO login_sessions (id, user_id, created_at, expires_at, ip, user_agent)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err := db.Pool.Exec(ctx, query,
		session.ID,
		session.UserID,
		session.CreatedAt,
		session.ExpiresAt,
		session.IP,
		session.UserAgent,
	)
	if err != nil {
		return fmt.Errorf("failed to create login session: %w", err)
	}
	return nil
}

// GetLoginSessionByID returns the session if it exists, not revoked, and not expired; otherwise nil.
func (db *DB) GetLoginSessionByID(ctx context.Context, id uuid.UUID) (*models.LoginSession, error) {
	query := `
		SELECT id, user_id, created_at, expires_at, revoked_at, ip, user_agent
		FROM login_sessions
		WHERE id = $1 AND revoked_at IS NULL AND expires_at > now()
	`
	s := &models.LoginSession{}
	err := db.Pool.QueryRow(ctx, query, id).Scan(
		&s.ID, &s.UserID, &s.CreatedAt, &s.ExpiresAt, &s.RevokedAt, &s.IP, &s.UserAgent,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get login session: %w", err)
	}
	return s, nil
}

// RevokeSessionByID sets revoked_at = now() for the session.
func (db *DB) RevokeSessionByID(ctx context.Context, id uuid.UUID) error {
	_, err := db.Pool.Exec(ctx, `UPDATE login_sessions SET revoked_at = now() WHERE id = $1`, id)
	return err
}
