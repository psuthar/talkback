package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
)

func sanitizeTextForPostgres(s string) string {
	// Postgres TEXT does not allow NUL bytes; also guard against invalid UTF-8.
	s = strings.ToValidUTF8(s, "")
	if strings.IndexByte(s, 0) == -1 {
		return s
	}
	return strings.Map(func(r rune) rune {
		if r == 0 {
			return -1
		}
		return r
	}, s)
}

func (db *DB) CreateMaterial(ctx context.Context, material *models.Material) error {
	query := `
		INSERT INTO materials (id, artifact_id, session_id, kind, filename, content_type, storage_url, storage_provider, storage_key, size_bytes, text_status, extracted_text, title, error_message, deleted_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`
	storageProvider := material.StorageProvider
	if storageProvider == "" {
		storageProvider = "local"
	}
	if material.ExtractedText != nil {
		t := sanitizeTextForPostgres(*material.ExtractedText)
		material.ExtractedText = &t
	}
	if material.Title != nil {
		t := sanitizeTextForPostgres(*material.Title)
		material.Title = &t
	}
	if material.ErrorMessage != nil {
		t := sanitizeTextForPostgres(*material.ErrorMessage)
		material.ErrorMessage = &t
	}
	_, err := db.Pool.Exec(ctx, query,
		material.ID,
		material.ArtifactID,
		material.SessionID,
		material.Kind,
		material.Filename,
		material.ContentType,
		material.StorageURL,
		storageProvider,
		material.StorageKey,
		material.SizeBytes,
		material.TextStatus,
		material.ExtractedText,
		material.Title,
		material.ErrorMessage,
		material.DeletedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create material: %w", err)
	}

	return nil
}

func (db *DB) GetMaterialByID(ctx context.Context, materialID uuid.UUID) (*models.Material, error) {
	query := `
		SELECT id, artifact_id, session_id, kind, filename, content_type, storage_url, COALESCE(storage_provider, 'local'), storage_key, size_bytes, text_status, extracted_text, title, error_message, created_at, deleted_at
		FROM materials
		WHERE id = $1
	`
	m := &models.Material{}
	var storageKeyNull sql.NullString
	err := db.Pool.QueryRow(ctx, query, materialID).Scan(
		&m.ID,
		&m.ArtifactID,
		&m.SessionID,
		&m.Kind,
		&m.Filename,
		&m.ContentType,
		&m.StorageURL,
		&m.StorageProvider,
		&storageKeyNull,
		&m.SizeBytes,
		&m.TextStatus,
		&m.ExtractedText,
		&m.Title,
		&m.ErrorMessage,
		&m.CreatedAt,
		&m.DeletedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get material: %w", err)
	}
	m.StorageKey = storageKeyNull.String
	if m.StorageProvider == "" {
		m.StorageProvider = "local"
	}
	return m, nil
}

func (db *DB) GetMaterialsByArtifactID(ctx context.Context, artifactID uuid.UUID) ([]*models.Material, error) {
	query := `
		SELECT id, artifact_id, session_id, kind, filename, content_type, storage_url, COALESCE(storage_provider, 'local'), storage_key, size_bytes, text_status, extracted_text, title, error_message, created_at, deleted_at
		FROM materials
		WHERE artifact_id = $1 AND (deleted_at IS NULL)
		ORDER BY created_at
	`

	rows, err := db.Pool.Query(ctx, query, artifactID)
	if err != nil {
		return nil, fmt.Errorf("failed to query materials: %w", err)
	}
	defer rows.Close()

	var materials []*models.Material
	for rows.Next() {
		m := &models.Material{}
		err := rows.Scan(
			&m.ID,
			&m.ArtifactID,
			&m.SessionID,
			&m.Kind,
			&m.Filename,
			&m.ContentType,
			&m.StorageURL,
			&m.StorageProvider,
			&m.StorageKey,
			&m.SizeBytes,
			&m.TextStatus,
			&m.ExtractedText,
			&m.Title,
			&m.ErrorMessage,
			&m.CreatedAt,
			&m.DeletedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan material: %w", err)
		}
		if m.StorageProvider == "" {
			m.StorageProvider = "local"
		}
		materials = append(materials, m)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating materials: %w", err)
	}

	// Always return a non-nil slice, even if empty
	if materials == nil {
		materials = []*models.Material{}
	}

	return materials, nil
}

