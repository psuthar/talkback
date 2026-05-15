package database

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAggregateProcessingState is the SCRUM-408 precedence-table regression.
// It pins the user-visible state computed across N per-recording jobs.
func TestAggregateProcessingState(t *testing.T) {
	cases := []struct {
		name   string
		states []string
		want   string
	}{
		{"empty list yields empty string", nil, ""},
		{"single ready stays ready", []string{models.ProcessingStateReady}, models.ProcessingStateReady},
		{"single failed_permanent stays failed_permanent", []string{models.ProcessingStateFailedPermanent}, models.ProcessingStateFailedPermanent},
		{"any non-terminal beats any terminal — fetching+ready -> fetching", []string{models.ProcessingStateReady, models.ProcessingStateFetching}, models.ProcessingStateFetching},
		{"failed_transient beats waiting", []string{models.ProcessingStateWaiting, models.ProcessingStateFailedTransient}, models.ProcessingStateFailedTransient},
		{"waiting beats queued", []string{models.ProcessingStateQueued, models.ProcessingStateWaiting}, models.ProcessingStateWaiting},
		{"queued beats embedding (queued = not started, embedding = working)", []string{models.ProcessingStateEmbedding, models.ProcessingStateQueued}, models.ProcessingStateQueued},
		{"failed_transient beats every other non-terminal", []string{models.ProcessingStateFetching, models.ProcessingStateEmbedding, models.ProcessingStateFailedTransient, models.ProcessingStateQueued}, models.ProcessingStateFailedTransient},
		{"all-terminal mix — failed_permanent + ready + canceled -> failed_permanent", []string{models.ProcessingStateReady, models.ProcessingStateCanceled, models.ProcessingStateFailedPermanent}, models.ProcessingStateFailedPermanent},
		{"all-terminal mix — canceled + ready -> canceled", []string{models.ProcessingStateReady, models.ProcessingStateCanceled}, models.ProcessingStateCanceled},
		{"all-ready stays ready", []string{models.ProcessingStateReady, models.ProcessingStateReady, models.ProcessingStateReady}, models.ProcessingStateReady},
		{"awaiting_whisper beats embedding", []string{models.ProcessingStateEmbedding, models.ProcessingStateAwaitingWhisper}, models.ProcessingStateAwaitingWhisper},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			jobs := make([]*models.SessionProcessingJob, 0, len(tc.states))
			for _, s := range tc.states {
				jobs = append(jobs, &models.SessionProcessingJob{State: s})
			}
			assert.Equal(t, tc.want, AggregateProcessingState(jobs))
		})
	}
}

