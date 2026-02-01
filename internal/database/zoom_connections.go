package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/psuthar/talkback/internal/models"
)

// CreateZoomConnection inserts or replaces a Zoom connection for a creator identity
func (db *DB) CreateZoomConnection(ctx context.Context, conn *models.ZoomConnection) error {
	query := `
		INSERT INTO zoom_connections (id, creator_identity_id, zoom_user_id, zoom_account_id, zoom_user_email, access_token_encrypted, refresh_token_encrypted, expires_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (creator_identity_id) DO UPDATE SET
			zoom_user_id = EXCLUDED.zoom_user_id,
			zoom_account_id = EXCLUDED.zoom_account_id,
			zoom_user_email = EXCLUDED.zoom_user_email,
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
		conn.ZoomUserID,
		conn.ZoomAccountID,
		conn.ZoomUserEmail,
		conn.AccessTokenEncrypted,
		conn.RefreshTokenEncrypted,
		conn.ExpiresAt,
		now,
		now,
	).Scan(&conn.CreatedAt, &conn.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create/update zoom connection: %w", err)
	}
	return nil
}

// GetZoomConnectionByCreatorIdentity returns the Zoom connection for a creator identity, or nil if not found
func (db *DB) GetZoomConnectionByCreatorIdentity(ctx context.Context, creatorIdentityID string) (*models.ZoomConnection, error) {
	conn := &models.ZoomConnection{}
	query := `
		SELECT id, creator_identity_id, zoom_user_id, zoom_account_id, zoom_user_email, access_token_encrypted, refresh_token_encrypted, expires_at, created_at, updated_at
		FROM zoom_connections
		WHERE creator_identity_id = $1
	`
	err := db.Pool.QueryRow(ctx, query, creatorIdentityID).Scan(
		&conn.ID,
		&conn.CreatorIdentityID,
		&conn.ZoomUserID,
		&conn.ZoomAccountID,
		&conn.ZoomUserEmail,
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
		return nil, fmt.Errorf("failed to get zoom connection: %w", err)
	}
	return conn, nil
}

// UpdateZoomConnectionTokens updates access/refresh tokens and expiry for a connection
func (db *DB) UpdateZoomConnectionTokens(ctx context.Context, creatorIdentityID string, accessEncrypted, refreshEncrypted []byte, expiresAt time.Time) error {
	query := `
		UPDATE zoom_connections
		SET access_token_encrypted = $1, refresh_token_encrypted = $2, expires_at = $3, updated_at = now()
		WHERE creator_identity_id = $4
	`
	result, err := db.Pool.Exec(ctx, query, accessEncrypted, refreshEncrypted, expiresAt, creatorIdentityID)
	if err != nil {
		return fmt.Errorf("failed to update zoom connection tokens: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("zoom connection not found for creator_identity_id %s", creatorIdentityID)
	}
	return nil
}

// DeleteZoomConnectionByCreatorIdentity removes the Zoom connection for a creator identity
func (db *DB) DeleteZoomConnectionByCreatorIdentity(ctx context.Context, creatorIdentityID string) error {
	query := `DELETE FROM zoom_connections WHERE creator_identity_id = $1`
	result, err := db.Pool.Exec(ctx, query, creatorIdentityID)
	if err != nil {
		return fmt.Errorf("failed to delete zoom connection: %w", err)
	}
	if result.RowsAffected() == 0 {
		return nil // idempotent: no row is ok
	}
	return nil
}
