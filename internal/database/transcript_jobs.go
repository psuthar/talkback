package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/psuthar/talkback/internal/models"
)

// CreateTranscriptJob creates a new transcript job record
func (db *DB) CreateTranscriptJob(ctx context.Context, job *models.TranscriptJob) error {
	query := `
		INSERT INTO transcript_jobs (
			id, video_source_id, session_id, status, source_url, job_key,
			error_message, resolved_media_url, queued_at, started_at, completed_at,
			whisper_model, detected_language, duration_seconds, loom_password
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING queued_at
	`
	
	var startedAt, completedAt *time.Time
	if job.StartedAt != nil {
		startedAt = job.StartedAt
	}
	if job.CompletedAt != nil {
		completedAt = job.CompletedAt
	}

	err := db.Pool.QueryRow(ctx, query,
		job.ID,
		job.VideoSourceID,
		job.SessionID,
		job.Status,
		job.SourceURL,
		job.JobKey,
		job.ErrorMessage,
		job.ResolvedMediaURL,
		job.QueuedAt,
		startedAt,
		completedAt,
		job.WhisperModel,
		job.DetectedLanguage,
		job.DurationSeconds,
		job.LoomPassword,
	).Scan(&job.QueuedAt)
	
	if err != nil {
		return fmt.Errorf("failed to create transcript job: %w", err)
	}
	return nil
}

// GetTranscriptJob retrieves a transcript job by ID
func (db *DB) GetTranscriptJob(ctx context.Context, jobID uuid.UUID) (*models.TranscriptJob, error) {
	job := &models.TranscriptJob{}
	query := `
		SELECT 
			id, video_source_id, session_id, status, error_message, source_url,
			resolved_media_url, queued_at, started_at, completed_at,
			whisper_model, detected_language, duration_seconds, job_key, loom_password
		FROM transcript_jobs
		WHERE id = $1
	`
	
	var startedAt, completedAt sql.NullTime
	var loomPassword sql.NullString
	err := db.Pool.QueryRow(ctx, query, jobID).Scan(
		&job.ID,
		&job.VideoSourceID,
		&job.SessionID,
		&job.Status,
		&job.ErrorMessage,
		&job.SourceURL,
		&job.ResolvedMediaURL,
		&job.QueuedAt,
		&startedAt,
		&completedAt,
		&job.WhisperModel,
		&job.DetectedLanguage,
		&job.DurationSeconds,
		&job.JobKey,
		&loomPassword,
	)
	
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("transcript job not found")
		}
		return nil, fmt.Errorf("failed to get transcript job: %w", err)
	}
	
	if startedAt.Valid {
		job.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		job.CompletedAt = &completedAt.Time
	}
	if loomPassword.Valid {
		job.LoomPassword = &loomPassword.String
	}
	
	return job, nil
}

// GetTranscriptJobByKey retrieves a transcript job by job key (for idempotency)
func (db *DB) GetTranscriptJobByKey(ctx context.Context, jobKey string) (*models.TranscriptJob, error) {
	job := &models.TranscriptJob{}
	query := `
		SELECT 
			id, video_source_id, session_id, status, error_message, source_url,
			resolved_media_url, queued_at, started_at, completed_at,
			whisper_model, detected_language, duration_seconds, job_key, loom_password
		FROM transcript_jobs
		WHERE job_key = $1
		ORDER BY queued_at DESC
		LIMIT 1
	`
	
	var startedAt, completedAt sql.NullTime
	var loomPassword sql.NullString
	err := db.Pool.QueryRow(ctx, query, jobKey).Scan(
		&job.ID,
		&job.VideoSourceID,
		&job.SessionID,
		&job.Status,
		&job.ErrorMessage,
		&job.SourceURL,
		&job.ResolvedMediaURL,
		&job.QueuedAt,
		&startedAt,
		&completedAt,
		&job.WhisperModel,
		&job.DetectedLanguage,
		&job.DurationSeconds,
		&job.JobKey,
		&loomPassword,
	)
	
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil // No existing job found
		}
		return nil, fmt.Errorf("failed to get transcript job by key: %w", err)
	}
	
	if startedAt.Valid {
		job.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		job.CompletedAt = &completedAt.Time
	}
	if loomPassword.Valid {
		job.LoomPassword = &loomPassword.String
	}
	
	return job, nil
}

// UpdateTranscriptJobStatus updates the status and other fields of a transcript job
func (db *DB) UpdateTranscriptJobStatus(ctx context.Context, jobID uuid.UUID, status models.TranscriptJobStatus, errorMsg *string) error {
	query := `
		UPDATE transcript_jobs
		SET status = $1, error_message = $2, updated_at = now()
		WHERE id = $3
	`
	
	_, err := db.Pool.Exec(ctx, query, status, errorMsg, jobID)
	if err != nil {
		return fmt.Errorf("failed to update transcript job status: %w", err)
	}
	return nil
}

