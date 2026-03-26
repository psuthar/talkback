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

// seedZoomJobForSession creates a session with SourceProvider=zoom and a corresponding
// session_processing_job with source="zoom" in the given state. Returns both.
func seedZoomJobForSession(t *testing.T, h *Handlers, title string, jobState string) (*models.Session, *models.SessionProcessingJob) {
	t.Helper()
	ctx := context.Background()

	sess := &models.Session{
		ID:             uuid.New(),
		Title:          title,
		Status:         models.SessionStatusOpen,
		SourceProvider: models.SessionSourceZoom,
	}
	require.NoError(t, h.DB.CreateSession(ctx, sess))

	meetingUUID := "test-meeting-" + sess.ID.String()[:8]
	creatorIdentity := "creator@smoke.test"
	job := &models.SessionProcessingJob{
		ID:              uuid.New(),
		SessionID:       sess.ID,
		Source:          "zoom",
		State:           jobState,
		Stage:           models.ProcessingStageFetch,
		MeetingUUID:     &meetingUUID,
		InstanceUUID:    &meetingUUID,
		CreatorIdentity: &creatorIdentity,
	}
	require.NoError(t, h.DB.CreateOrGetSessionProcessingJob(ctx, job))
	return sess, job
}

// TestSmoke_ProcessingStatus_NoJobReturnsEmptyNotError verifies that
// GET /api/sessions/:id/processing returns 200 with empty state/stage fields
// when no processing job exists for the session (typical for newly created sessions).
func TestSmoke_ProcessingStatus_NoJobReturnsEmptyNotError(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	// Use a session with source_provider=zoom so sessionSourceForID returns "zoom".
	ctx := context.Background()
	sess := &models.Session{
		ID:             uuid.New(),
		Title:          "Proc Status No Job",
		Status:         models.SessionStatusOpen,
		SourceProvider: models.SessionSourceZoom,
	}
	require.NoError(t, h.DB.CreateSession(ctx, sess))

	req := httptest.NewRequest(http.MethodGet,
		"/api/sessions/"+sess.ID.String()+"/processing", nil)
	w := httptest.NewRecorder()
	h.SessionProcessingStatus(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp ProcessingStatusResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Empty(t, resp.State, "state should be empty when no job exists")
	assert.Empty(t, resp.Stage, "stage should be empty when no job exists")
	assert.Empty(t, resp.UpdatedAt, "updated_at should be empty when no job exists")
}

// TestSmoke_ProcessingStatus_ZoomJobReturnsState verifies that
// GET /api/sessions/:id/processing returns the job state when a zoom job exists.
// This exercises the sessionSourceForID path: session.source_provider="zoom" -> source="zoom".
func TestSmoke_ProcessingStatus_ZoomJobReturnsState(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	sess, _ := seedZoomJobForSession(t, h, "Proc Status Zoom Job", models.ProcessingStateQueued)

	req := httptest.NewRequest(http.MethodGet,
		"/api/sessions/"+sess.ID.String()+"/processing", nil)
	w := httptest.NewRecorder()
	h.SessionProcessingStatus(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp ProcessingStatusResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, models.ProcessingStateQueued, resp.State)
	assert.NotEmpty(t, resp.UpdatedAt)
}

// TestSmoke_ProcessingStatus_UnknownSessionReturnsEmpty verifies that
// GET /api/sessions/:id/processing for a session ID that does not exist returns
// 200 with empty fields (not 404 or 500). The handler falls back to "zoom" source
// via sessionSourceForID on DB miss.
func TestSmoke_ProcessingStatus_UnknownSessionReturnsEmpty(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	nonexistentID := uuid.New()
	req := httptest.NewRequest(http.MethodGet,
		"/api/sessions/"+nonexistentID.String()+"/processing", nil)
	w := httptest.NewRecorder()
	h.SessionProcessingStatus(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp ProcessingStatusResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Empty(t, resp.State)
}

// TestSmoke_ProcessingRetry_ZoomJobCanBeRetried verifies that
// POST /api/sessions/:id/processing/retry transitions a failed_permanent zoom job
// back to queued and returns 200 with state=queued.
func TestSmoke_ProcessingRetry_ZoomJobCanBeRetried(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	sess, job := seedZoomJobForSession(t, h, "Proc Retry Zoom", models.ProcessingStateFailedPermanent)

	// Manually set to failed_permanent so retry can flip it back
	ctx := context.Background()
	code := "test_error"
	msg := "test permanent failure"
	require.NoError(t, h.DB.UpdateSessionProcessingJobState(
		ctx, job.ID, models.ProcessingStateFailedPermanent, models.ProcessingStageFetch,
		1, nil, &code, &msg,
	))

	req := httptest.NewRequest(http.MethodPost,
		"/api/sessions/"+sess.ID.String()+"/processing/retry", nil)
	w := httptest.NewRecorder()
	h.SessionProcessingRetry(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "queued", resp["state"])

	// Verify DB state changed to queued
	updated, err := h.DB.GetSessionProcessingJobBySessionID(ctx, sess.ID, "zoom")
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, models.ProcessingStateQueued, updated.State)
}

// TestSmoke_ProcessingCancel_ZoomJobCanBeCanceled verifies that
// POST /api/sessions/:id/processing/cancel transitions a queued zoom job
// to canceled and returns 200 with state=canceled.
func TestSmoke_ProcessingCancel_ZoomJobCanBeCanceled(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	sess, _ := seedZoomJobForSession(t, h, "Proc Cancel Zoom", models.ProcessingStateQueued)

	req := httptest.NewRequest(http.MethodPost,
		"/api/sessions/"+sess.ID.String()+"/processing/cancel", nil)
	w := httptest.NewRecorder()
	h.SessionProcessingCancel(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "canceled", resp["state"])

	// Verify DB state changed to canceled
	ctx := context.Background()
	updated, err := h.DB.GetSessionProcessingJobBySessionID(ctx, sess.ID, "zoom")
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, models.ProcessingStateCanceled, updated.State)
}

