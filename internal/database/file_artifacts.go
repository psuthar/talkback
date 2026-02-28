package database

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/psuthar/talkback/internal/models"
)

// CreateFileArtifact inserts a new file_artifact row (status pending). Returns the created row.
func (db *DB) CreateFileArtifact(ctx context.Context, fa *models.FileArtifact) error {
	query := `
		INSERT INTO file_artifacts (id, session_id, owner_user_id, kind, filename, content_type, size_bytes, sha256,
			storage_provider, storage_bucket, storage_key, status, failure_reason, metadata_json)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING created_at, updated_at
	`
	err := db.Pool.QueryRow(ctx, query,
		fa.ID,
		fa.SessionID,
		fa.OwnerUserID,
		string(fa.Kind),
		fa.Filename,
		fa.ContentType,
		fa.SizeBytes,
		fa.Sha256,
		fa.StorageProvider,
		fa.StorageBucket,
		fa.StorageKey,
		string(fa.Status),
		fa.FailureReason,
		fa.MetadataJSON,
	).Scan(&fa.CreatedAt, &fa.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create file_artifact: %w", err)
	}
	return nil
}

// GetFileArtifactByID returns the file artifact by id, or nil if not found.
func (db *DB) GetFileArtifactByID(ctx context.Context, id uuid.UUID) (*models.FileArtifact, error) {
	fa := &models.FileArtifact{}
	var sessionID, ownerUserID *uuid.UUID
	var filename, sha256, failureReason *string
	var sizeBytes *int64
	var kindStr, statusStr string
	query := `
		SELECT id, session_id, owner_user_id, kind, filename, content_type, size_bytes, sha256,
			storage_provider, storage_bucket, storage_key, status, failure_reason, metadata_json, created_at, updated_at
		FROM file_artifacts
		WHERE id = $1
	`
	err := db.Pool.QueryRow(ctx, query, id).Scan(
		&fa.ID,
		&sessionID,
		&ownerUserID,
		&kindStr,
		&filename,
		&fa.ContentType,
		&sizeBytes,
		&sha256,
		&fa.StorageProvider,
		&fa.StorageBucket,
		&fa.StorageKey,
		&statusStr,
		&failureReason,
		&fa.MetadataJSON,
		&fa.CreatedAt,
		&fa.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get file_artifact: %w", err)
	}
	fa.Kind = models.FileArtifactKind(kindStr)
	fa.Status = models.FileArtifactStatus(statusStr)
	fa.SessionID = sessionID
	fa.OwnerUserID = ownerUserID
	fa.Filename = filename
	fa.Sha256 = sha256
	fa.SizeBytes = sizeBytes
	fa.FailureReason = failureReason
	return fa, nil
}

// UpdateFileArtifactToReady sets status=ready and size_bytes (and optionally content_type from head).
func (db *DB) UpdateFileArtifactToReady(ctx context.Context, id uuid.UUID, sizeBytes int64, contentType string) error {
	query := `
		UPDATE file_artifacts
		SET status = 'ready', size_bytes = $1, content_type = $2, updated_at = now()
		WHERE id = $3
	`
	result, err := db.Pool.Exec(ctx, query, sizeBytes, contentType, id)
	if err != nil {
		return fmt.Errorf("update file_artifact to ready: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("file_artifact not found: %s", id)
	}
	return nil
}

// SetSessionPrimaryVideoArtifact sets sessions.primary_video_artifact_id for the given session.
func (db *DB) SetSessionPrimaryVideoArtifact(ctx context.Context, sessionID uuid.UUID, artifactID *uuid.UUID) error {
	query := `UPDATE sessions SET primary_video_artifact_id = $1, updated_at = now() WHERE id = $2`
	_, err := db.Pool.Exec(ctx, query, artifactID, sessionID)
	if err != nil {
		return fmt.Errorf("set session primary_video_artifact_id: %w", err)
	}
	return nil
}
