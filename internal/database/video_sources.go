package database

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
)

func (db *DB) CreateVideoSource(ctx context.Context, videoSource *models.VideoSource) error {
	query := `
		INSERT INTO video_sources (id, artifact_id, provider, video_url, transcript_status)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING created_at
	`

	err := db.Pool.QueryRow(ctx, query,
		videoSource.ID,
		videoSource.ArtifactID,
		videoSource.Provider,
		videoSource.VideoURL,
		videoSource.TranscriptStatus,
	).Scan(&videoSource.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to create video source: %w", err)
	}

	return nil
}

func (db *DB) UpdateVideoSourceTranscript(ctx context.Context, videoID uuid.UUID, transcriptText string) error {
	query := `
		UPDATE video_sources
		SET transcript_status = 'ready', transcript_text = $1
		WHERE id = $2
	`

	_, err := db.Pool.Exec(ctx, query, transcriptText, videoID)
	if err != nil {
		return fmt.Errorf("failed to update video source transcript: %w", err)
	}

	return nil
}

func (db *DB) GetVideoSourceByArtifactID(ctx context.Context, artifactID uuid.UUID) (*models.VideoSource, error) {
	videoSource := &models.VideoSource{}
	query := `
		SELECT id, artifact_id, provider, video_url, transcript_status, transcript_text, created_at
		FROM video_sources
		WHERE artifact_id = $1
	`

	err := db.Pool.QueryRow(ctx, query, artifactID).Scan(
		&videoSource.ID,
		&videoSource.ArtifactID,
		&videoSource.Provider,
		&videoSource.VideoURL,
		&videoSource.TranscriptStatus,
		&videoSource.TranscriptText,
		&videoSource.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get video source: %w", err)
	}

	return videoSource, nil
}

func (db *DB) GetVideoSourceByID(ctx context.Context, videoID uuid.UUID) (*models.VideoSource, error) {
	videoSource := &models.VideoSource{}
	query := `
		SELECT id, artifact_id, provider, video_url, transcript_status, transcript_text, created_at
		FROM video_sources
		WHERE id = $1
	`

	err := db.Pool.QueryRow(ctx, query, videoID).Scan(
		&videoSource.ID,
		&videoSource.ArtifactID,
		&videoSource.Provider,
		&videoSource.VideoURL,
		&videoSource.TranscriptStatus,
		&videoSource.TranscriptText,
		&videoSource.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get video source: %w", err)
	}

	return videoSource, nil
}