// GetMaterialsBySessionID retrieves all materials for a session (including soft-deleted tombstones).
func (db *DB) GetMaterialsBySessionID(ctx context.Context, sessionID uuid.UUID) ([]*models.Material, error) {
	query := `
		SELECT id, artifact_id, session_id, kind, filename, content_type, storage_url, COALESCE(storage_provider, 'local'), storage_key, size_bytes, text_status, extracted_text, title, error_message, created_at, deleted_at
		FROM materials
		WHERE session_id = $1
		ORDER BY created_at
	`

	rows, err := db.Pool.Query(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query materials: %w", err)
	}
	defer rows.Close()

	var materials []*models.Material
	for rows.Next() {
		m := &models.Material{}
		var storageKeyNull sql.NullString
		err := rows.Scan(
			&m.ID,
			&m.ArtifactID,
			&m.SessionID,
			&m.Kind,
			&m.Filename,
			&m.ContentType,
			&m.StorageURL,
			&m.StorageProvider,
			&storageKeyNull,
			&m.SizeBytes,
			&m.TextStatus,
			&m.ExtractedText,
			&m.Title,
			&m.ErrorMessage,
			&m.CreatedAt,
			&m.DeletedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan material: %w", err)
		}
		m.StorageKey = storageKeyNull.String
		if m.StorageProvider == "" {
			m.StorageProvider = "local"
		}
		materials = append(materials, m)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating materials: %w", err)
	}

	// Always return a non-nil slice, even if empty
	if materials == nil {
		materials = []*models.Material{}
	}

	return materials, nil
}

// GetActiveMaterialsBySessionID returns materials for a session with deleted_at IS NULL (for list UI, RAG, session_ask).
func (db *DB) GetActiveMaterialsBySessionID(ctx context.Context, sessionID uuid.UUID) ([]*models.Material, error) {
	query := `
		SELECT id, artifact_id, session_id, kind, filename, content_type, storage_url, COALESCE(storage_provider, 'local'), storage_key, size_bytes, text_status, extracted_text, title, error_message, created_at, deleted_at
		FROM materials
		WHERE session_id = $1 AND (deleted_at IS NULL)
		ORDER BY created_at
	`

	rows, err := db.Pool.Query(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query materials: %w", err)
	}
	defer rows.Close()

	var materials []*models.Material
	for rows.Next() {
		m := &models.Material{}
		var storageKeyNull sql.NullString
		err := rows.Scan(
			&m.ID,
			&m.ArtifactID,
			&m.SessionID,
			&m.Kind,
			&m.Filename,
			&m.ContentType,
			&m.StorageURL,
			&m.StorageProvider,
			&storageKeyNull,
			&m.SizeBytes,
			&m.TextStatus,
			&m.ExtractedText,
			&m.Title,
			&m.ErrorMessage,
			&m.CreatedAt,
			&m.DeletedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan material: %w", err)
		}
		m.StorageKey = storageKeyNull.String
		if m.StorageProvider == "" {
			m.StorageProvider = "local"
		}
		materials = append(materials, m)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating materials: %w", err)
	}

	if materials == nil {
		materials = []*models.Material{}
	}

	return materials, nil
}

