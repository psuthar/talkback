package database

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/psuthar/talkback/internal/models"
)

// CreateOrGetSessionProcessingJob creates a job or returns existing one for session/source (idempotent enqueue).
func (db *DB) CreateOrGetSessionProcessingJob(ctx context.Context, job *models.SessionProcessingJob) error {
	query := `
		INSERT INTO session_processing_jobs (
			id, session_id, source, state, stage, attempt_count, next_retry_at,
			meeting_uuid, instance_uuid, creator_identity
		) VALUES ($1, $2, $3, $4, $5, 0, now(), $6, $7, $8)
		ON CONFLICT (session_id, source) DO UPDATE SET
			state = 'queued', stage = 'fetch', next_retry_at = now(),
			meeting_uuid = EXCLUDED.meeting_uuid, instance_uuid = EXCLUDED.instance_uuid,
			creator_identity = EXCLUDED.creator_identity, last_error_code = NULL, last_error_message = NULL,
			locked_at = NULL, lock_owner = NULL, updated_at = now()
		RETURNING id, state, stage, attempt_count, next_retry_at, last_error_code, last_error_message,
			locked_at, lock_owner, meeting_uuid, instance_uuid, creator_identity, created_at, updated_at
	`
	var nextRetryAt, lockedAt *time.Time
	var lastCode, lastMsg, lockOwner *string
	var meetingUUID, instanceUUID, creatorIdentity *string
	err := db.Pool.QueryRow(ctx, query,
		job.ID,
		job.SessionID,
		job.Source,
		job.State,
		job.Stage,
		job.MeetingUUID,
		job.InstanceUUID,
		job.CreatorIdentity,
	).Scan(
		&job.ID,
		&job.State,
		&job.Stage,
		&job.AttemptCount,
		&nextRetryAt,
		&lastCode,
		&lastMsg,
		&lockedAt,
		&lockOwner,
		&meetingUUID,
		&instanceUUID,
		&creatorIdentity,
		&job.CreatedAt,
		&job.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create or get session processing job: %w", err)
	}
	job.NextRetryAt = nextRetryAt
	job.LastErrorCode = lastCode
	job.LastErrorMessage = lastMsg
	job.LockedAt = lockedAt
	job.LockOwner = lockOwner
	job.MeetingUUID = meetingUUID
	job.InstanceUUID = instanceUUID
	job.CreatorIdentity = creatorIdentity
	return nil
}

// GetSessionProcessingJobByID returns a job by id.
func (db *DB) GetSessionProcessingJobByID(ctx context.Context, id uuid.UUID) (*models.SessionProcessingJob, error) {
	query := `
		SELECT id, session_id, source, state, stage, attempt_count, next_retry_at,
			last_error_code, last_error_message, locked_at, lock_owner,
			meeting_uuid, instance_uuid, creator_identity, created_at, updated_at
		FROM session_processing_jobs WHERE id = $1
	`
	j := &models.SessionProcessingJob{}
	var nextRetryAt, lockedAt *time.Time
	var lastCode, lastMsg, lockOwner *string
	var meetingUUID, instanceUUID, creatorIdentity *string
	err := db.Pool.QueryRow(ctx, query, id).Scan(
		&j.ID,
		&j.SessionID,
		&j.Source,
		&j.State,
		&j.Stage,
		&j.AttemptCount,
		&nextRetryAt,
		&lastCode,
		&lastMsg,
		&lockedAt,
		&lockOwner,
		&meetingUUID,
		&instanceUUID,
		&creatorIdentity,
		&j.CreatedAt,
		&j.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get session processing job: %w", err)
	}
	j.NextRetryAt = nextRetryAt
	j.LastErrorCode = lastCode
	j.LastErrorMessage = lastMsg
	j.LockedAt = lockedAt
	j.LockOwner = lockOwner
	j.MeetingUUID = meetingUUID
	j.InstanceUUID = instanceUUID
	j.CreatorIdentity = creatorIdentity
	return j, nil
}

// GetSessionProcessingJobBySessionID returns the job for session/source (any state).
// When source is "" it defaults to "zoom" for backward compatibility with callers that
// predate multi-provider support. New callers should always pass an explicit source value.
func (db *DB) GetSessionProcessingJobBySessionID(ctx context.Context, sessionID uuid.UUID, source string) (*models.SessionProcessingJob, error) {
	if source == "" {
		// Default retained for backward compat; callers should pass explicit source.
		source = "zoom"
	}
	query := `
		SELECT id, session_id, source, state, stage, attempt_count, next_retry_at,
			last_error_code, last_error_message, locked_at, lock_owner,
			meeting_uuid, instance_uuid, creator_identity, created_at, updated_at
		FROM session_processing_jobs
		WHERE session_id = $1 AND source = $2
	`
	j := &models.SessionProcessingJob{}
	var nextRetryAt, lockedAt *time.Time
	var lastCode, lastMsg, lockOwner *string
	var meetingUUID, instanceUUID, creatorIdentity *string
	err := db.Pool.QueryRow(ctx, query, sessionID, source).Scan(
		&j.ID,
		&j.SessionID,
		&j.Source,
		&j.State,
		&j.Stage,
		&j.AttemptCount,
		&nextRetryAt,
		&lastCode,
		&lastMsg,
		&lockedAt,
		&lockOwner,
		&meetingUUID,
		&instanceUUID,
		&creatorIdentity,
		&j.CreatedAt,
		&j.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get session processing job: %w", err)
	}
	j.NextRetryAt = nextRetryAt
	j.LastErrorCode = lastCode
	j.LastErrorMessage = lastMsg
	j.LockedAt = lockedAt
	j.LockOwner = lockOwner
	j.MeetingUUID = meetingUUID
	j.InstanceUUID = instanceUUID
	j.CreatorIdentity = creatorIdentity
	return j, nil
}

