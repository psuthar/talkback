package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMaxRecordingsPerSession_DefaultsAndEnv pins the env-var parsing branches
// without spinning a DB.
func TestMaxRecordingsPerSession_DefaultsAndEnv(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want int
	}{
		{"unset returns default 10", "", defaultMaxRecordingsPerSession},
		{"unparseable returns default", "not-a-number", defaultMaxRecordingsPerSession},
		{"zero returns default", "0", defaultMaxRecordingsPerSession},
		{"negative returns default", "-5", defaultMaxRecordingsPerSession},
		{"explicit positive value honored", "25", 25},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if tc.env == "" {
				t.Setenv("MAX_RECORDINGS_PER_SESSION", "")
			} else {
				t.Setenv("MAX_RECORDINGS_PER_SESSION", tc.env)
			}
			assert.Equal(t, tc.want, maxRecordingsPerSession())
		})
	}
}

// TestCountActiveRecordingsForSession verifies the DB helper's exclusion
// rules: it counts non-failed recordings only (failed_permanent / canceled
// excluded), and includes ready + every non-terminal state.
func TestCountActiveRecordingsForSession(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	ctx := context.Background()
	session := createTestSessionForHandlers(t, h.DB, "cap counter session")

	insertArtifact := func(t *testing.T) uuid.UUID {
		t.Helper()
		id := uuid.New()
		_, err := h.DB.Pool.Exec(ctx, `
			INSERT INTO artifacts (id, session_id, title, status, created_at, updated_at)
			VALUES ($1, $2, 'a', 'ready', now(), now())
		`, id, session.ID)
		require.NoError(t, err)
		return id
	}

	insertRecording := func(t *testing.T, label string) uuid.UUID {
		t.Helper()
		artifactID := insertArtifact(t)
		id := uuid.New()
		_, err := h.DB.Pool.Exec(ctx, `
			INSERT INTO video_sources (id, artifact_id, session_id, provider, video_url, source_type, external_recording_id)
			VALUES ($1, $2, $3, 'zoom', $4, 'upload', $5)
		`, id, artifactID, session.ID, "https://example.com/"+label+".mp4", label)
		require.NoError(t, err)
		return id
	}

	insertJob := func(t *testing.T, meeting string, state string) {
		t.Helper()
		_, err := h.DB.Pool.Exec(ctx, `
			INSERT INTO session_processing_jobs (session_id, source, meeting_uuid, state, stage)
			VALUES ($1, 'zoom', $2, $3, 'fetch')
		`, session.ID, meeting, state)
		require.NoError(t, err)
	}

	// Seed: one ready, one in-progress, one failed_permanent, one canceled,
	// one with no job at all (counts as active).
	_ = insertRecording(t, "ready-rec")
	insertJob(t, "ready-rec", models.ProcessingStateReady)
	_ = insertRecording(t, "fetching-rec")
	insertJob(t, "fetching-rec", models.ProcessingStateFetching)
	_ = insertRecording(t, "failed-rec")
	insertJob(t, "failed-rec", models.ProcessingStateFailedPermanent)
	_ = insertRecording(t, "canceled-rec")
	insertJob(t, "canceled-rec", models.ProcessingStateCanceled)
	_ = insertRecording(t, "orphan-rec")
	// no job for orphan-rec

	n, err := h.DB.CountActiveRecordingsForSession(ctx, session.ID)
	require.NoError(t, err)
	// Active: ready, fetching, orphan-rec. Failed + canceled excluded.
	assert.Equal(t, 3, n)
}

// TestSessionImportZoom_RecordingCap drives the cap check end-to-end through
// the Zoom attach handler: at-cap returns 429; below-cap proceeds past the
// authz/dedupe/cap gate (downstream may 401/422/etc).
func TestSessionImportZoom_RecordingCap(t *testing.T) {
	t.Setenv("MAX_RECORDINGS_PER_SESSION", "3")
	t.Setenv("ENABLE_GOOGLE_MEET", "true")
	t.Setenv("ENABLE_TEAMS", "true")
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	ctx := context.Background()
	editor := &models.User{
		ID:          uuid.New(),
		Email:       "editor-" + uuid.NewString() + "@example.com",
		DisplayName: "Editor",
		GlobalRole:  models.GlobalRoleCreator,
		Status:      models.UserStatusActive,
	}
	require.NoError(t, h.DB.CreateUser(ctx, editor))

	session := createTestSessionForHandlers(t, h.DB, "cap-handler session")
	require.NoError(t, h.DB.CreateSessionMembership(ctx, session.ID, editor.ID, "creator", nil))

	// Seed 3 active recordings for the session — at cap.
	artifactID := uuid.New()
	_, err := h.DB.Pool.Exec(ctx, `
		INSERT INTO artifacts (id, session_id, title, status, created_at, updated_at)
		VALUES ($1, $2, 'a', 'ready', now(), now())
	`, artifactID, session.ID)
	require.NoError(t, err)
	for i := 0; i < 3; i++ {
		_, err := h.DB.Pool.Exec(ctx, `
			INSERT INTO video_sources (id, artifact_id, session_id, provider, video_url, source_type, external_recording_id)
			VALUES ($1, $2, $3, 'zoom', $4, 'upload', $5)
		`, uuid.New(), artifactID, session.ID, fmt.Sprintf("https://example.com/v%d.mp4", i), fmt.Sprintf("seed-%d", i))
		require.NoError(t, err)
		_, err = h.DB.Pool.Exec(ctx, `
			INSERT INTO session_processing_jobs (session_id, source, meeting_uuid, instance_uuid, state, stage)
			VALUES ($1, 'zoom', $2, $2, 'ready', 'ready')
		`, session.ID, fmt.Sprintf("seed-%d", i))
		require.NoError(t, err)
	}

	post := func(meetingUUID string) *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"meeting_uuid":%q}`, meetingUUID)
		req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID.String()+"/import/zoom", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Creator-Identity", "any-identity")
		req = req.WithContext(context.WithValue(req.Context(), userContextKey, editor))
		w := httptest.NewRecorder()
		h.SessionImportZoom(w, req)
		return w
	}

	t.Run("11th distinct attach (4th here, cap=3) returns 429", func(t *testing.T) {
		w := post("new-meeting-cap-test")
		require.Equal(t, http.StatusTooManyRequests, w.Code, w.Body.String())

		var body map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.Equal(t, "session_recording_cap_exceeded", body["error"])
		assert.EqualValues(t, 3, body["cap"])
		assert.EqualValues(t, 3, body["current"])
	})

	t.Run("dedupe of an existing recording does NOT 429 — it returns 200 already_imported", func(t *testing.T) {
		// "seed-0" is already in DB → dedupe should fire before cap.
		w := post("seed-0")
		// Dedupe path returns 200 already_imported even when session is at cap.
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "already_imported")
	})

	t.Run("below-cap attach passes the cap gate (downstream may still 4xx for OAuth)", func(t *testing.T) {
		// Drop the cap-floor by killing one recording's job state.
		_, err := h.DB.Pool.Exec(ctx, `UPDATE session_processing_jobs SET state='failed_permanent' WHERE session_id = $1 AND meeting_uuid = 'seed-0'`, session.ID)
		require.NoError(t, err)
		w := post("new-meeting-under-cap")
		// Not 429 — past the cap gate. The actual response depends on Zoom
		// OAuth token availability (typically 401 in test env).
		assert.NotEqual(t, http.StatusTooManyRequests, w.Code)
	})
}