// ExistsMaterialWithFilenameInSession returns true if the session already has an active material with the same filename (case-insensitive).
func (db *DB) ExistsMaterialWithFilenameInSession(ctx context.Context, sessionID uuid.UUID, filename string) (bool, error) {
	var exists bool
	err := db.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM materials WHERE session_id = $1 AND LOWER(filename) = LOWER($2) AND (deleted_at IS NULL))`,
		sessionID, filename,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check duplicate filename: %w", err)
	}
	return exists, nil
}

// GetMaterialBySessionAndStorage finds an active material in the session whose storage_url or storage_key matches the given key (e.g. from VideoSource.StoredVideoObjectKey). Used to link video transcript back to the material.
func (db *DB) GetMaterialBySessionAndStorage(ctx context.Context, sessionID uuid.UUID, storageKey string) (*models.Material, error) {
	if storageKey == "" {
		return nil, nil
	}
	query := `
		SELECT id, artifact_id, session_id, kind, filename, content_type, storage_url, COALESCE(storage_provider, 'local'), storage_key, size_bytes, text_status, extracted_text, title, error_message, created_at, deleted_at
		FROM materials
		WHERE session_id = $1 AND (deleted_at IS NULL)
		  AND (storage_url = $2 OR storage_key = $2 OR REPLACE(storage_url, E'\\', '/') = REPLACE($2, E'\\', '/') OR REPLACE(COALESCE(storage_key, ''), E'\\', '/') = REPLACE($2, E'\\', '/'))
		LIMIT 1
	`
	m := &models.Material{}
	var storageKeyNull sql.NullString
	err := db.Pool.QueryRow(ctx, query, sessionID, storageKey).Scan(
		&m.ID,
		&m.ArtifactID,
		&m.SessionID,
		&m.Kind,
		&m.Filename,
		&m.ContentType,
		&m.StorageURL,
		&m.StorageProvider,
		&storageKeyNull,
		&m.SizeBytes,
		&m.TextStatus,
		&m.ExtractedText,
		&m.Title,
		&m.ErrorMessage,
		&m.CreatedAt,
		&m.DeletedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get material by session and storage: %w", err)
	}
	m.StorageKey = storageKeyNull.String
	if m.StorageProvider == "" {
		m.StorageProvider = "local"
	}
	return m, nil
}

func (db *DB) UpdateMaterialTextStatus(ctx context.Context, materialID uuid.UUID, textStatus models.MaterialTextStatus, extractedText *string) error {
	return db.UpdateMaterialTextStatusWithError(ctx, materialID, textStatus, extractedText, nil)
}

// UpdateMaterialTextStatusWithError updates status, extracted text, and optional error message
func (db *DB) UpdateMaterialTextStatusWithError(ctx context.Context, materialID uuid.UUID, textStatus models.MaterialTextStatus, extractedText *string, errMsg *string) error {
	query := `
		UPDATE materials
		SET text_status = $1, extracted_text = $2, error_message = $3
		WHERE id = $4
	`
	_, err := db.Pool.Exec(ctx, query, textStatus, extractedText, errMsg, materialID)
	if err != nil {
		return fmt.Errorf("failed to update material text status: %w", err)
	}
	return nil
}

// SoftDeleteMaterial marks a material as deleted (tombstone): sets deleted_at and clears storage_key. File must be removed from R2/local by caller first.
func (db *DB) SoftDeleteMaterial(ctx context.Context, materialID uuid.UUID) error {
	_, err := db.Pool.Exec(ctx, `UPDATE materials SET deleted_at = now(), storage_key = NULL WHERE id = $1`, materialID)
	if err != nil {
		return fmt.Errorf("soft delete material: %w", err)
	}
	return nil
}

// DeleteMaterial deletes a material by ID. Caller should delete session chunks for this material first (or use DeleteMaterialAndChunks).
func (db *DB) DeleteMaterial(ctx context.Context, materialID uuid.UUID) error {
	_, err := db.Pool.Exec(ctx, `DELETE FROM materials WHERE id = $1`, materialID)
	if err != nil {
		return fmt.Errorf("delete material: %w", err)
	}
	return nil
}