// UpdateSessionProcessingJobState updates state, stage, attempt_count, next_retry_at, error fields, and updated_at.
func (db *DB) UpdateSessionProcessingJobState(ctx context.Context, id uuid.UUID, state, stage string, attemptCount int, nextRetryAt *time.Time, lastErrorCode, lastErrorMessage *string) error {
	query := `
		UPDATE session_processing_jobs
		SET state = $2, stage = $3, attempt_count = $4, next_retry_at = $5,
		    last_error_code = $6, last_error_message = $7, updated_at = now()
		WHERE id = $1
	`
	_, err := db.Pool.Exec(ctx, query, id, state, stage, attemptCount, nextRetryAt, lastErrorCode, lastErrorMessage)
	if err != nil {
		return fmt.Errorf("update session processing job state: %w", err)
	}
	return nil
}

// LockSessionProcessingJob sets locked_at and lock_owner. Returns true if lock acquired (row existed and was not already locked).
func (db *DB) LockSessionProcessingJob(ctx context.Context, id uuid.UUID, owner string, lockDuration time.Duration) (bool, error) {
	query := `
		UPDATE session_processing_jobs
		SET locked_at = now(), lock_owner = $2, updated_at = now()
		WHERE id = $1 AND (locked_at IS NULL OR locked_at < now() - $3::interval)
	`
	result, err := db.Pool.Exec(ctx, query, id, owner, lockDuration.String())
	if err != nil {
		return false, fmt.Errorf("lock session processing job: %w", err)
	}
	return result.RowsAffected() > 0, nil
}

// UnlockSessionProcessingJob clears locked_at and lock_owner.
func (db *DB) UnlockSessionProcessingJob(ctx context.Context, id uuid.UUID) error {
	query := `UPDATE session_processing_jobs SET locked_at = NULL, lock_owner = NULL, updated_at = now() WHERE id = $1`
	_, err := db.Pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("unlock session processing job: %w", err)
	}
	return nil
}

