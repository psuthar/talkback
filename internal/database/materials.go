package database

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
)

func (db *DB) CreateMaterial(ctx context.Context, material *models.Material) error {
	query := `
		INSERT INTO materials (id, artifact_id, kind, filename, content_type, storage_url, text_status, extracted_text)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`

	_, err := db.Pool.Exec(ctx, query,
		material.ID,
		material.ArtifactID,
		material.Kind,
		material.Filename,
		material.ContentType,
		material.StorageURL,
		material.TextStatus,
		material.ExtractedText,
	)
	if err != nil {
		return fmt.Errorf("failed to create material: %w", err)
	}

	return nil
}

func (db *DB) GetMaterialsByArtifactID(ctx context.Context, artifactID uuid.UUID) ([]*models.Material, error) {
	query := `
		SELECT id, artifact_id, kind, filename, content_type, storage_url, text_status, extracted_text, created_at
		FROM materials
		WHERE artifact_id = $1
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
			&m.Kind,
			&m.Filename,
			&m.ContentType,
			&m.StorageURL,
			&m.TextStatus,
			&m.ExtractedText,
			&m.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan material: %w", err)
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

func (db *DB) UpdateMaterialTextStatus(ctx context.Context, materialID uuid.UUID, textStatus models.MaterialTextStatus, extractedText *string) error {
	query := `
		UPDATE materials
		SET text_status = $1, extracted_text = $2
		WHERE id = $3
	`

	_, err := db.Pool.Exec(ctx, query, textStatus, extractedText, materialID)
	if err != nil {
		return fmt.Errorf("failed to update material text status: %w", err)
	}

	return nil
}
