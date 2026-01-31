package database

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
)

func (db *DB) CreateVideoSource(ctx context.Context, videoSource *models.VideoSource) error {
	query := `
		INSERT INTO video_sources (id, artifact_id, session_id, provider, video_url, playback_mode, embed_url, media_url, duration_seconds, poster_url, source_type, stored_video_object_key, original_url, failure_reason, transcript_status, auto_transcribe_enabled, transcription_source, transcription_job_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		RETURNING created_at
	`

	// Set defaults if not provided
	playbackMode := videoSource.PlaybackMode
	if playbackMode == "" {
		playbackMode = "embed"
	}
	
	sourceType := string(videoSource.SourceType)
	if sourceType == "" {
		sourceType = "embed_url" // Default for backward compatibility
	}

	err := db.Pool.QueryRow(ctx, query,
		videoSource.ID,
		videoSource.ArtifactID,
		videoSource.SessionID,
		videoSource.Provider,
		videoSource.VideoURL, // Keep for backward compatibility
		playbackMode,
		videoSource.EmbedURL,
		videoSource.MediaURL,
		videoSource.DurationSeconds,
		videoSource.PosterURL,
		sourceType,
		videoSource.StoredVideoObjectKey,
		videoSource.OriginalURL,
		videoSource.FailureReason,
		videoSource.TranscriptStatus,
		videoSource.AutoTranscribeEnabled,
		videoSource.TranscriptionSource,
		videoSource.TranscriptionJobID,
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

	result, err := db.Pool.Exec(ctx, query, transcriptText, videoID)
	if err != nil {
		return fmt.Errorf("failed to update video source transcript: %w", err)
	}

	// Check if any rows were updated
	if result.RowsAffected() == 0 {
		return fmt.Errorf("failed to update video source transcript: video source not found")
	}

	return nil
}

func (db *DB) UpdateVideoSourceTranscriptStatus(ctx context.Context, videoID uuid.UUID, status models.VideoTranscriptStatus) error {
	query := `
		UPDATE video_sources
		SET transcript_status = $1
		WHERE id = $2
	`

	result, err := db.Pool.Exec(ctx, query, status, videoID)
	if err != nil {
		return fmt.Errorf("failed to update video source transcript status: %w", err)
	}

	// Check if any rows were updated
	if result.RowsAffected() == 0 {
		return fmt.Errorf("failed to update video source transcript status: video source not found")
	}

	return nil
}

func (db *DB) GetVideoSourceByArtifactID(ctx context.Context, artifactID uuid.UUID) (*models.VideoSource, error) {
	videoSource := &models.VideoSource{}
	var sourceTypeStr string
	query := `
		SELECT id, artifact_id, session_id, provider, video_url, playback_mode, embed_url, media_url, duration_seconds, poster_url, source_type, stored_video_object_key, original_url, failure_reason, transcript_status, transcript_text, auto_transcribe_enabled, transcription_source, transcription_job_id, created_at
		FROM video_sources
		WHERE artifact_id = $1
	`

	err := db.Pool.QueryRow(ctx, query, artifactID).Scan(
		&videoSource.ID,
		&videoSource.ArtifactID,
		&videoSource.SessionID,
		&videoSource.Provider,
		&videoSource.VideoURL,
		&videoSource.PlaybackMode,
		&videoSource.EmbedURL,
		&videoSource.MediaURL,
		&videoSource.DurationSeconds,
		&videoSource.PosterURL,
		&sourceTypeStr,
		&videoSource.StoredVideoObjectKey,
		&videoSource.OriginalURL,
		&videoSource.FailureReason,
		&videoSource.TranscriptStatus,
		&videoSource.TranscriptText,
		&videoSource.AutoTranscribeEnabled,
		&videoSource.TranscriptionSource,
		&videoSource.TranscriptionJobID,
		&videoSource.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get video source: %w", err)
	}
	
	videoSource.SourceType = models.VideoSourceType(sourceTypeStr)

	return videoSource, nil
}

// GetVideoSourcesBySessionID retrieves all video sources for a session
func (db *DB) GetVideoSourcesBySessionID(ctx context.Context, sessionID uuid.UUID) ([]*models.VideoSource, error) {
	query := `
		SELECT id, artifact_id, session_id, provider, video_url, playback_mode, embed_url, media_url, duration_seconds, poster_url, source_type, stored_video_object_key, original_url, failure_reason, transcript_status, transcript_text, auto_transcribe_enabled, transcription_source, transcription_job_id, created_at
		FROM video_sources
		WHERE session_id = $1
		ORDER BY created_at
	`

	rows, err := db.Pool.Query(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query video sources: %w", err)
	}
	defer rows.Close()

	var videoSources []*models.VideoSource
	for rows.Next() {
		vs := &models.VideoSource{}
		var sourceTypeStr string
		err := rows.Scan(
			&vs.ID,
			&vs.ArtifactID,
			&vs.SessionID,
			&vs.Provider,
			&vs.VideoURL,
			&vs.PlaybackMode,
			&vs.EmbedURL,
			&vs.MediaURL,
			&vs.DurationSeconds,
			&vs.PosterURL,
			&sourceTypeStr,
			&vs.StoredVideoObjectKey,
			&vs.OriginalURL,
			&vs.FailureReason,
			&vs.TranscriptStatus,
			&vs.TranscriptText,
			&vs.AutoTranscribeEnabled,
			&vs.TranscriptionSource,
			&vs.TranscriptionJobID,
			&vs.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan video source: %w", err)
		}
		vs.SourceType = models.VideoSourceType(sourceTypeStr)
		videoSources = append(videoSources, vs)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating video sources: %w", err)
	}

	if videoSources == nil {
		videoSources = []*models.VideoSource{}
	}

	return videoSources, nil
}

func (db *DB) GetVideoSourceByID(ctx context.Context, videoID uuid.UUID) (*models.VideoSource, error) {
	videoSource := &models.VideoSource{}
	var sourceTypeStr string
	query := `
		SELECT id, artifact_id, session_id, provider, video_url, playback_mode, embed_url, media_url, duration_seconds, poster_url, source_type, stored_video_object_key, original_url, failure_reason, transcript_status, transcript_text, auto_transcribe_enabled, transcription_source, transcription_job_id, created_at
		FROM video_sources
		WHERE id = $1
	`

	err := db.Pool.QueryRow(ctx, query, videoID).Scan(
		&videoSource.ID,
		&videoSource.ArtifactID,
		&videoSource.SessionID,
		&videoSource.Provider,
		&videoSource.VideoURL,
		&videoSource.PlaybackMode,
		&videoSource.EmbedURL,
		&videoSource.MediaURL,
		&videoSource.DurationSeconds,
		&videoSource.PosterURL,
		&sourceTypeStr,
		&videoSource.StoredVideoObjectKey,
		&videoSource.OriginalURL,
		&videoSource.FailureReason,
		&videoSource.TranscriptStatus,
		&videoSource.TranscriptText,
		&videoSource.AutoTranscribeEnabled,
		&videoSource.TranscriptionSource,
		&videoSource.TranscriptionJobID,
		&videoSource.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get video source: %w", err)
	}
	
	videoSource.SourceType = models.VideoSourceType(sourceTypeStr)

	return videoSource, nil
}

// UpdateVideoSourceIngestionStatus updates the transcript status and failure reason
func (db *DB) UpdateVideoSourceIngestionStatus(ctx context.Context, videoID uuid.UUID, status models.VideoTranscriptStatus, failureReason *string) error {
	query := `
		UPDATE video_sources
		SET transcript_status = $1, failure_reason = $2
		WHERE id = $3
	`

	result, err := db.Pool.Exec(ctx, query, status, failureReason, videoID)
	if err != nil {
		return fmt.Errorf("failed to update video source ingestion status: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("failed to update video source ingestion status: video source not found")
	}

	return nil
}

// UpdateVideoSourceStoredFile updates the stored video object key
func (db *DB) UpdateVideoSourceStoredFile(ctx context.Context, videoID uuid.UUID, objectKey string) error {
	query := `
		UPDATE video_sources
		SET stored_video_object_key = $1
		WHERE id = $2
	`

	result, err := db.Pool.Exec(ctx, query, objectKey, videoID)
	if err != nil {
		return fmt.Errorf("failed to update video source stored file: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("failed to update video source stored file: video source not found")
	}

	return nil
}
