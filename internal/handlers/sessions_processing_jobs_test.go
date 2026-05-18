// SCRUM-468: pin processing_jobs exposure on the GET /sessions/:id
// response so the SPA can render in-progress import placeholder rows.
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetSession_ProcessingJobsField surfaces session_processing_jobs in
// non-terminal states only. Ready / canceled / failed_permanent are
// excluded so they don't clutter the SPA's placeholder list once the
// import is done.
func TestGetSession_ProcessingJobsField(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	session := createTestSessionForHandlers(t, h.DB, "processing-jobs-payload-test")

	insertJob := func(t *testing.T, state, source string, meeting, instance string) uuid.UUID {
		t.Helper()
		id := uuid.New()
		_, err := h.DB.Pool.Exec(ctx, `
			INSERT INTO session_processing_jobs
			(id, session_id, source, state, stage, meeting_uuid, instance_uuid, created_at, updated_at)
			VALUES ($1, $2, $3, $4, 'fetch', $5, $6, $7, $7)
		`, id, session.ID, source, state, meeting, instance, time.Now())
		require.NoError(t, err)
		return id
	}

	queued := insertJob(t, models.ProcessingStateQueued, "teams", "MSPB_meet_1", "rec-1")
	downloading := insertJob(t, models.ProcessingStateDownloading, "zoom", "zoom_m1", "zoom_i1")
	terminalReady := insertJob(t, models.ProcessingStateReady, "google_meet", "conf/A", "conf/A/rec/r")
	terminalFailed := insertJob(t, models.ProcessingStateFailedPermanent, "zoom", "zoom_m2", "zoom_i2")

	req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String(), nil)
	w := httptest.NewRecorder()
	h.GetSession(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp GetSessionResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	gotIDs := map[string]SessionProcessingJobView{}
	for _, j := range resp.ProcessingJobs {
		gotIDs[j.ID] = j
	}

	assert.Contains(t, gotIDs, queued.String(), "queued job must surface for the placeholder row")
	assert.Contains(t, gotIDs, downloading.String(), "downloading job must surface")
	assert.NotContains(t, gotIDs, terminalReady.String(), "ready job must NOT clutter the placeholder list")
	assert.NotContains(t, gotIDs, terminalFailed.String(), "failed_permanent job must NOT surface in the placeholder list (use a separate failed-imports surface)")

	// Field carries enough to render the row.
	q := gotIDs[queued.String()]
	assert.Equal(t, "teams", q.Source)
	assert.Equal(t, models.ProcessingStateQueued, q.State)
	require.NotNil(t, q.MeetingUUID)
	assert.Equal(t, "MSPB_meet_1", *q.MeetingUUID)
	require.NotNil(t, q.InstanceUUID)
	assert.Equal(t, "rec-1", *q.InstanceUUID)
}

func TestIsActiveProcessingState(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		models.ProcessingStateQueued,
		models.ProcessingStateFetching,
		models.ProcessingStateDownloading,
		models.ProcessingStateParsing,
		models.ProcessingStateChunking,
		models.ProcessingStateEmbedding,
		models.ProcessingStateAwaitingWhisper,
		models.ProcessingStateWaiting,
		models.ProcessingStateWaitingNativeTranscript,
		models.ProcessingStateFailedTransient,
	} {
		assert.True(t, isActiveProcessingState(s), "expected %q to be active (placeholder row visible)", s)
	}
	for _, s := range []string{
		models.ProcessingStateReady,
		models.ProcessingStateCanceled,
		models.ProcessingStateFailedPermanent,
	} {
		assert.False(t, isActiveProcessingState(s), "expected %q to be terminal (no placeholder row)", s)
	}
}
