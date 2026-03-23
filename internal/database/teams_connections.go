package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/psuthar/talkback/internal/models"
)

// CreateTeamsConnection inserts or replaces a Teams connection for a creator identity.
func (db *DB) CreateTeamsConnection(ctx context.Context, conn *models.TeamsConnection) error {
	query := `
		INSERT INTO teams_connections (id, creator_identity_id, tenant_id, teams_user_id, teams_user_email, access_token_encrypted, refresh_token_encrypted, expires_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (creator_identity_id) DO UPDATE SET
			tenant_id = EXCLUDED.tenant_id,
			teams_user_id = EXCLUDED.teams_user_id,
			teams_user_email = EXCLUDED.teams_user_email,
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
		conn.TenantID,
		conn.TeamsUserID,
		conn.TeamsUserEmail,
		conn.AccessTokenEncrypted,
		conn.RefreshTokenEncrypted,
		conn.ExpiresAt,
		now,
		now,
	).Scan(&conn.CreatedAt, &conn.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create/update teams connection: %w", err)
	}
	return nil
}

// GetTeamsConnectionByCreatorIdentity returns the Teams connection for a creator identity, or nil.
func (db *DB) GetTeamsConnectionByCreatorIdentity(ctx context.Context, creatorIdentityID string) (*models.TeamsConnection, error) {
	conn := &models.TeamsConnection{}
	query := `
		SELECT id, creator_identity_id, tenant_id, teams_user_id, teams_user_email, access_token_encrypted, refresh_token_encrypted, expires_at, created_at, updated_at
		FROM teams_connections
		WHERE creator_identity_id = $1
	`
	err := db.Pool.QueryRow(ctx, query, creatorIdentityID).Scan(
		&conn.ID,
		&conn.CreatorIdentityID,
		&conn.TenantID,
		&conn.TeamsUserID,
		&conn.TeamsUserEmail,
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
		return nil, fmt.Errorf("failed to get teams connection: %w", err)
	}
	return conn, nil
}

// UpdateTeamsConnectionTokens updates access/refresh tokens and expiry.
func (db *DB) UpdateTeamsConnectionTokens(ctx context.Context, creatorIdentityID string, accessEncrypted, refreshEncrypted []byte, expiresAt time.Time) error {
	query := `
		UPDATE teams_connections
		SET access_token_encrypted = $1, refresh_token_encrypted = $2, expires_at = $3, updated_at = now()
		WHERE creator_identity_id = $4
	`
	result, err := db.Pool.Exec(ctx, query, accessEncrypted, refreshEncrypted, expiresAt, creatorIdentityID)
	if err != nil {
		return fmt.Errorf("failed to update teams connection tokens: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("teams connection not found for creator_identity_id %s", creatorIdentityID)
	}
	return nil
}

// DeleteTeamsConnectionByCreatorIdentity removes the Teams connection for a creator identity.
func (db *DB) DeleteTeamsConnectionByCreatorIdentity(ctx context.Context, creatorIdentityID string) error {
	query := `DELETE FROM teams_connections WHERE creator_identity_id = $1`
	_, err := db.Pool.Exec(ctx, query, creatorIdentityID)
	if err != nil {
		return fmt.Errorf("failed to delete teams connection: %w", err)
	}
	return nil
}
