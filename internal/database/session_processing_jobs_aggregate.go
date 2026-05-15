package database

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
)

// ListSessionProcessingJobsBySessionID returns every job for a session,
// ordered by created_at. SCRUM-408: needed so the aggregate-state mirror and
// future UI surfaces can read the full per-recording job list for a session.
func (db *DB) ListSessionProcessingJobsBySessionID(ctx context.Context, sessionID uuid.UUID) ([]*models.SessionProcessingJob, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT id, session_id, source, state, stage, attempt_count, next_retry_at,
		       last_error_code, last_error_message, locked_at, lock_owner,
		       meeting_uuid, instance_uuid, creator_identity, created_at, updated_at
		FROM session_processing_jobs
		WHERE session_id = $1
		ORDER BY created_at, id
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list session processing jobs by session: %w", err)
	}
	defer rows.Close()

	out := []*models.SessionProcessingJob{}
	for rows.Next() {
		j := &models.SessionProcessingJob{}
		var nextRetryAt, lockedAt *time.Time
		var lastCode, lastMsg, lockOwner *string
		var meetingUUID, instanceUUID, creatorIdentity *string
		if err := rows.Scan(
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
		); err != nil {
			return nil, fmt.Errorf("scan session processing job: %w", err)
		}
		j.NextRetryAt = nextRetryAt
		j.LastErrorCode = lastCode
		j.LastErrorMessage = lastMsg
		j.LockedAt = lockedAt
		j.LockOwner = lockOwner
		j.MeetingUUID = meetingUUID
		j.InstanceUUID = instanceUUID
		j.CreatorIdentity = creatorIdentity
		out = append(out, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate session processing jobs: %w", err)
	}
	return out, nil
}

// statePriority returns the aggregate-state precedence for a per-job state.
// Higher value wins. Non-terminal states sit in 200+; terminal states sit
// below 200, so any non-terminal beats any terminal regardless of which
// specific terminal it is.
//
// Within non-terminal, the precedence is roughly "worst signal to the user":
// failed_transient (active retry on error) > waiting (stalled) > queued
// (not started) > working states. Within terminal: failed_permanent >
// canceled > ready.
func statePriority(state string) int {
	switch state {
	// Non-terminal — visible "something is wrong" comes first.
	case models.ProcessingStateFailedTransient:
		return 290
	case models.ProcessingStateWaiting:
		return 280
	case models.ProcessingStateAwaitingWhisper:
		return 270
	case models.ProcessingStateQueued:
		return 260
	// Working states — order is the rough "earliest in the pipeline wins" so
	// that an aggregate over (fetching, embedding) surfaces "fetching" (more
	// total work remaining). Any ordering inside this group is acceptable —
	// they all read as "this session is currently being processed".
	case models.ProcessingStateFetching:
		return 250
	case models.ProcessingStateDownloading:
		return 240
	case models.ProcessingStateParsing:
		return 230
	case models.ProcessingStateChunking:
		return 220
	case models.ProcessingStateEmbedding:
		return 210
	// Terminal — failed/canceled outrank ready so a partial-failure session
	// surfaces the failure to the user.
	case models.ProcessingStateFailedPermanent:
		return 120
	case models.ProcessingStateCanceled:
		return 110
	case models.ProcessingStateReady:
		return 100
	default:
		// Unknown states should not be silently treated as terminal: rank
		// below everything so explicit known states always win, but above
		// "no state" (0).
		return 1
	}
}

// AggregateProcessingState computes the user-visible session-level state
// from a slice of job states using the SCRUM-408 precedence rules:
//   1. Any non-terminal state wins over any terminal state.
//   2. Among non-terminal jobs, pick the "worst" signal (failed_transient >
//      waiting > queued > working stages).
//   3. Among all-terminal jobs, pick the worst (failed_permanent > canceled
//      > ready).
// Empty input returns "".
func AggregateProcessingState(jobs []*models.SessionProcessingJob) string {
	best := ""
	bestPriority := -1
	for _, j := range jobs {
		if j == nil {
			continue
		}
		p := statePriority(j.State)
		if p > bestPriority {
			bestPriority = p
			best = j.State
		}
	}
	return best
}

// GetSessionProcessingJobByRecordingKey returns the existing session-processing
// job for the SCRUM-403 partial-null-safe key
// (session_id, source, meeting_uuid, instance_uuid), or nil if none. Used by
// the SCRUM-413 attach-handler dedupe pre-check.
//
// nil meetingUUID / instanceUUID inputs match the same NULL-coalesces-to-''
// semantics as the row's unique index.
func (db *DB) GetSessionProcessingJobByRecordingKey(
	ctx context.Context,
	sessionID uuid.UUID,
	source string,
	meetingUUID, instanceUUID *string,
) (*models.SessionProcessingJob, error) {
	mu := ""
	iu := ""
	if meetingUUID != nil {
		mu = *meetingUUID
	}
	if instanceUUID != nil {
		iu = *instanceUUID
	}
	row := db.Pool.QueryRow(ctx, `
		SELECT id, session_id, source, state, stage, attempt_count, next_retry_at,
		       last_error_code, last_error_message, locked_at, lock_owner,
		       meeting_uuid, instance_uuid, creator_identity, created_at, updated_at
		FROM session_processing_jobs
		WHERE session_id = $1
		  AND source = $2
		  AND COALESCE(meeting_uuid, '') = $3
		  AND COALESCE(instance_uuid, '') = $4
		LIMIT 1
	`, sessionID, source, mu, iu)
	j := &models.SessionProcessingJob{}
	var nextRetryAt, lockedAt *time.Time
	var lastCode, lastMsg, lockOwner *string
	var mUUID, iUUID, creator *string
	err := row.Scan(
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
		&mUUID,
		&iUUID,
		&creator,
		&j.CreatedAt,
		&j.UpdatedAt,
	)
	if err != nil {
		if err.Error() == "no rows in result set" || errIsNoRows(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get session processing job by recording key: %w", err)
	}
	j.NextRetryAt = nextRetryAt
	j.LastErrorCode = lastCode
	j.LastErrorMessage = lastMsg
	j.LockedAt = lockedAt
	j.LockOwner = lockOwner
	j.MeetingUUID = mUUID
	j.InstanceUUID = iUUID
	j.CreatorIdentity = creator
	return j, nil
}

// errIsNoRows is a local helper that handles pgx's typed sentinel without
// pulling pgx into this file (the package already imports pgx elsewhere).
func errIsNoRows(err error) bool {
	return err != nil && err.Error() == "no rows in result set"
}

// MirrorAggregateProcessingState reads every job for the session, computes
// the aggregate state via AggregateProcessingState, and updates
// sessions.processing_state + processing_updated_at. Equivalent to
// UpdateSessionProcessingMirror(sessionID, aggregate) for the single-job
// case, but correct when N>1.
//
// A session with no jobs is a no-op (mirror left as-is) — the caller is
// responsible for the initial mirror=queued write at enqueue time, which
// happens before the row exists in this query's result.
func (db *DB) MirrorAggregateProcessingState(ctx context.Context, sessionID uuid.UUID) error {
	jobs, err := db.ListSessionProcessingJobsBySessionID(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("mirror aggregate state: %w", err)
	}
	if len(jobs) == 0 {
		return nil
	}
	state := AggregateProcessingState(jobs)
	if state == "" {
		return nil
	}
	return db.UpdateSessionProcessingMirror(ctx, sessionID, state)
}
