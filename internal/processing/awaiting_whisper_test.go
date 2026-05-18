// SCRUM-469: tests for FlipAwaitingWhisperJobsToReady. The bug it fixes:
// OnTranscriptCompleted looked up a single job by sessions.source_provider
// and missed multi-recording sessions where the awaiting job's source
// differs from the session's original.
package processing

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlipAwaitingWhisperJobsToReady(t *testing.T) {
	// No t.Parallel(): the shared test DB doesn't isolate per-test data,
	// and this test scans by session.
	db, cleanup := setupTestDBForProcessing(t)
	defer cleanup()
	ctx := context.Background()

	session := createTestSession(t, db, "awaiting-whisper-flip")

	insertJob := func(state, source string) uuid.UUID {
		id := uuid.New()
		// UNIQUE(session_id, source, meeting_uuid, instance_uuid) — use the
		// job id itself in the keys so multiple rows under the same source
		// don't collide.
		mUUID := "m-" + source + "-" + id.String()
		iUUID := "i-" + source + "-" + id.String()
		_, err := db.Pool.Exec(ctx, `
			INSERT INTO session_processing_jobs
			(id, session_id, source, state, stage, meeting_uuid, instance_uuid, created_at, updated_at)
			VALUES ($1, $2, $3, $4, 'fetch', $5, $6, $7, $7)
		`, id, session.ID, source, state, mUUID, iUUID, time.Now())
		require.NoError(t, err)
		return id
	}

	zoomAwaiting := insertJob(models.ProcessingStateAwaitingWhisper, "zoom")
	teamsAwaiting := insertJob(models.ProcessingStateAwaitingWhisper, "teams")
	meetReady := insertJob(models.ProcessingStateReady, "google_meet")
	zoomQueued := insertJob(models.ProcessingStateQueued, "zoom")

	flipped := FlipAwaitingWhisperJobsToReady(ctx, db, session.ID)
	assert.Equal(t, 2, flipped, "should flip both awaiting_whisper rows regardless of source")

	mustState := func(jobID uuid.UUID) string {
		var s string
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT state FROM session_processing_jobs WHERE id=$1`, jobID).Scan(&s))
		return s
	}
	assert.Equal(t, models.ProcessingStateReady, mustState(zoomAwaiting), "zoom awaiting → ready")
	assert.Equal(t, models.ProcessingStateReady, mustState(teamsAwaiting), "teams awaiting → ready (multi-recording case)")
	assert.Equal(t, models.ProcessingStateReady, mustState(meetReady), "ready stays ready")
	assert.Equal(t, models.ProcessingStateQueued, mustState(zoomQueued), "queued is NOT touched — only awaiting_whisper flips")
}

func TestFlipAwaitingWhisperJobsToReady_NoOpWhenNoneAwaiting(t *testing.T) {
	db, cleanup := setupTestDBForProcessing(t)
	defer cleanup()
	ctx := context.Background()

	session := createTestSession(t, db, "no-awaiting-jobs")

	flipped := FlipAwaitingWhisperJobsToReady(ctx, db, session.ID)
	assert.Equal(t, 0, flipped)
}
