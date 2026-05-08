package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SCRUM-353: optional async mode for CopySession.
//
// The synchronous path is unchanged (covered by the SCRUM-339/345 tests).
// These tests cover the new async branch:
//   - ?async=true returns 202 + {job_id, session_id}.
//   - The spawned goroutine drives the job to a terminal state.
//   - Polling GET /api/import-jobs/:id reflects the terminal state.

func TestCopySession_AsyncMode_Returns202WithJobID(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	// Seed a source session as creator.
	createBody, _ := json.Marshal(map[string]string{"title": "Async Source"})
	createReq := httptest.NewRequest(http.MethodPost, "/api/sessions", bytes.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	creator := addUserSessionCookie(t, h, createReq, "copy-async@example.com")
	createW := httptest.NewRecorder()
	h.RequireAuth(h.CreateSession)(createW, createReq)
	require.Equal(t, http.StatusCreated, createW.Code, "create source: %s", createW.Body.String())
	var srcResp CreateSessionResponse
	require.NoError(t, json.Unmarshal(createW.Body.Bytes(), &srcResp))
	srcSessionID, err := uuid.Parse(srcResp.ID)
	require.NoError(t, err)

	// Async copy.
	copyReq := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/api/sessions/%s/copy?async=true", srcSessionID.String()),
		bytes.NewReader([]byte(`{}`)),
	)
	copyReq.Header.Set("Content-Type", "application/json")
	addUserSessionCookieFor(t, h, copyReq, creator)
	copyW := httptest.NewRecorder()
	h.RequireAuth(h.CopySession)(copyW, copyReq)

	require.Equal(t, http.StatusAccepted, copyW.Code, "async copy must return 202: %s", copyW.Body.String())
	var asyncResp map[string]string
	require.NoError(t, json.Unmarshal(copyW.Body.Bytes(), &asyncResp))
	require.Contains(t, asyncResp, "job_id")
	require.Contains(t, asyncResp, "session_id")

	jobID, err := uuid.Parse(asyncResp["job_id"])
	require.NoError(t, err)

	// Wait for the spawned goroutine to drive the job to a terminal state.
	// The work is small (no source content beyond the auto-created artifact)
	// so this should complete in well under a second.
	deadline := time.Now().Add(5 * time.Second)
	var got *models.ImportJob
	for time.Now().Before(deadline) {
		got, err = h.DB.GetImportJob(ctx, jobID)
		require.NoError(t, err)
		require.NotNil(t, got)
		if got.State == models.ImportJobStateSucceeded || got.State == models.ImportJobStatePartial || got.State == models.ImportJobStateFailed {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	require.NotNil(t, got)
	assert.Contains(t, []models.ImportJobState{models.ImportJobStateSucceeded, models.ImportJobStatePartial}, got.State,
		"async copy job must reach a terminal success/partial state; got=%s err=%v", got.State, got.ErrorMessage)
	require.NotNil(t, got.SessionID)
}

func TestCopySession_SyncMode_Unchanged(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	createBody, _ := json.Marshal(map[string]string{"title": "Sync Source"})
	createReq := httptest.NewRequest(http.MethodPost, "/api/sessions", bytes.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	creator := addUserSessionCookie(t, h, createReq, "copy-sync@example.com")
	createW := httptest.NewRecorder()
	h.RequireAuth(h.CreateSession)(createW, createReq)
	require.Equal(t, http.StatusCreated, createW.Code)
	var srcResp CreateSessionResponse
	require.NoError(t, json.Unmarshal(createW.Body.Bytes(), &srcResp))

	// No ?async — must take the synchronous path and return 201 with the
	// session payload (byte-identical to pre-SCRUM-353 behavior).
	copyReq := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/api/sessions/%s/copy", srcResp.ID),
		bytes.NewReader([]byte(`{}`)),
	)
	copyReq.Header.Set("Content-Type", "application/json")
	addUserSessionCookieFor(t, h, copyReq, creator)
	copyW := httptest.NewRecorder()
	h.RequireAuth(h.CopySession)(copyW, copyReq)

	require.Equal(t, http.StatusCreated, copyW.Code, "sync path must still return 201: %s", copyW.Body.String())
	var syncResp CopySessionResponse
	require.NoError(t, json.Unmarshal(copyW.Body.Bytes(), &syncResp))
	assert.NotEmpty(t, syncResp.ID)
	assert.NotEmpty(t, syncResp.Title)
	assert.Equal(t, "open", syncResp.Status)
}
