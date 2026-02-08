package database

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/psuthar/talkback/internal/models"
)

func (db *DB) CreateIngestionJob(ctx context.Context, job *models.IngestionJob) error {
	query := `
		INSERT INTO ingestion_jobs (id, session_id, source, state, meeting_uuid, instance_uuid, last_error)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at, updated_at
	`
	err := db.Pool.QueryRow(ctx, query,
		job.ID,
		job.SessionID,
		job.Source,
		job.State,
		job.MeetingUUID,
		job.InstanceUUID,
		job.LastError,
	).Scan(&job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create ingestion job: %w", err)
	}
	return nil
}

func (db *DB) GetIngestionJobBySessionID(ctx context.Context, sessionID uuid.UUID) (*models.IngestionJob, error) {
	job := &models.IngestionJob{}
	var meetingUUID, instanceUUID, lastError *string
	query := `
		SELECT id, session_id, source, state, meeting_uuid, instance_uuid, last_error, created_at, updated_at
		FROM ingestion_jobs
		WHERE session_id = $1
		ORDER BY created_at DESC
		LIMIT 1
	`
	err := db.Pool.QueryRow(ctx, query, sessionID).Scan(
		&job.ID,
		&job.SessionID,
		&job.Source,
		&job.State,
		&meetingUUID,
		&instanceUUID,
		&lastError,
		&job.CreatedAt,
		&job.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get ingestion job: %w", err)
	}
	job.MeetingUUID = meetingUUID
	job.InstanceUUID = instanceUUID
	job.LastError = lastError
	return job, nil
}

func (db *DB) UpdateIngestionJobState(ctx context.Context, id uuid.UUID, state string, lastError *string) error {
	query := `
		UPDATE ingestion_jobs
		SET state = $2, last_error = $3, updated_at = now()
		WHERE id = $1
	`
	_, err := db.Pool.Exec(ctx, query, id, state, lastError)
	if err != nil {
		return fmt.Errorf("update ingestion job: %w", err)
	}
	return nil
}
