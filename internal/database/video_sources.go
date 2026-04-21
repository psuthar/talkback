package database

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
)

func (db *DB) CreateVideoSource(ctx context.Context, videoSource *models.VideoSource) error {
	query := `
		INSERT INTO video_sources (id, artifact_id, session_id, provider, video_url, playback_mode, embed_url, media_url, duration_seconds, poster_url, source_type, stored_video_object_key, original_url, failure_reason, transcript_status, auto_transcribe_enabled, transcription_source, transcription_job_id, video_role)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
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
	var videoRole *string
	if videoSource.VideoRole != nil {
		s := string(*videoSource.VideoRole)
		videoRole = &s
	}

	err := db.Pool.QueryRow(ctx, query,
		videoSource.ID,
		videoSource.ArtifactID,
		videoSource.SessionID,
		videoSource.Provider,
		videoSource.VideoURL,
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
		videoRole,
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

// UpdateVideoSourceZoomTranscript sets transcript text, raw VTT, and segments for a Zoom-sourced video
func (db *DB) UpdateVideoSourceZoomTranscript(ctx context.Context, videoID uuid.UUID, transcriptText string, rawVTT *string, segments []models.TranscriptSegment) error {
	var segmentsJSON []byte
	if len(segments) > 0 {
		var err error
		segmentsJSON, err = json.Marshal(segments)
		if err != nil {
			return fmt.Errorf("failed to marshal transcript segments: %w", err)
		}
	}
	query := `
		UPDATE video_sources
		SET transcript_status = 'ready', transcript_text = $1, raw_vtt = $2, transcript_segments = $3
		WHERE id = $4
	`
	result, err := db.Pool.Exec(ctx, query, transcriptText, rawVTT, segmentsJSON, videoID)
	if err != nil {
		return fmt.Errorf("failed to update video source zoom transcript: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("video source not found")
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
	var segmentsRaw []byte
	query := `
		SELECT id, artifact_id, session_id, provider, video_url, playback_mode, embed_url, media_url, duration_seconds, poster_url, source_type, stored_video_object_key, original_url, failure_reason, transcript_status, transcript_text, auto_transcribe_enabled, transcription_source, transcription_job_id, raw_vtt, transcript_segments, video_role, display_title, created_at
		FROM video_sources
		WHERE artifact_id = $1
	`
	var videoRolePtr *string
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
		&videoSource.RawVTT,
		&segmentsRaw,
		&videoRolePtr,
		&videoSource.DisplayTitle,
		&videoSource.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get video source: %w", err)
	}
	videoSource.SourceType = models.VideoSourceType(sourceTypeStr)
	if videoRolePtr != nil {
		r := models.VideoRole(*videoRolePtr)
		videoSource.VideoRole = &r
	}
	if len(segmentsRaw) > 0 {
		_ = json.Unmarshal(segmentsRaw, &videoSource.TranscriptSegments)
	}
	return videoSource, nil
}

// GetVideoSourcesBySessionID retrieves all video sources for a session
func (db *DB) GetVideoSourcesBySessionID(ctx context.Context, sessionID uuid.UUID) ([]*models.VideoSource, error) {
	query := `
		SELECT id, artifact_id, session_id, provider, video_url, playback_mode, embed_url, media_url, duration_seconds, poster_url, source_type, stored_video_object_key, original_url, failure_reason, transcript_status, transcript_text, auto_transcribe_enabled, transcription_source, transcription_job_id, raw_vtt, transcript_segments, video_role, display_title, created_at
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
		var segmentsRaw []byte
		var videoRolePtr *string
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
			&vs.RawVTT,
			&segmentsRaw,
			&videoRolePtr,
			&vs.DisplayTitle,
			&vs.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan video source: %w", err)
		}
		vs.SourceType = models.VideoSourceType(sourceTypeStr)
		if videoRolePtr != nil {
			r := models.VideoRole(*videoRolePtr)
			vs.VideoRole = &r
		}
		if len(segmentsRaw) > 0 {
			_ = json.Unmarshal(segmentsRaw, &vs.TranscriptSegments)
		}
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
	var segmentsRaw []byte
	var videoRolePtr *string
	query := `
		SELECT id, artifact_id, session_id, provider, video_url, playback_mode, embed_url, media_url, duration_seconds, poster_url, source_type, stored_video_object_key, original_url, failure_reason, transcript_status, transcript_text, auto_transcribe_enabled, transcription_source, transcription_job_id, raw_vtt, transcript_segments, video_role, display_title, created_at
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
		&videoSource.RawVTT,
		&segmentsRaw,
		&videoRolePtr,
		&videoSource.DisplayTitle,
		&videoSource.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get video source: %w", err)
	}
	videoSource.SourceType = models.VideoSourceType(sourceTypeStr)
	if videoRolePtr != nil {
		r := models.VideoRole(*videoRolePtr)
		videoSource.VideoRole = &r
	}
	if len(segmentsRaw) > 0 {
		_ = json.Unmarshal(segmentsRaw, &videoSource.TranscriptSegments)
	}
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

// DeleteVideoSourceByID removes a video source by ID. Used when a material (uploaded video file) is deleted.
func (db *DB) DeleteVideoSourceByID(ctx context.Context, videoSourceID uuid.UUID) error {
	result, err := db.Pool.Exec(ctx, `DELETE FROM video_sources WHERE id = $1`, videoSourceID)
	if err != nil {
		return fmt.Errorf("failed to delete video source: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("video source not found")
	}
	return nil
}

// UpdateVideoSourceDisplayTitle sets a human-readable display title for a video source.
// The video must belong to the given session (ownership check). Pass nil to clear the title.
func (db *DB) UpdateVideoSourceDisplayTitle(ctx context.Context, sessionID, videoSourceID uuid.UUID, title *string) error {
	result, err := db.Pool.Exec(ctx,
		`UPDATE video_sources SET display_title = $1 WHERE id = $2 AND session_id = $3`,
		title, videoSourceID, sessionID,
	)
	if err != nil {
		return fmt.Errorf("failed to update video source display title: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("video source not found or wrong session")
	}
	return nil
}

// SetVideoSourceVideoRole sets the video_role for a video source. When setting primary, any other
// primary in the same session is demoted to secondary. The video must belong to the given session.
func (db *DB) SetVideoSourceVideoRole(ctx context.Context, sessionID, videoSourceID uuid.UUID, role models.VideoRole) error {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if role == models.VideoRolePrimary {
		_, err = tx.Exec(ctx, `UPDATE video_sources SET video_role = 'secondary' WHERE session_id = $1 AND video_role = 'primary'`, sessionID)
		if err != nil {
			return fmt.Errorf("demote existing primary: %w", err)
		}
	}
	roleStr := string(role)
	result, err := tx.Exec(ctx, `UPDATE video_sources SET video_role = $1 WHERE id = $2 AND session_id = $3`, roleStr, videoSourceID, sessionID)
	if err != nil {
		return fmt.Errorf("update video role: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("video source not found or wrong session")
	}
	return tx.Commit(ctx)
}
