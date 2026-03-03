package database

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/psuthar/talkback/internal/models"
)

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