// seedTeamsJobForSession creates a session with SourceProvider=teams and a corresponding
// session_processing_job with source="teams" in the given state. Returns both.
func seedTeamsJobForSession(t *testing.T, h *Handlers, title string, jobState string) (*models.Session, *models.SessionProcessingJob) {
	t.Helper()
	ctx := context.Background()

	sess := &models.Session{
		ID:             uuid.New(),
		Title:          title,
		Status:         models.SessionStatusOpen,
		SourceProvider: models.SessionSourceTeams,
	}
	require.NoError(t, h.DB.CreateSession(ctx, sess))

	meetingUUID := "teams-meeting-" + sess.ID.String()[:8]
	creatorIdentity := "teams-creator@smoke.test"
	job := &models.SessionProcessingJob{
		ID:              uuid.New(),
		SessionID:       sess.ID,
		Source:          "teams",
		State:           jobState,
		Stage:           models.ProcessingStageFetch,
		MeetingUUID:     &meetingUUID,
		InstanceUUID:    &meetingUUID,
		CreatorIdentity: &creatorIdentity,
	}
	require.NoError(t, h.DB.CreateOrGetSessionProcessingJob(ctx, job))
	return sess, job
}

// TestSmoke_ProcessingRetry_TeamsJobCanBeRetried verifies that
// POST /api/sessions/:id/processing/retry transitions a failed_permanent teams job
// back to queued and returns 200 with state=queued.
func TestSmoke_ProcessingRetry_TeamsJobCanBeRetried(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	sess, job := seedTeamsJobForSession(t, h, "Proc Retry Teams", models.ProcessingStateFailedPermanent)

	// Move to failed_permanent so retry can flip it back.
	ctx := context.Background()
	code := "teams_not_configured"
	msg := "Teams token resolver not configured"
	require.NoError(t, h.DB.UpdateSessionProcessingJobState(
		ctx, job.ID, models.ProcessingStateFailedPermanent, models.ProcessingStageFetch,
		1, nil, &code, &msg,
	))

	req := httptest.NewRequest(http.MethodPost,
		"/api/sessions/"+sess.ID.String()+"/processing/retry", nil)
	w := httptest.NewRecorder()
	h.SessionProcessingRetry(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "queued", resp["state"])

	// Verify DB state changed to queued.
	updated, err := h.DB.GetSessionProcessingJobBySessionID(ctx, sess.ID, "teams")
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, models.ProcessingStateQueued, updated.State)
}

// TestSmoke_ProcessingImport_TeamsCancel verifies that
// POST /api/sessions/:id/processing/cancel transitions a queued teams job
// to canceled and returns 200 with state=canceled.
func TestSmoke_ProcessingImport_TeamsCancel(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	sess, _ := seedTeamsJobForSession(t, h, "Proc Cancel Teams", models.ProcessingStateQueued)

	req := httptest.NewRequest(http.MethodPost,
		"/api/sessions/"+sess.ID.String()+"/processing/cancel", nil)
	w := httptest.NewRecorder()
	h.SessionProcessingCancel(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "canceled", resp["state"])

	// Verify DB state changed to canceled.
	ctx := context.Background()
	updated, err := h.DB.GetSessionProcessingJobBySessionID(ctx, sess.ID, "teams")
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, models.ProcessingStateCanceled, updated.State)
}

// TestSmoke_ProcessingStatus_TeamsJobDerivesSource verifies that
// GET /api/sessions/:id/processing for a teams session finds the teams job
// (not a zoom job) via sessionSourceForID. This proves the source derivation
// path is not hardcoded to "zoom".
func TestSmoke_ProcessingStatus_TeamsJobDerivesSource(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	ctx := context.Background()

	// Create a teams session
	sess := &models.Session{
		ID:             uuid.New(),
		Title:          "Proc Status Teams Job",
		Status:         models.SessionStatusOpen,
		SourceProvider: models.SessionSourceTeams,
	}
	require.NoError(t, h.DB.CreateSession(ctx, sess))

	// Seed a teams job (not zoom) in failed_permanent state
	creatorIdentity := "teams-creator@smoke.test"
	nextRetry := time.Now().Add(time.Hour)
	code := "teams_not_configured"
	msg := "Teams token resolver not configured"
	job := &models.SessionProcessingJob{
		ID:              uuid.New(),
		SessionID:       sess.ID,
		Source:          "teams",
		State:           models.ProcessingStateQueued,
		Stage:           models.ProcessingStageFetch,
		CreatorIdentity: &creatorIdentity,
	}
	require.NoError(t, h.DB.CreateOrGetSessionProcessingJob(ctx, job))
	// Move to failed_permanent to simulate pipeline dispatch result
	require.NoError(t, h.DB.UpdateSessionProcessingJobState(
		ctx, job.ID, models.ProcessingStateFailedPermanent, models.ProcessingStageFetch,
		1, &nextRetry, &code, &msg,
	))

	req := httptest.NewRequest(http.MethodGet,
		"/api/sessions/"+sess.ID.String()+"/processing", nil)
	w := httptest.NewRecorder()
	h.SessionProcessingStatus(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp ProcessingStatusResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	// Handler must find the teams job (not return empty due to looking for zoom job)
	assert.Equal(t, models.ProcessingStateFailedPermanent, resp.State,
		"teams session must find the teams job, not return empty state")
	assert.Equal(t, "teams_not_configured", resp.LastErrorCode)
}