// UpdateTranscriptJobStarted marks a job as started
func (db *DB) UpdateTranscriptJobStarted(ctx context.Context, jobID uuid.UUID) error {
	query := `
		UPDATE transcript_jobs
		SET status = $1, started_at = now(), updated_at = now()
		WHERE id = $2
	`
	
	_, err := db.Pool.Exec(ctx, query, models.TranscriptJobStatusDownloading, jobID)
	if err != nil {
		return fmt.Errorf("failed to update transcript job started: %w", err)
	}
	return nil
}

// UpdateTranscriptJobProgress updates job progress (status, resolved URL, etc.)
func (db *DB) UpdateTranscriptJobProgress(ctx context.Context, jobID uuid.UUID, status models.TranscriptJobStatus, resolvedMediaURL *string) error {
	query := `
		UPDATE transcript_jobs
		SET status = $1, resolved_media_url = $2, updated_at = now()
		WHERE id = $3
	`
	
	_, err := db.Pool.Exec(ctx, query, status, resolvedMediaURL, jobID)
	if err != nil {
		return fmt.Errorf("failed to update transcript job progress: %w", err)
	}
	return nil
}

// CompleteTranscriptJob marks a job as completed with results
func (db *DB) CompleteTranscriptJob(ctx context.Context, jobID uuid.UUID, whisperModel *string, detectedLanguage *string, durationSeconds *int) error {
	query := `
		UPDATE transcript_jobs
		SET status = $1, completed_at = now(), whisper_model = $2, 
		    detected_language = $3, duration_seconds = $4, updated_at = now()
		WHERE id = $5
	`
	
	_, err := db.Pool.Exec(ctx, query, models.TranscriptJobStatusCompleted, whisperModel, detectedLanguage, durationSeconds, jobID)
	if err != nil {
		return fmt.Errorf("failed to complete transcript job: %w", err)
	}
	return nil
}

// FailTranscriptJob marks a job as failed with an error message
func (db *DB) FailTranscriptJob(ctx context.Context, jobID uuid.UUID, errorMsg string) error {
	query := `
		UPDATE transcript_jobs
		SET status = $1, error_message = $2, completed_at = now(), updated_at = now()
		WHERE id = $3
	`
	
	_, err := db.Pool.Exec(ctx, query, models.TranscriptJobStatusFailed, errorMsg, jobID)
	if err != nil {
		return fmt.Errorf("failed to fail transcript job: %w", err)
	}
	return nil
}

// GetTranscriptJobsByVideoSource retrieves all transcript jobs for a video source
func (db *DB) GetTranscriptJobsByVideoSource(ctx context.Context, videoSourceID uuid.UUID) ([]*models.TranscriptJob, error) {
	query := `
		SELECT 
			id, video_source_id, session_id, status, error_message, source_url,
			resolved_media_url, queued_at, started_at, completed_at,
			whisper_model, detected_language, duration_seconds, job_key
		FROM transcript_jobs
		WHERE video_source_id = $1
		ORDER BY queued_at DESC
	`
	
	rows, err := db.Pool.Query(ctx, query, videoSourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to query transcript jobs: %w", err)
	}
	defer rows.Close()
	
	var jobs []*models.TranscriptJob
	for rows.Next() {
		job := &models.TranscriptJob{}
		var startedAt, completedAt sql.NullTime
		
		err := rows.Scan(
			&job.ID,
			&job.VideoSourceID,
			&job.SessionID,
			&job.Status,
			&job.ErrorMessage,
			&job.SourceURL,
			&job.ResolvedMediaURL,
			&job.QueuedAt,
			&startedAt,
			&completedAt,
			&job.WhisperModel,
			&job.DetectedLanguage,
			&job.DurationSeconds,
			&job.JobKey,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan transcript job: %w", err)
		}
		
		if startedAt.Valid {
			job.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			job.CompletedAt = &completedAt.Time
		}
		
		jobs = append(jobs, job)
	}
	
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating transcript jobs: %w", err)
	}
	
	return jobs, nil
}

// UpdateVideoSourceTranscriptionJob updates the transcription_job_id on a video source
func (db *DB) UpdateVideoSourceTranscriptionJob(ctx context.Context, videoSourceID uuid.UUID, jobID *uuid.UUID) error {
	query := `
		UPDATE video_sources
		SET transcription_job_id = $1
		WHERE id = $2
	`
	
	_, err := db.Pool.Exec(ctx, query, jobID, videoSourceID)
	if err != nil {
		return fmt.Errorf("failed to update video source transcription job: %w", err)
	}
	return nil
}

// UpdateVideoSourceTranscriptionSource updates the transcription_source on a video source
func (db *DB) UpdateVideoSourceTranscriptionSource(ctx context.Context, videoSourceID uuid.UUID, source string) error {
	query := `
		UPDATE video_sources
		SET transcription_source = $1
		WHERE id = $2
	`
	
	_, err := db.Pool.Exec(ctx, query, source, videoSourceID)
	if err != nil {
		return fmt.Errorf("failed to update video source transcription source: %w", err)
	}
	return nil
}
