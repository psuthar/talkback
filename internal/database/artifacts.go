package database

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
)

func (db *DB) CreateArtifact(ctx context.Context, sessionID uuid.UUID, title string, description *string) (*models.Artifact, error) {
	artifact := &models.Artifact{
		ID:          uuid.New(),
		SessionID:   sessionID,
		Title:       title,
		Description: description,
		Status:      models.StatusDraft,
	}

	query := `
		INSERT INTO artifacts (id, session_id, title, description, status)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING created_at, updated_at
	`

	err := db.Pool.QueryRow(ctx, query, artifact.ID, artifact.SessionID, artifact.Title, artifact.Description, artifact.Status).
		Scan(&artifact.CreatedAt, &artifact.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to create artifact: %w", err)
	}

	return artifact, nil
}

func (db *DB) GetArtifact(ctx context.Context, id uuid.UUID) (*models.Artifact, error) {
	artifact := &models.Artifact{}
	query := `
		SELECT id, session_id, title, description, status, created_at, updated_at
		FROM artifacts
		WHERE id = $1
	`

	err := db.Pool.QueryRow(ctx, query, id).Scan(
		&artifact.ID,
		&artifact.SessionID,
		&artifact.Title,
		&artifact.Description,
		&artifact.Status,
		&artifact.CreatedAt,
		&artifact.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get artifact: %w", err)
	}

	return artifact, nil
}

func (db *DB) UpdateArtifactStatus(ctx context.Context, id uuid.UUID, status models.ArtifactStatus) error {
	query := `
		UPDATE artifacts
		SET status = $1
		WHERE id = $2
	`

	_, err := db.Pool.Exec(ctx, query, status, id)
	if err != nil {
		return fmt.Errorf("failed to update artifact status: %w", err)
	}

	return nil
}
