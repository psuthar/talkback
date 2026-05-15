package handlers

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDedupeExistingAttach covers the SCRUM-413 lookup helper's response
// behavior across every job-state branch the spec enumerates. The handler
// integration is exercised separately by TestSessionImportAttachHandlers_Dedupe.
func TestDedupeExistingAttach(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	ctx := context.Background()
	session := createTestSessionForHandlers(t, h.DB, "dedupe-helper session")

	mu := "meeting-A"
	iu := "instance-A"
	ci := "creator@example.com"

	// Seed one job for the (session, source, meeting, instance) tuple — we
	// move it through each state branch in the sub-tests below.
	job := &models.SessionProcessingJob{
		ID:              uuid.New(),
		SessionID:       session.ID,
		Source:          models.SessionProcessingJobSourceZoom,
		State:           models.ProcessingStateQueued,
		Stage:           models.ProcessingStageFetch,
		MeetingUUID:     &mu,
		InstanceUUID:    &iu,
		CreatorIdentity: &ci,
	}
	require.NoError(t, h.DB.CreateOrGetSessionProcessingJob(ctx, job))

	t.Run("no existing job → Existing nil (caller proceeds with create)", func(t *testing.T) {
		altMU := "different-meeting"
		dedupe, err := dedupeExistingAttach(ctx, h.DB, session.ID, models.SessionProcessingJobSourceZoom, &altMU, &iu, &ci)
		require.NoError(t, err)
		assert.Nil(t, dedupe.Existing)
	})

	t.Run("queued state → 200 already_imported", func(t *testing.T) {
		dedupe, err := dedupeExistingAttach(ctx, h.DB, session.ID, models.SessionProcessingJobSourceZoom, &mu, &iu, &ci)
		require.NoError(t, err)
		require.NotNil(t, dedupe.Existing)
		assert.Equal(t, http.StatusOK, dedupe.Status)
		assert.True(t, dedupe.Response.AlreadyImported)
		assert.False(t, dedupe.Response.Retried)
		assert.Equal(t, models.ProcessingStateQueued, dedupe.Response.State)
	})

	t.Run("mid-stage state → 200 already_imported (preserves the in-flight worker)", func(t *testing.T) {
		require.NoError(t, h.DB.UpdateSessionProcessingJobState(ctx, job.ID, models.ProcessingStateFetching, models.ProcessingStageDownload, 3, nil, nil, nil))
		dedupe, err := dedupeExistingAttach(ctx, h.DB, session.ID, models.SessionProcessingJobSourceZoom, &mu, &iu, &ci)
		require.NoError(t, err)
		require.NotNil(t, dedupe.Existing)
		assert.Equal(t, http.StatusOK, dedupe.Status)
		assert.True(t, dedupe.Response.AlreadyImported)
		assert.Equal(t, models.ProcessingStateFetching, dedupe.Response.State)

		// And the row in DB is still fetching (not reset to queued).
		var state string
		require.NoError(t, h.DB.Pool.QueryRow(ctx, `SELECT state FROM session_processing_jobs WHERE id = $1`, job.ID).Scan(&state))
		assert.Equal(t, models.ProcessingStateFetching, state)
	})

	t.Run("ready state → 200 already_imported", func(t *testing.T) {
		require.NoError(t, h.DB.UpdateSessionProcessingJobState(ctx, job.ID, models.ProcessingStateReady, models.ProcessingStageReady, 1, nil, nil, nil))
		dedupe, err := dedupeExistingAttach(ctx, h.DB, session.ID, models.SessionProcessingJobSourceZoom, &mu, &iu, &ci)
		require.NoError(t, err)
		require.NotNil(t, dedupe.Existing)
		assert.Equal(t, http.StatusOK, dedupe.Status)
		assert.True(t, dedupe.Response.AlreadyImported)
		assert.Equal(t, models.ProcessingStateReady, dedupe.Response.State)
	})

	t.Run("failed_permanent state → 202 retried (re-queues in place)", func(t *testing.T) {
		require.NoError(t, h.DB.UpdateSessionProcessingJobState(ctx, job.ID, models.ProcessingStateFailedPermanent, models.ProcessingStageFetch, 5, nil, nil, nil))
		dedupe, err := dedupeExistingAttach(ctx, h.DB, session.ID, models.SessionProcessingJobSourceZoom, &mu, &iu, &ci)
		require.NoError(t, err)
		require.NotNil(t, dedupe.Existing)
		assert.Equal(t, http.StatusAccepted, dedupe.Status)
		assert.True(t, dedupe.Response.Retried)
		assert.False(t, dedupe.Response.AlreadyImported)
		assert.Equal(t, models.ProcessingStateQueued, dedupe.Response.State)

		// Existing job_id preserved (no new row).
		assert.Equal(t, job.ID.String(), dedupe.Response.JobID)

		// DB row was reset to queued (and a single row remains for this key).
		var state string
		require.NoError(t, h.DB.Pool.QueryRow(ctx, `SELECT state FROM session_processing_jobs WHERE id = $1`, job.ID).Scan(&state))
		assert.Equal(t, models.ProcessingStateQueued, state)

		var n int
		require.NoError(t, h.DB.Pool.QueryRow(ctx, `
			SELECT count(*) FROM session_processing_jobs
			WHERE session_id = $1 AND source = $2
			  AND COALESCE(meeting_uuid, '') = $3 AND COALESCE(instance_uuid, '') = $4
		`, session.ID, models.SessionProcessingJobSourceZoom, mu, iu).Scan(&n))
		assert.Equal(t, 1, n, "no new row was created")
	})

	t.Run("canceled state → 202 retried (same path as failed_permanent)", func(t *testing.T) {
		require.NoError(t, h.DB.UpdateSessionProcessingJobState(ctx, job.ID, models.ProcessingStateCanceled, models.ProcessingStageFetch, 1, nil, nil, nil))
		dedupe, err := dedupeExistingAttach(ctx, h.DB, session.ID, models.SessionProcessingJobSourceZoom, &mu, &iu, &ci)
		require.NoError(t, err)
		require.NotNil(t, dedupe.Existing)
		assert.Equal(t, http.StatusAccepted, dedupe.Status)
		assert.True(t, dedupe.Response.Retried)
		assert.Equal(t, models.ProcessingStateQueued, dedupe.Response.State)
	})

	t.Run("NULL meeting/instance UUIDs treated as empty by the partial-null-safe lookup", func(t *testing.T) {
		s := createTestSessionForHandlers(t, h.DB, "dedupe-null-key session")
		j := &models.SessionProcessingJob{
			ID:        uuid.New(),
			SessionID: s.ID,
			Source:    models.SessionProcessingJobSourceZoom,
			State:     models.ProcessingStateQueued,
			Stage:     models.ProcessingStageFetch,
		}
		require.NoError(t, h.DB.CreateOrGetSessionProcessingJob(ctx, j))

		dedupe, err := dedupeExistingAttach(ctx, h.DB, s.ID, models.SessionProcessingJobSourceZoom, nil, nil, nil)
		require.NoError(t, err)
		require.NotNil(t, dedupe.Existing, "row with NULL meeting/instance UUIDs must match a lookup with nil pointers")
		assert.True(t, dedupe.Response.AlreadyImported)
	})
}
