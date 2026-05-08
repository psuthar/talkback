package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/auth"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCopySession_Baseline_HappyPath locks in the behavior of CopySession after
// the SCRUM-339 refactor. It seeds a source session with the DB-only categories
// (artifacts, session_links, video_sources without stored objects), invokes
// CopySession via the HTTP handler, and asserts the destination session has
// matching content with new IDs and no references back to the source.
//
// Storage-dependent paths (R2/local material copies, slide manifests, primary
// MP4 file_artifact copy) are exercised by SCRUM-345's data-driven test sweep
// using a fake storage.Interface; this baseline focuses on regression-locking
// the DB-only happy path.
func TestCopySession_Baseline_HappyPath(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	// Inject a stub IndexAsync to assert the orchestrator calls triggerIndex
	// for the destination (not the source).
	var indexCalls atomic.Int64
	var lastIndexedID atomic.Pointer[uuid.UUID]
	h.IndexAsync = func(id uuid.UUID) {
		indexCalls.Add(1)
		copyID := id
		lastIndexedID.Store(&copyID)
	}

	// Seed a source session as creator.
	createReqBody, _ := json.Marshal(map[string]string{"title": "Source Session"})
	createReq := httptest.NewRequest(http.MethodPost, "/api/sessions", bytes.NewReader(createReqBody))
	createReq.Header.Set("Content-Type", "application/json")
	creator := addUserSessionCookie(t, h, createReq, "copy-baseline@example.com")
	createW := httptest.NewRecorder()
	h.RequireAuth(h.CreateSession)(createW, createReq)
	require.Equal(t, http.StatusCreated, createW.Code, "create source session: %s", createW.Body.String())
	var createResp CreateSessionResponse
	require.NoError(t, json.Unmarshal(createW.Body.Bytes(), &createResp))
	srcSessionID, err := uuid.Parse(createResp.ID)
	require.NoError(t, err)

	// Seed: a second artifact (CreateSession already inserted one).
	srcArtifactsBefore, err := h.DB.GetArtifactsBySessionID(ctx, srcSessionID)
	require.NoError(t, err)
	require.Len(t, srcArtifactsBefore, 1, "CreateSession seeds one artifact")
	desc := "Topic notes"
	srcArtifact2, err := h.DB.CreateArtifact(ctx, srcSessionID, "Second Artifact", &desc)
	require.NoError(t, err)

	// Seed: a session_link.
	srcLink := &models.SessionLink{
		ID:        uuid.New(),
		SessionID: srcSessionID,
		URL:       "https://example.com/agenda",
		Status:    models.SessionLinkStatusVerified,
	}
	require.NoError(t, h.DB.CreateSessionLink(ctx, srcLink))

	// Seed: a video_source on the second artifact (no stored object → no
	// storage I/O exercised, just the DB rekey path).
	secondaryRole := models.VideoRoleSecondary
	srcVS := &models.VideoSource{
		ID:               uuid.New(),
		ArtifactID:       srcArtifact2.ID,
		SessionID:        srcSessionID,
		Provider:         "other",
		PlaybackMode:     "embed",
		SourceType:       "embed_url",
		TranscriptStatus: models.VideoTranscriptStatusMissing,
		VideoRole:        &secondaryRole,
	}
	require.NoError(t, h.DB.CreateVideoSource(ctx, srcVS))

	// Invoke CopySession via the HTTP handler.
	copyReq := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/api/sessions/%s/copy", srcSessionID.String()),
		bytes.NewReader([]byte(`{}`)),
	)
	copyReq.Header.Set("Content-Type", "application/json")
	addUserSessionCookieFor(t, h, copyReq, creator)
	copyW := httptest.NewRecorder()
	h.RequireAuth(h.CopySession)(copyW, copyReq)
	require.Equal(t, http.StatusCreated, copyW.Code, "CopySession: %s", copyW.Body.String())

	var copyResp CopySessionResponse
	require.NoError(t, json.Unmarshal(copyW.Body.Bytes(), &copyResp))
	assert.NotEmpty(t, copyResp.ID)
	assert.NotEqual(t, srcSessionID.String(), copyResp.ID, "destination must have a new ID")
	assert.Equal(t, "Copy of Source Session", copyResp.Title)
	require.NotNil(t, copyResp.CreatedBy)
	assert.Equal(t, creator.Email, *copyResp.CreatedBy)
	assert.Equal(t, "open", copyResp.Status)
	dstSessionID, err := uuid.Parse(copyResp.ID)
	require.NoError(t, err)

	// Artifacts: destination should have the same count, all with new IDs.
	dstArtifacts, err := h.DB.GetArtifactsBySessionID(ctx, dstSessionID)
	require.NoError(t, err)
	assert.Len(t, dstArtifacts, len(srcArtifactsBefore)+1, "all source artifacts copied")
	for _, a := range dstArtifacts {
		assert.Equal(t, dstSessionID, a.SessionID)
		for _, src := range srcArtifactsBefore {
			assert.NotEqual(t, src.ID, a.ID, "artifact ID must be new")
		}
		assert.NotEqual(t, srcArtifact2.ID, a.ID, "artifact ID must be new")
	}

	// Session links: destination should have one new link with same URL.
	dstLinks, err := h.DB.GetSessionLinksBySessionID(ctx, dstSessionID)
	require.NoError(t, err)
	require.Len(t, dstLinks, 1)
	assert.Equal(t, dstSessionID, dstLinks[0].SessionID)
	assert.NotEqual(t, srcLink.ID, dstLinks[0].ID, "link ID must be new")
	assert.Equal(t, srcLink.URL, dstLinks[0].URL, "URL copied verbatim")

	// Video sources: destination should have one new row pointing at a
	// new artifact and the destination session.
	dstVS, err := h.DB.GetVideoSourcesBySessionID(ctx, dstSessionID)
	require.NoError(t, err)
	require.Len(t, dstVS, 1)
	assert.Equal(t, dstSessionID, dstVS[0].SessionID)
	assert.NotEqual(t, srcVS.ID, dstVS[0].ID, "video_source ID must be new")
	assert.NotEqual(t, srcArtifact2.ID, dstVS[0].ArtifactID, "artifact_id must be remapped")
	// First copied source is forced to primary by ImportVideoSources.
	require.NotNil(t, dstVS[0].VideoRole)
	assert.Equal(t, models.VideoRolePrimary, *dstVS[0].VideoRole, "first copied video source is primary")

	// Index callback fired for the destination session.
	assert.GreaterOrEqual(t, indexCalls.Load(), int64(1), "triggerIndex must fire for destination")
	if last := lastIndexedID.Load(); assert.NotNil(t, last) {
		assert.Equal(t, dstSessionID, *last)
	}
}

// addUserSessionCookieFor reuses an existing user (instead of creating a new
// one) so the same creator can clone their own session.
func addUserSessionCookieFor(t *testing.T, h *Handlers, req *http.Request, user *models.User) {
	t.Helper()
	session := &models.LoginSession{
		ID:        uuid.New(),
		UserID:    user.ID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	require.NoError(t, h.DB.CreateLoginSession(context.Background(), session))
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: session.ID.String()})
}