// ClaimNextSessionProcessingJob finds a runnable job, locks it, and returns it. Returns nil if none.
func (db *DB) ClaimNextSessionProcessingJob(ctx context.Context, owner string, lockDuration time.Duration, now time.Time) (*models.SessionProcessingJob, error) {
	query := `
		WITH candidate AS (
			SELECT id FROM session_processing_jobs
			WHERE state IN ('queued', 'failed_transient', 'waiting')
			  AND (next_retry_at IS NULL OR next_retry_at <= $1)
			  AND (locked_at IS NULL OR locked_at < $1 - $2::interval)
			ORDER BY next_retry_at ASC NULLS FIRST, created_at ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		),
		updated AS (
			UPDATE session_processing_jobs j
			SET locked_at = $1, lock_owner = $3, updated_at = now()
			FROM candidate c WHERE j.id = c.id
			RETURNING j.id, j.session_id, j.source, j.state, j.stage, j.attempt_count, j.next_retry_at,
				j.last_error_code, j.last_error_message, j.meeting_uuid, j.instance_uuid, j.creator_identity, j.created_at, j.updated_at
		)
		SELECT id, session_id, source, state, stage, attempt_count, next_retry_at,
			last_error_code, last_error_message, meeting_uuid, instance_uuid, creator_identity, created_at, updated_at
		FROM updated
	`
	rows, err := db.Pool.Query(ctx, query, now, lockDuration.String(), owner)
	if err != nil {
		return nil, fmt.Errorf("claim next session processing job: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	j := &models.SessionProcessingJob{}
	var nextRetryAt *time.Time
	var lastCode, lastMsg *string
	var meetingUUID, instanceUUID, creatorIdentity *string
	err = rows.Scan(
		&j.ID,
		&j.SessionID,
		&j.Source,
		&j.State,
		&j.Stage,
		&j.AttemptCount,
		&nextRetryAt,
		&lastCode,
		&lastMsg,
		&meetingUUID,
		&instanceUUID,
		&creatorIdentity,
		&j.CreatedAt,
		&j.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	j.NextRetryAt = nextRetryAt
	j.LastErrorCode = lastCode
	j.LastErrorMessage = lastMsg
	j.MeetingUUID = meetingUUID
	j.InstanceUUID = instanceUUID
	j.CreatorIdentity = creatorIdentity
	j.LockedAt = &now
	lo := owner
	j.LockOwner = &lo
	return j, nil
}

// UpdateSessionProcessingMirror updates sessions.processing_state and processing_updated_at from job.
func (db *DB) UpdateSessionProcessingMirror(ctx context.Context, sessionID uuid.UUID, state string) error {
	query := `UPDATE sessions SET processing_state = $1, processing_updated_at = now() WHERE id = $2`
	_, err := db.Pool.Exec(ctx, query, state, sessionID)
	if err != nil {
		return fmt.Errorf("update session processing mirror: %w", err)
	}
	return nil
}

// RetrySessionProcessingJob sets state=queued, next_retry_at=now, clears lock and error fields.
// When source is "" it defaults to "zoom" for backward compatibility. New callers should
// always pass an explicit source value derived from the session's source_provider.
func (db *DB) RetrySessionProcessingJob(ctx context.Context, sessionID uuid.UUID, source string) error {
	if source == "" {
		// Default retained for backward compat; callers should pass explicit source.
		source = "zoom"
	}
	query := `
		UPDATE session_processing_jobs
		SET state = 'queued', stage = 'fetch', next_retry_at = now(),
		    last_error_code = NULL, last_error_message = NULL, locked_at = NULL, lock_owner = NULL, updated_at = now()
		WHERE session_id = $1 AND source = $2 AND state IN ('failed_transient', 'failed_permanent', 'waiting')
	`
	_, err := db.Pool.Exec(ctx, query, sessionID, source)
	if err != nil {
		return fmt.Errorf("retry session processing job: %w", err)
	}
	return nil
}

// CancelSessionProcessingJob sets state=canceled.
// When source is "" it defaults to "zoom" for backward compatibility. New callers should
// always pass an explicit source value derived from the session's source_provider.
func (db *DB) CancelSessionProcessingJob(ctx context.Context, sessionID uuid.UUID, source string) error {
	if source == "" {
		// Default retained for backward compat; callers should pass explicit source.
		source = "zoom"
	}
	query := `UPDATE session_processing_jobs SET state = 'canceled', updated_at = now() WHERE session_id = $1 AND source = $2`
	_, err := db.Pool.Exec(ctx, query, sessionID, source)
	if err != nil {
		return fmt.Errorf("cancel session processing job: %w", err)
	}
	return nil
}

// ResetSessionProcessingJobRetry sets next_retry_at = now() and clears lock so the job can be claimed again.
func (db *DB) ResetSessionProcessingJobRetry(ctx context.Context, id uuid.UUID) error {
	query := `UPDATE session_processing_jobs SET next_retry_at = now(), locked_at = NULL, lock_owner = NULL, updated_at = now() WHERE id = $1`
	_, err := db.Pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("reset session processing job retry: %w", err)
	}
	return nil
}

// ListSessionProcessingJobsForReconciler returns jobs that are stuck or waiting (for reset next_retry_at).
func (db *DB) ListSessionProcessingJobsForReconciler(ctx context.Context, inProgressOlderThan time.Time) ([]*models.SessionProcessingJob, error) {
	query := `
		SELECT id, session_id, source, state, stage, attempt_count, next_retry_at,
			last_error_code, last_error_message, meeting_uuid, instance_uuid, creator_identity, created_at, updated_at
		FROM session_processing_jobs
		WHERE state IN ('waiting', 'failed_transient')
		   OR (state IN ('queued', 'fetching', 'downloading', 'parsing', 'chunking', 'embedding') AND updated_at < $1)
		ORDER BY updated_at ASC
		LIMIT 100
	`
	rows, err := db.Pool.Query(ctx, query, inProgressOlderThan)
	if err != nil {
		return nil, fmt.Errorf("list jobs for reconciler: %w", err)
	}
	defer rows.Close()
	var list []*models.SessionProcessingJob
	for rows.Next() {
		j := &models.SessionProcessingJob{}
		var nextRetryAt *time.Time
		var lastCode, lastMsg *string
		var meetingUUID, instanceUUID, creatorIdentity *string
		err = rows.Scan(
			&j.ID,
			&j.SessionID,
			&j.Source,
			&j.State,
			&j.Stage,
			&j.AttemptCount,
			&nextRetryAt,
			&lastCode,
			&lastMsg,
			&meetingUUID,
			&instanceUUID,
			&creatorIdentity,
			&j.CreatedAt,
			&j.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		j.NextRetryAt = nextRetryAt
		j.LastErrorCode = lastCode
		j.LastErrorMessage = lastMsg
		j.MeetingUUID = meetingUUID
		j.InstanceUUID = instanceUUID
		j.CreatorIdentity = creatorIdentity
		list = append(list, j)
	}
	return list, rows.Err()
}
