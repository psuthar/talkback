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

// ListFileArtifactsBySessionID returns all file_artifacts whose session_id
// matches the given session, ordered by created_at. SCRUM-343: CopySession
// uses this to copy every session-scoped file_artifact (not just the primary
// MP4) so video_sources.file_artifact_id remap is complete.
func (db *DB) ListFileArtifactsBySessionID(ctx context.Context, sessionID uuid.UUID) ([]*models.FileArtifact, error) {
	query := `
		SELECT id, session_id, owner_user_id, kind, filename, content_type, size_bytes, sha256,
			storage_provider, storage_bucket, storage_key, status, failure_reason, metadata_json, created_at, updated_at
		FROM file_artifacts
		WHERE session_id = $1
		ORDER BY created_at
	`
	rows, err := db.Pool.Query(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list file_artifacts: %w", err)
	}
	defer rows.Close()
	var out []*models.FileArtifact
	for rows.Next() {
		fa := &models.FileArtifact{}
		var sid, ownerUserID *uuid.UUID
		var filename, sha256, failureReason *string
		var sizeBytes *int64
		var kindStr, statusStr string
		if err := rows.Scan(
			&fa.ID,
			&sid,
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
		); err != nil {
			return nil, fmt.Errorf("scan file_artifact: %w", err)
		}
		fa.Kind = models.FileArtifactKind(kindStr)
		fa.Status = models.FileArtifactStatus(statusStr)
		fa.SessionID = sid
		fa.OwnerUserID = ownerUserID
		fa.Filename = filename
		fa.Sha256 = sha256
		fa.SizeBytes = sizeBytes
		fa.FailureReason = failureReason
		out = append(out, fa)
	}
	return out, rows.Err()
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

// GetFileArtifactsByIDs returns the file_artifacts whose ids are in the
// provided slice (single query). Used by ListSessions to resolve session
// primary descriptors (kind=video) in O(1) extra queries regardless of
// page size. Empty input → empty map; missing ids are silently absent.
func (db *DB) GetFileArtifactsByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*models.FileArtifact, error) {
	out := make(map[uuid.UUID]*models.FileArtifact, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	query := `
		SELECT id, session_id, owner_user_id, kind, filename, content_type, size_bytes, sha256,
			storage_provider, storage_bucket, storage_key, status, failure_reason, metadata_json, created_at, updated_at
		FROM file_artifacts
		WHERE id = ANY($1)
	`
	rows, err := db.Pool.Query(ctx, query, ids)
	if err != nil {
		return nil, fmt.Errorf("query file_artifacts by ids: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		fa := &models.FileArtifact{}
		var sessionID, ownerUserID *uuid.UUID
		var filename, sha256, failureReason *string
		var sizeBytes *int64
		var kindStr, statusStr string
		if err := rows.Scan(
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
		); err != nil {
			return nil, fmt.Errorf("scan file_artifact: %w", err)
		}
		fa.Kind = models.FileArtifactKind(kindStr)
		fa.Status = models.FileArtifactStatus(statusStr)
		fa.SessionID = sessionID
		fa.OwnerUserID = ownerUserID
		fa.Filename = filename
		fa.Sha256 = sha256
		fa.SizeBytes = sizeBytes
		fa.FailureReason = failureReason
		out[fa.ID] = fa
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate file_artifacts by ids: %w", err)
	}
	return out, nil
}

// UpdateFileArtifactToReady sets status=ready and size_bytes (and optionally content_type from head).
func (db *DB) UpdateFileArtifactToReady(ctx context.Context, id uuid.UUID, sizeBytes int64, contentType string) error {
	return db.UpdateFileArtifactToReadyWithMetadata(ctx, id, sizeBytes, contentType, nil)
}

// UpdateFileArtifactToReadyWithMetadata sets status=ready, size_bytes, content_type, and optionally metadata_json (e.g. for etag).
func (db *DB) UpdateFileArtifactToReadyWithMetadata(ctx context.Context, id uuid.UUID, sizeBytes int64, contentType string, metadataJSON []byte) error {
	if metadataJSON != nil {
		query := `
			UPDATE file_artifacts
			SET status = 'ready', size_bytes = $1, content_type = $2, metadata_json = $3, updated_at = now()
			WHERE id = $4
		`
		result, err := db.Pool.Exec(ctx, query, sizeBytes, contentType, metadataJSON, id)
		if err != nil {
			return fmt.Errorf("update file_artifact to ready: %w", err)
		}
		if result.RowsAffected() == 0 {
			return fmt.Errorf("file_artifact not found: %s", id)
		}
		return nil
	}
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

// UpdateFileArtifactToFailed sets status=failed and failure_reason (e.g. zoom_download or r2_put).
func (db *DB) UpdateFileArtifactToFailed(ctx context.Context, id uuid.UUID, failureReason string) error {
	query := `
		UPDATE file_artifacts
		SET status = 'failed', failure_reason = $1, updated_at = now()
		WHERE id = $2
	`
	result, err := db.Pool.Exec(ctx, query, failureReason, id)
	if err != nil {
		return fmt.Errorf("update file_artifact to failed: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("file_artifact not found: %s", id)
	}
	return nil
}

// DeleteFileArtifactsBySessionID deletes all file_artifacts for the session (required before deleting session, since session_id has ON DELETE SET NULL).
func (db *DB) DeleteFileArtifactsBySessionID(ctx context.Context, sessionID uuid.UUID) error {
	_, err := db.Pool.Exec(ctx, `DELETE FROM file_artifacts WHERE session_id = $1`, sessionID)
	if err != nil {
		return fmt.Errorf("delete file_artifacts by session_id: %w", err)
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
