package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/psuthar/talkback/internal/models"
)

// CreateGoogleMeetConnection inserts or replaces a Google Meet connection for a creator identity.
func (db *DB) CreateGoogleMeetConnection(ctx context.Context, conn *models.GoogleMeetConnection) error {
	query := `
		INSERT INTO google_meet_connections (id, creator_identity_id, google_user_id, google_user_email, granted_scope, workspace_eligible, access_token_encrypted, refresh_token_encrypted, expires_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (creator_identity_id) DO UPDATE SET
			google_user_id = EXCLUDED.google_user_id,
			google_user_email = EXCLUDED.google_user_email,
			granted_scope = EXCLUDED.granted_scope,
			workspace_eligible = EXCLUDED.workspace_eligible,
			access_token_encrypted = EXCLUDED.access_token_encrypted,
			refresh_token_encrypted = EXCLUDED.refresh_token_encrypted,
			expires_at = EXCLUDED.expires_at,
			updated_at = EXCLUDED.updated_at
		RETURNING created_at, updated_at
	`
	now := time.Now()
	err := db.Pool.QueryRow(ctx, query,
		conn.ID,
		conn.CreatorIdentityID,
		conn.GoogleUserID,
		conn.GoogleUserEmail,
		conn.GrantedScope,
		conn.WorkspaceEligible,
		conn.AccessTokenEncrypted,
		conn.RefreshTokenEncrypted,
		conn.ExpiresAt,
		now,
		now,
	).Scan(&conn.CreatedAt, &conn.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create/update google meet connection: %w", err)
	}
	return nil
}

// GetGoogleMeetConnectionByCreatorIdentity returns the Google Meet connection for a creator identity, or nil.
func (db *DB) GetGoogleMeetConnectionByCreatorIdentity(ctx context.Context, creatorIdentityID string) (*models.GoogleMeetConnection, error) {
	conn := &models.GoogleMeetConnection{}
	query := `
		SELECT id, creator_identity_id, google_user_id, google_user_email, granted_scope, workspace_eligible, access_token_encrypted, refresh_token_encrypted, expires_at, created_at, updated_at
		FROM google_meet_connections
		WHERE creator_identity_id = $1
	`
	err := db.Pool.QueryRow(ctx, query, creatorIdentityID).Scan(
		&conn.ID,
		&conn.CreatorIdentityID,
		&conn.GoogleUserID,
		&conn.GoogleUserEmail,
		&conn.GrantedScope,
		&conn.WorkspaceEligible,
		&conn.AccessTokenEncrypted,
		&conn.RefreshTokenEncrypted,
		&conn.ExpiresAt,
		&conn.CreatedAt,
		&conn.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get google meet connection: %w", err)
	}
	return conn, nil
}

// UpdateGoogleMeetConnectionTokens updates access/refresh tokens and expiry for the connection.
func (db *DB) UpdateGoogleMeetConnectionTokens(ctx context.Context, creatorIdentityID string, accessEncrypted, refreshEncrypted []byte, expiresAt time.Time) error {
	query := `
		UPDATE google_meet_connections
		SET access_token_encrypted = $1, refresh_token_encrypted = $2, expires_at = $3, updated_at = now()
		WHERE creator_identity_id = $4
	`
	result, err := db.Pool.Exec(ctx, query, accessEncrypted, refreshEncrypted, expiresAt, creatorIdentityID)
	if err != nil {
		return fmt.Errorf("failed to update google meet connection tokens: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("google meet connection not found for creator_identity_id %s", creatorIdentityID)
	}
	return nil
}

// DeleteGoogleMeetConnectionByCreatorIdentity removes the Google Meet connection for a creator identity (idempotent).
func (db *DB) DeleteGoogleMeetConnectionByCreatorIdentity(ctx context.Context, creatorIdentityID string) error {
	query := `DELETE FROM google_meet_connections WHERE creator_identity_id = $1`
	_, err := db.Pool.Exec(ctx, query, creatorIdentityID)
	if err != nil {
		return fmt.Errorf("failed to delete google meet connection: %w", err)
	}
	return nil
}