// TestMirrorAggregateProcessingState_AndUpsertGuard exercises the DB-side
// pieces of SCRUM-408 end-to-end:
//   - ListSessionProcessingJobsBySessionID returns all rows for a session,
//   - MirrorAggregateProcessingState writes the aggregate to
//     sessions.processing_state,
//   - CreateOrGetSessionProcessingJob refuses to clobber an in-flight job
//     (state IN ('fetching',…,'awaiting_whisper')) on idempotent re-import.
func TestMirrorAggregateProcessingState_AndUpsertGuard(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "aggregate-mirror session")

	mkJob := func(source, meeting string, state, stage string) *models.SessionProcessingJob {
		j := &models.SessionProcessingJob{
			ID:        uuid.New(),
			SessionID: session.ID,
			Source:    source,
			State:     state,
			Stage:     stage,
		}
		if meeting != "" {
			s := meeting
			j.MeetingUUID = &s
		}
		return j
	}

	// Seed three jobs that exercise the new partial-null-safe UNIQUE: same
	// session+source, different meeting_uuid. With the SCRUM-403 key this is
	// what multi-recording-per-session looks like in the wire.
	job1 := mkJob("zoom", "meeting-A", models.ProcessingStateQueued, models.ProcessingStageFetch)
	job2 := mkJob("zoom", "meeting-B", models.ProcessingStateQueued, models.ProcessingStageFetch)
	job3 := mkJob("zoom", "meeting-C", models.ProcessingStateQueued, models.ProcessingStageFetch)
	require.NoError(t, db.CreateOrGetSessionProcessingJob(ctx, job1))
	require.NoError(t, db.CreateOrGetSessionProcessingJob(ctx, job2))
	require.NoError(t, db.CreateOrGetSessionProcessingJob(ctx, job3))

	t.Run("ListSessionProcessingJobsBySessionID returns all 3 jobs", func(t *testing.T) {
		jobs, err := db.ListSessionProcessingJobsBySessionID(ctx, session.ID)
		require.NoError(t, err)
		require.Len(t, jobs, 3)
	})

	t.Run("Mirror aggregate over (queued,queued,queued) -> queued", func(t *testing.T) {
		require.NoError(t, db.MirrorAggregateProcessingState(ctx, session.ID))
		var got *string
		err := db.Pool.QueryRow(ctx, `SELECT processing_state FROM sessions WHERE id = $1`, session.ID).Scan(&got)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, models.ProcessingStateQueued, *got)
	})

	t.Run("Mirror aggregate when one job is fetching -> fetching wins over queued", func(t *testing.T) {
		require.NoError(t, db.UpdateSessionProcessingJobState(ctx, job1.ID, models.ProcessingStateFetching, models.ProcessingStageFetch, 1, nil, nil, nil))

		require.NoError(t, db.MirrorAggregateProcessingState(ctx, session.ID))
		var got *string
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT processing_state FROM sessions WHERE id = $1`, session.ID).Scan(&got))
		require.NotNil(t, got)
		// queued beats fetching per the precedence table — queued = "not
		// started" outranks "working" by design.
		assert.Equal(t, models.ProcessingStateQueued, *got, "queued (not-started) should outrank fetching (working) so the user still sees something pending")
	})

	t.Run("Mirror aggregate when one job is failed_transient -> failed_transient wins", func(t *testing.T) {
		require.NoError(t, db.UpdateSessionProcessingJobState(ctx, job2.ID, models.ProcessingStateFailedTransient, models.ProcessingStageFetch, 1, nil, nil, nil))

		require.NoError(t, db.MirrorAggregateProcessingState(ctx, session.ID))
		var got *string
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT processing_state FROM sessions WHERE id = $1`, session.ID).Scan(&got))
		require.NotNil(t, got)
		assert.Equal(t, models.ProcessingStateFailedTransient, *got)
	})

	t.Run("All-terminal: failed_permanent + ready -> failed_permanent", func(t *testing.T) {
		require.NoError(t, db.UpdateSessionProcessingJobState(ctx, job1.ID, models.ProcessingStateReady, models.ProcessingStageReady, 1, nil, nil, nil))
		require.NoError(t, db.UpdateSessionProcessingJobState(ctx, job2.ID, models.ProcessingStateFailedPermanent, models.ProcessingStageFetch, 1, nil, nil, nil))
		require.NoError(t, db.UpdateSessionProcessingJobState(ctx, job3.ID, models.ProcessingStateReady, models.ProcessingStageReady, 1, nil, nil, nil))

		require.NoError(t, db.MirrorAggregateProcessingState(ctx, session.ID))
		var got *string
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT processing_state FROM sessions WHERE id = $1`, session.ID).Scan(&got))
		require.NotNil(t, got)
		assert.Equal(t, models.ProcessingStateFailedPermanent, *got)
	})

	t.Run("Upsert refuses to clobber an in-flight job", func(t *testing.T) {
		inFlight := mkJob("zoom", "meeting-X", models.ProcessingStateFetching, models.ProcessingStageDownload)
		inFlight.AttemptCount = 5 // exercise that AttemptCount isn't reset either
		// Manually create with a specific state — DB layer always inserts as 'queued' on creation,
		// so we set it to fetching afterwards.
		require.NoError(t, db.CreateOrGetSessionProcessingJob(ctx, inFlight))
		// Lock + transition to fetching to mimic an active mid-stage row.
		require.NoError(t, db.UpdateSessionProcessingJobState(ctx, inFlight.ID, models.ProcessingStateFetching, models.ProcessingStageDownload, 5, nil, nil, nil))
		_, err := db.Pool.Exec(ctx, `UPDATE session_processing_jobs SET locked_at = now(), lock_owner = 'worker-1' WHERE id = $1`, inFlight.ID)
		require.NoError(t, err)

		// Now simulate a re-import for the SAME (session, source, meeting_uuid):
		// must NOT clobber state/lock back to queued.
		reattach := mkJob("zoom", "meeting-X", models.ProcessingStateQueued, models.ProcessingStageFetch)
		require.NoError(t, db.CreateOrGetSessionProcessingJob(ctx, reattach))

		// Read back the row by the in-flight job's id and assert its state/lock preserved.
		var state string
		var attempt int
		var lockOwner *string
		var lockedAt *time.Time
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT state, attempt_count, lock_owner, locked_at FROM session_processing_jobs WHERE id = $1`, inFlight.ID).
			Scan(&state, &attempt, &lockOwner, &lockedAt))
		assert.Equal(t, models.ProcessingStateFetching, state, "in-flight state must not be clobbered by re-import")
		assert.Equal(t, 5, attempt, "attempt_count must be preserved")
		require.NotNil(t, lockOwner)
		assert.Equal(t, "worker-1", *lockOwner, "active worker lock must be preserved")
		require.NotNil(t, lockedAt, "lock timestamp must be preserved")
	})

	t.Run("Upsert RE-queues a row that is in a terminal/idle state", func(t *testing.T) {
		j := mkJob("zoom", "meeting-Y", models.ProcessingStateQueued, models.ProcessingStageFetch)
		require.NoError(t, db.CreateOrGetSessionProcessingJob(ctx, j))
		require.NoError(t, db.UpdateSessionProcessingJobState(ctx, j.ID, models.ProcessingStateFailedPermanent, models.ProcessingStageFetch, 1, nil, nil, nil))

		// Re-import same (session, source, meeting_uuid).
		reattach := mkJob("zoom", "meeting-Y", models.ProcessingStateQueued, models.ProcessingStageFetch)
		require.NoError(t, db.CreateOrGetSessionProcessingJob(ctx, reattach))

		var state string
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT state FROM session_processing_jobs WHERE id = $1`, j.ID).Scan(&state))
		assert.Equal(t, models.ProcessingStateQueued, state, "a row not in active mid-stage must be re-queued by the upsert")
	})

	t.Run("Existing Meet single-recording regression: enqueue + complete -> mirror=ready", func(t *testing.T) {
		s := createTestSession(t, db, "single-meet regression session")
		j := &models.SessionProcessingJob{
			ID:        uuid.New(),
			SessionID: s.ID,
			Source:    models.SessionProcessingJobSourceGoogleMeet,
			State:     models.ProcessingStateQueued,
			Stage:     models.ProcessingStageFetch,
		}
		require.NoError(t, db.CreateOrGetSessionProcessingJob(ctx, j))
		require.NoError(t, db.UpdateSessionProcessingJobState(ctx, j.ID, models.ProcessingStateReady, models.ProcessingStageReady, 1, nil, nil, nil))
		require.NoError(t, db.MirrorAggregateProcessingState(ctx, s.ID))
		var got *string
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT processing_state FROM sessions WHERE id = $1`, s.ID).Scan(&got))
		require.NotNil(t, got)
		assert.Equal(t, models.ProcessingStateReady, *got)
	})
}
