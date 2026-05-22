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

func TestCreateSession(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	t.Run("returns 401 when not authenticated", func(t *testing.T) {
		reqBody := map[string]interface{}{"title": "Weekly Review"}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.RequireAuth(h.CreateSession)(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("returns 403 when authenticated as participant", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader([]byte(`{"title":"Weekly Review"}`)))
		req.Header.Set("Content-Type", "application/json")
		addParticipantSessionCookie(t, h, req)
		w := httptest.NewRecorder()

		h.RequireAuth(h.CreateSession)(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("creates session with title when authenticated as creator", func(t *testing.T) {
		reqBody := map[string]interface{}{"title": "Weekly Review"}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		creator := addUserSessionCookie(t, h, req, "creator@example.com")
		w := httptest.NewRecorder()

		h.RequireAuth(h.CreateSession)(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		var response CreateSessionResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.NotEmpty(t, response.ID)
		assert.Equal(t, "Weekly Review", response.Title)
		assert.Equal(t, "open", response.Status)
		require.NotNil(t, response.CreatedBy)
		assert.Equal(t, creator.Email, *response.CreatedBy)
	})

	t.Run("creates session with title and created_by (body ignored, uses auth user)", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"title":      "Design Review",
			"created_by": "Paresh",
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		creator := addUserSessionCookie(t, h, req, "creator2@example.com")
		w := httptest.NewRecorder()

		h.RequireAuth(h.CreateSession)(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		var response CreateSessionResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.NotEmpty(t, response.ID)
		assert.Equal(t, "Design Review", response.Title)
		require.NotNil(t, response.CreatedBy)
		assert.Equal(t, creator.Email, *response.CreatedBy)
		assert.Equal(t, "open", response.Status)
	})

	t.Run("returns 400 when title is missing", func(t *testing.T) {
		reqBody := map[string]interface{}{}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		addUserSessionCookie(t, h, req, "creator3@example.com")
		w := httptest.NewRecorder()

		h.RequireAuth(h.CreateSession)(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestGetArtifactsBySessionID(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	db := h.DB
	ctx := context.Background()

	// Create a session
	session := createTestSessionForHandlers(t, h.DB, "Test Session")

	t.Run("returns empty list for session with no artifacts", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"/artifacts", nil)
		w := httptest.NewRecorder()

		h.GetArtifactsBySession(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		artifactsRaw, ok := response["artifacts"]
		require.True(t, ok, "Response should have 'artifacts' key")

		// Handle nil, empty slice, or populated slice
		if artifactsRaw == nil {
			// nil is acceptable for empty list
			return
		}

		artifacts, ok := artifactsRaw.([]interface{})
		if !ok {
			// Try as []map[string]interface{} (when unmarshaling structs)
			artifactsArray, ok2 := artifactsRaw.([]map[string]interface{})
			if ok2 {
				assert.Empty(t, artifactsArray, "Artifacts should be empty")
				return
			}
			t.Fatalf("artifacts is not []interface{} or []map[string]interface{}, got %T: %v", artifactsRaw, artifactsRaw)
		}
		assert.Empty(t, artifacts)
	})

	t.Run("returns artifacts for session", func(t *testing.T) {
		// Create artifacts
		artifact1, err := db.CreateArtifact(ctx, session.ID, "Artifact 1", nil)
		require.NoError(t, err)
		artifact2, err := db.CreateArtifact(ctx, session.ID, "Artifact 2", nil)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"/artifacts", nil)
		w := httptest.NewRecorder()

		h.GetArtifactsBySession(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		artifacts, ok := response["artifacts"].([]interface{})
		require.True(t, ok)
		assert.Len(t, artifacts, 2)
		// Verify artifact IDs are in the response
		artifactIDs := []string{artifact1.ID.String(), artifact2.ID.String()}
		for _, art := range artifacts {
			artMap := art.(map[string]interface{})
			assert.Contains(t, artifactIDs, artMap["id"].(string))
		}
	})
}

func TestGetSession(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	db := h.DB
	ctx := context.Background()

	// Create session and artifacts
	session := createTestSessionForHandlers(t, h.DB, "Test Session")
	artifact1, err := db.CreateArtifact(ctx, session.ID, "Test Artifact 1", nil)
	require.NoError(t, err)
	_, err = db.CreateArtifact(ctx, session.ID, "Test Artifact 2", nil)
	require.NoError(t, err)

	// Create materials and video sources for artifacts
	material := &models.Material{
		ID:            uuid.New(),
		ArtifactID:    artifact1.ID,
		SessionID:     session.ID,
		Kind:          string(models.MaterialKindDocument),
		Filename:      "test.txt",
		ContentType:   "text/plain",
		StorageURL:    "./test.txt",
		TextStatus:    models.MaterialTextStatusReady,
		ExtractedText: stringPtr("Test content"),
	}
	err = db.CreateMaterial(ctx, material)
	require.NoError(t, err)

	embedURL := "https://example.com/share/test"
	videoSource := &models.VideoSource{
		ID:               uuid.New(),
		ArtifactID:       artifact1.ID,
		SessionID:        session.ID,
		Provider:         string(models.VideoProviderOther),
		VideoURL:         embedURL,
		PlaybackMode:     "embed",
		EmbedURL:         &embedURL,
		TranscriptStatus: models.VideoTranscriptStatusReady,
		TranscriptText:   stringPtr("Video transcript"),
	}
	err = db.CreateVideoSource(ctx, videoSource)
	require.NoError(t, err)

	t.Run("returns session with artifacts context", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String(), nil)
		w := httptest.NewRecorder()

		h.GetSession(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var response GetSessionResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.NotNil(t, response.Session)
		assert.Equal(t, session.ID, response.Session.ID)
		assert.Len(t, response.Artifacts, 2)
		assert.Len(t, response.Materials, 1)
		assert.Len(t, response.VideoSources, 1)
		// Primary/additional: with no video_role set, effective primary is first video and additional is empty
		require.NotNil(t, response.PrimaryVideo)
		assert.Equal(t, videoSource.ID, response.PrimaryVideo.ID)
		assert.Empty(t, response.AdditionalVideos)
	})

	t.Run("returns 404 for non-existent session", func(t *testing.T) {
		nonExistentID := uuid.New()
		req := httptest.NewRequest(http.MethodGet, "/sessions/"+nonExistentID.String(), nil)
		w := httptest.NewRecorder()

		h.GetSession(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("returns creator mode when current user matches created_by", func(t *testing.T) {
		ctx := context.Background()
		creatorName := "test-creator"
		session := &models.Session{
			ID:        uuid.New(),
			Title:     "Creator Session",
			CreatedBy: &creatorName,
			Status:    models.SessionStatusOpen,
		}
		err := h.DB.CreateSession(ctx, session)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"?user="+creatorName, nil)
		req.Header.Set("X-Current-User", creatorName)
		w := httptest.NewRecorder()

		h.GetSession(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var response GetSessionResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, "creator", response.Mode)
	})

	t.Run("returns participant mode when current user does not match created_by", func(t *testing.T) {
		ctx := context.Background()
		creatorName := "test-creator"
		participantName := "test-participant"
		session := &models.Session{
			ID:        uuid.New(),
			Title:     "Creator Session",
			CreatedBy: &creatorName,
			Status:    models.SessionStatusOpen,
		}
		err := h.DB.CreateSession(ctx, session)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"?user="+participantName, nil)
		req.Header.Set("X-Current-User", participantName)
		w := httptest.NewRecorder()

		h.GetSession(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var response GetSessionResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, "participant", response.Mode)
	})

	t.Run("returns participant mode when created_by is null", func(t *testing.T) {
		ctx := context.Background()
		session := &models.Session{
			ID:        uuid.New(),
			Title:     "No Creator Session",
			CreatedBy: nil,
			Status:    models.SessionStatusOpen,
		}
		err := h.DB.CreateSession(ctx, session)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"?user=anyone", nil)
		w := httptest.NewRecorder()

		h.GetSession(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var response GetSessionResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, "participant", response.Mode)
	})
}

// TestGetSession_PrimaryDescriptor exercises the SCRUM-271 resolved primary
// block in the GET response: legacy video-first sessions still report
// kind=video without DB writes; document-first sessions report kind=document
// with the material's title; sessions without any primary omit the field.
func TestGetSession_PrimaryDescriptor(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("omits Primary when no primary set", func(t *testing.T) {
		session := createTestSessionForHandlers(t, h.DB, "no-primary session")

		req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String(), nil)
		w := httptest.NewRecorder()
		h.GetSession(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		var response GetSessionResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		assert.Nil(t, response.Primary, "Primary must be nil/omitted when no primary is set")
	})

	t.Run("legacy video fallback returns kind=video", func(t *testing.T) {
		session := createTestSessionForHandlers(t, h.DB, "legacy video session")

		// Create a file_artifact and point primary_video_artifact_id at it without
		// setting primary_content_kind — exactly the existing-row state the
		// SCRUM-269 backward-compat note describes.
		artifactID := uuid.New()
		filename := "legacy.mp4"
		_, err := h.DB.Pool.Exec(ctx, `
			INSERT INTO file_artifacts (id, session_id, kind, filename, content_type,
			                            storage_provider, storage_bucket, storage_key, status,
			                            created_at, updated_at)
			VALUES ($1, $2, 'video', $3, 'video/mp4', 'r2', 'b', $4, 'ready', now(), now())
		`, artifactID, session.ID, filename, artifactID.String())
		require.NoError(t, err)
		_, err = h.DB.Pool.Exec(ctx,
			`UPDATE sessions SET primary_video_artifact_id = $1 WHERE id = $2`, artifactID, session.ID)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String(), nil)
		w := httptest.NewRecorder()
		h.GetSession(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		var response GetSessionResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		require.NotNil(t, response.Primary)
		assert.Equal(t, "video", response.Primary.Kind)
		assert.Equal(t, artifactID, response.Primary.ID)
		// Title is enriched from the file_artifact's filename when available.
		require.NotNil(t, response.Primary.Title)
		assert.Equal(t, filename, *response.Primary.Title)
		require.NotNil(t, response.Primary.Status)
		assert.Equal(t, "ready", *response.Primary.Status)
	})

	t.Run("explicit document primary returns kind=document with title", func(t *testing.T) {
		session := createTestSessionForHandlers(t, h.DB, "document-primary session")

		artifact, err := h.DB.CreateArtifact(ctx, session.ID, "doc artifact", nil)
		require.NoError(t, err)
		material := &models.Material{
			ID:            uuid.New(),
			ArtifactID:    artifact.ID,
			SessionID:     session.ID,
			Kind:          string(models.MaterialKindDocument),
			Filename:      "spec.pdf",
			ContentType:   "application/pdf",
			StorageURL:    "r2://bucket/spec.pdf",
			TextStatus:    models.MaterialTextStatusReady,
			ExtractedText: stringPtr("text"),
		}
		require.NoError(t, h.DB.CreateMaterial(ctx, material))

		_, err = h.DB.Pool.Exec(ctx, `
			UPDATE sessions
			SET primary_content_kind = 'document', primary_material_id = $1
			WHERE id = $2
		`, material.ID, session.ID)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String(), nil)
		w := httptest.NewRecorder()
		h.GetSession(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		var response GetSessionResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		require.NotNil(t, response.Primary)
		assert.Equal(t, "document", response.Primary.Kind)
		assert.Equal(t, material.ID, response.Primary.ID)
		require.NotNil(t, response.Primary.Title)
		assert.Equal(t, "spec.pdf", *response.Primary.Title)
		require.NotNil(t, response.Primary.Status)
		assert.Equal(t, "ready", *response.Primary.Status)
	})
}

func TestUpsertSessionParticipant(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	// Create session
	session := createTestSessionForHandlers(t, h.DB, "Test Session")

	t.Run("joins session as new participant", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"participant_ref": "anonymous",
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/sessions/"+session.ID.String()+"/participants", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.JoinSessionParticipant(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var participant models.SessionParticipant
		err := json.Unmarshal(w.Body.Bytes(), &participant)
		require.NoError(t, err)
		assert.Equal(t, session.ID, participant.SessionID)
		assert.Equal(t, "anonymous", participant.ParticipantRef)
	})

	t.Run("updates existing participant", func(t *testing.T) {
		// Join first time
		reqBody1 := map[string]interface{}{
			"participant_ref": "user1",
		}
		body1, _ := json.Marshal(reqBody1)
		req1 := httptest.NewRequest(http.MethodPost, "/sessions/"+session.ID.String()+"/participants", bytes.NewReader(body1))
		req1.Header.Set("Content-Type", "application/json")
		w1 := httptest.NewRecorder()
		h.JoinSessionParticipant(w1, req1)
		assert.Equal(t, http.StatusOK, w1.Code)

		// Join again (should update last_seen_at)
		reqBody2 := map[string]interface{}{
			"participant_ref": "user1",
		}
		body2, _ := json.Marshal(reqBody2)
		req2 := httptest.NewRequest(http.MethodPost, "/sessions/"+session.ID.String()+"/participants", bytes.NewReader(body2))
		req2.Header.Set("Content-Type", "application/json")
		w2 := httptest.NewRecorder()
		h.JoinSessionParticipant(w2, req2)

		assert.Equal(t, http.StatusOK, w2.Code)
		var participant models.SessionParticipant
		err := json.Unmarshal(w2.Body.Bytes(), &participant)
		require.NoError(t, err)
		assert.Equal(t, "user1", participant.ParticipantRef)
	})

	t.Run("returns 400 when participant_ref is missing", func(t *testing.T) {
		reqBody := map[string]interface{}{}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/sessions/"+session.ID.String()+"/participants", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.JoinSessionParticipant(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestCreateSessionEvent(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	// Create session
	session := createTestSessionForHandlers(t, h.DB, "Test Session")

	t.Run("creates play event", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"participant_ref":    "anonymous",
			"event_type":         "play",
			"video_time_seconds": 120,
			"payload":            map[string]interface{}{},
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/sessions/"+session.ID.String()+"/events", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.CreateSessionEvent(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		var event models.SessionEvent
		err := json.Unmarshal(w.Body.Bytes(), &event)
		require.NoError(t, err)
		assert.Equal(t, session.ID, event.SessionID)
		assert.Equal(t, models.SessionEventTypePlay, event.EventType)
		assert.Equal(t, 120, *event.VideoTimeSeconds)
	})

	t.Run("creates seek event", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"participant_ref":    "anonymous",
			"event_type":         "seek",
			"video_time_seconds": 300,
			"payload":            map[string]interface{}{},
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/sessions/"+session.ID.String()+"/events", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.CreateSessionEvent(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
	})

	t.Run("creates join event", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"participant_ref": "user1",
			"event_type":      "join",
			"payload":         map[string]interface{}{},
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/sessions/"+session.ID.String()+"/events", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.CreateSessionEvent(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
	})
}

func TestAskSessionQuestion(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	db := h.DB
	ctx := context.Background()

	// Create session and artifact with content
	session := createTestSessionForHandlers(t, h.DB, "Test Session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "Test Artifact", nil)
	require.NoError(t, err)

	material := &models.Material{
		ID:            uuid.New(),
		ArtifactID:    artifact.ID,
		SessionID:     session.ID,
		Kind:          string(models.MaterialKindDocument),
		Filename:      "test.txt",
		ContentType:   "text/plain",
		StorageURL:    "./test.txt",
		TextStatus:    models.MaterialTextStatusReady,
		ExtractedText: stringPtr("This document discusses machine learning and neural networks."),
	}
	err = db.CreateMaterial(ctx, material)
	require.NoError(t, err)

	t.Run("asks question in session context", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"question_text": "What is this about?",
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/sessions/"+session.ID.String()+"/questions", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.AskSessionQuestion(w, req)

		// Should succeed (may return answered or not_covered depending on LLM)
		assert.True(t, w.Code == http.StatusCreated || w.Code == http.StatusOK)
		var response AskQuestionResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.NotNil(t, response.Question)
		assert.NotNil(t, response.Answer)
		assert.Equal(t, session.ID, response.Question.SessionID)
	})

	t.Run("asks question with video_time_seconds", func(t *testing.T) {
		videoTimeSeconds := 300
		reqBody := map[string]interface{}{
			"question_text":      "What was discussed at 5 minutes?",
			"video_time_seconds": videoTimeSeconds,
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/sessions/"+session.ID.String()+"/questions", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.AskSessionQuestion(w, req)

		// Should succeed
		assert.True(t, w.Code == http.StatusCreated || w.Code == http.StatusOK)
		var response AskQuestionResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.NotNil(t, response.Question)
		assert.NotNil(t, response.Answer)
		// Verify video_time_seconds is stored
		assert.NotNil(t, response.Question.VideoTimeSeconds)
		assert.Equal(t, videoTimeSeconds, *response.Question.VideoTimeSeconds)
	})

	t.Run("returns 400 when question_text is missing", func(t *testing.T) {
		reqBody := map[string]interface{}{}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/sessions/"+session.ID.String()+"/questions", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.AskSessionQuestion(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestAskSessionQuestion_LimitReached(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	db := h.DB
	ctx := context.Background()

	oldMax := auth.Config.MaxQuestionsPerSession
	auth.Config.MaxQuestionsPerSession = 2
	defer func() { auth.Config.MaxQuestionsPerSession = oldMax }()

	session := createTestSessionForHandlers(t, h.DB, "Limit Test Session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "Test Artifact", nil)
	require.NoError(t, err)

	// Create 2 questions directly so session is at limit
	for i := 0; i < 2; i++ {
		q := &models.Question{
			ID:             uuid.New(),
			ArtifactID:     artifact.ID,
			SessionID:      session.ID,
			QuestionText:   fmt.Sprintf("Question %d?", i+1),
			QuestionSource: models.QuestionSourceText,
		}
		require.NoError(t, db.CreateQuestion(ctx, q))
	}

	reqBody := map[string]interface{}{"question_text": "Third question?"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/sessions/"+session.ID.String()+"/questions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.AskSessionQuestion(w, req)

	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "session question limit reached", resp["error"])
	assert.EqualValues(t, 2, resp["max_questions"])
}

func TestGetSessionQuestions(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	db := h.DB
	ctx := context.Background()

	// Create session and artifact
	session := createTestSessionForHandlers(t, h.DB, "Test Session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "Test Artifact", nil)
	require.NoError(t, err)

	t.Run("returns empty list for session with no questions", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"/questions", nil)
		w := httptest.NewRecorder()

		h.GetSessionQuestions(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var response GetQuestionsResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Empty(t, response.Questions)
		assert.Empty(t, response.Answers)
	})

	t.Run("returns questions with answers for session", func(t *testing.T) {
		// Create session-scoped question
		question := &models.Question{
			ID:               uuid.New(),
			ArtifactID:       artifact.ID,
			SessionID:        session.ID, // SessionID is required (non-pointer)
			QuestionText:     "Test question?",
			QuestionSource:   models.QuestionSourceText,
			VideoTimeSeconds: nil,
		}
		err := db.CreateQuestion(ctx, question)
		require.NoError(t, err)

		answer := &models.Answer{
			ID:           uuid.New(),
			QuestionID:   question.ID,
			AnswerText:   "Test answer",
			AnswerStatus: models.AnswerStatusAnswered,
			Confidence:   0.8,
			Citations:    []models.Citation{},
		}
		err = db.CreateAnswer(ctx, answer)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"/questions", nil)
		w := httptest.NewRecorder()

		h.GetSessionQuestions(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var response GetQuestionsResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Len(t, response.Questions, 1)
		assert.Len(t, response.Answers, 1)
		assert.Equal(t, question.ID, response.Questions[0].ID)
		assert.Equal(t, answer.ID, response.Answers[0].ID)
		assert.Equal(t, session.ID, response.Questions[0].SessionID)
	})

	t.Run("returns questions in thread order with parent_question_id", func(t *testing.T) {
		rootQ := &models.Question{
			ID:             uuid.New(),
			ArtifactID:     artifact.ID,
			SessionID:      session.ID,
			QuestionText:   "Root question?",
			QuestionSource: models.QuestionSourceText,
		}
		err := db.CreateQuestion(ctx, rootQ)
		require.NoError(t, err)

		replyQ := &models.Question{
			ID:                uuid.New(),
			ArtifactID:        artifact.ID,
			SessionID:         session.ID,
			ParentQuestionID:  &rootQ.ID,
			QuestionText:      "Reply question?",
			QuestionSource:    models.QuestionSourceText,
		}
		err = db.CreateQuestion(ctx, replyQ)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"/questions", nil)
		w := httptest.NewRecorder()
		h.GetSessionQuestions(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var response GetQuestionsResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Len(t, response.Questions, 3, "previous subtest question + root + reply") // 1 from "returns questions with answers" + root + reply
		// Last two should be root then reply (thread order)
		var rootFound, replyFound bool
		for _, q := range response.Questions {
			if q.ID == rootQ.ID {
				rootFound = true
				assert.Nil(t, q.ParentQuestionID)
			}
			if q.ID == replyQ.ID {
				replyFound = true
				require.NotNil(t, q.ParentQuestionID)
				assert.Equal(t, rootQ.ID, *q.ParentQuestionID)
			}
		}
		assert.True(t, rootFound)
		assert.True(t, replyFound)
	})
}

func TestUpdateSessionStatus(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	db := h.DB
	ctx := context.Background()

	// Create a user and session so we have a creator; reuse the same cookie for all subtests
	req0 := httptest.NewRequest(http.MethodPatch, "/sessions/"+uuid.New().String(), bytes.NewReader([]byte("{}")))
	creator := addUserSessionCookie(t, h, req0, "creator-update@example.com")
	session := &models.Session{
		ID:        uuid.New(),
		Title:     "Session to Update",
		CreatedBy: &creator.Email,
		Status:    models.SessionStatusOpen,
	}
	err := db.CreateSession(ctx, session)
	require.NoError(t, err)
	assert.Equal(t, models.SessionStatusOpen, session.Status)

	addCookieToRequest := func(req *http.Request) {
		for _, c := range req0.Cookies() {
			req.AddCookie(c)
		}
	}

	t.Run("updates session status to closed", func(t *testing.T) {
		reqBody := map[string]string{"status": "closed"}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPatch, "/sessions/"+session.ID.String(), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		addCookieToRequest(req)
		w := httptest.NewRecorder()

		h.RequireAuth(h.UpdateSessionStatus)(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var updatedSession models.Session
		err := json.Unmarshal(w.Body.Bytes(), &updatedSession)
		require.NoError(t, err)
		assert.Equal(t, models.SessionStatusClosed, updatedSession.Status)
		assert.Equal(t, session.ID, updatedSession.ID)

		// Verify in database
		dbSession, err := db.GetSession(ctx, session.ID)
		require.NoError(t, err)
		assert.Equal(t, models.SessionStatusClosed, dbSession.Status)
		assert.True(t, dbSession.UpdatedAt.After(session.UpdatedAt))
	})

	t.Run("updates session status back to open", func(t *testing.T) {
		reqBody := map[string]string{"status": "open"}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPatch, "/sessions/"+session.ID.String(), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		addCookieToRequest(req)
		w := httptest.NewRecorder()

		h.RequireAuth(h.UpdateSessionStatus)(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var updatedSession models.Session
		err := json.Unmarshal(w.Body.Bytes(), &updatedSession)
		require.NoError(t, err)
		assert.Equal(t, models.SessionStatusOpen, updatedSession.Status)
	})

	t.Run("returns 400 for invalid status", func(t *testing.T) {
		reqBody := map[string]string{"status": "invalid"}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPatch, "/sessions/"+session.ID.String(), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		addCookieToRequest(req)
		w := httptest.NewRecorder()

		h.RequireAuth(h.UpdateSessionStatus)(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Invalid status")
	})

	t.Run("returns 400 for invalid session ID", func(t *testing.T) {
		reqBody := map[string]string{"status": "closed"}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPatch, "/sessions/invalid-id", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		addCookieToRequest(req)
		w := httptest.NewRecorder()

		h.RequireAuth(h.UpdateSessionStatus)(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("returns 404 for non-existent session", func(t *testing.T) {
		nonExistentID := uuid.New()
		reqBody := map[string]string{"status": "closed"}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPatch, "/sessions/"+nonExistentID.String(), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		addCookieToRequest(req)
		w := httptest.NewRecorder()

		h.RequireAuth(h.UpdateSessionStatus)(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("returns 405 for non-PATCH/PUT methods", func(t *testing.T) {
		reqBody := map[string]string{"status": "closed"}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String(), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		addCookieToRequest(req)
		w := httptest.NewRecorder()

		h.RequireAuth(h.UpdateSessionStatus)(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("returns 400 for invalid request body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/sessions/"+session.ID.String(), bytes.NewReader([]byte("invalid json")))
		req.Header.Set("Content-Type", "application/json")
		addCookieToRequest(req)
		w := httptest.NewRecorder()

		h.RequireAuth(h.UpdateSessionStatus)(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestUpdateSessionStatus_TriggersReindex (SCRUM-558) locks in the contract that title /
// status / premise / primary_decision / decision_outcome edits fire IndexAsync so the
// session_metadata RAG chunk stays fresh. No-op mutations don't trigger.
func TestUpdateSessionStatus_TriggersReindex(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	var indexCalls atomic.Int64
	var lastID atomic.Pointer[uuid.UUID]
	h.IndexAsync = func(id uuid.UUID) {
		indexCalls.Add(1)
		copyID := id
		lastID.Store(&copyID)
	}

	req0 := httptest.NewRequest(http.MethodPatch, "/sessions/"+uuid.New().String(), bytes.NewReader([]byte("{}")))
	creator := addUserSessionCookie(t, h, req0, "reindex-trigger@example.com")
	session := &models.Session{
		ID:        uuid.New(),
		Title:     "Reindex trigger session",
		CreatedBy: &creator.Email,
		Status:    models.SessionStatusOpen,
	}
	require.NoError(t, h.DB.CreateSession(ctx, session))

	patch := func(body map[string]any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPatch, "/sessions/"+session.ID.String(), bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		for _, c := range req0.Cookies() {
			req.AddCookie(c)
		}
		w := httptest.NewRecorder()
		h.RequireAuth(h.UpdateSessionStatus)(w, req)
		return w
	}

	cases := []struct {
		name string
		body map[string]any
	}{
		{"title edit triggers", map[string]any{"title": "Renamed session"}},
		{"status edit triggers", map[string]any{"status": "closed"}},
		{"premise edit triggers", map[string]any{"premise": "Some premise."}},
		{"primary_decision edit triggers", map[string]any{"primary_decision": "Approve X."}},
		{"decision_outcome edit triggers", map[string]any{"decision_outcome": "Approved."}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := indexCalls.Load()
			w := patch(tc.body)
			require.Equal(t, http.StatusOK, w.Code, "%s: %s", tc.name, w.Body.String())
			require.Equal(t, before+1, indexCalls.Load(), "%s should trigger exactly one IndexAsync", tc.name)
			require.NotNil(t, lastID.Load())
			require.Equal(t, session.ID, *lastID.Load())
		})
	}

	t.Run("empty body does not trigger", func(t *testing.T) {
		before := indexCalls.Load()
		w := patch(map[string]any{})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Equal(t, before, indexCalls.Load(), "no metadata fields means no reindex")
	})
}

func TestGetSessionTimeline(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	db := h.DB
	ctx := context.Background()

	session := createTestSessionForHandlers(t, db, "Timeline Session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "Timeline Artifact", nil)
	require.NoError(t, err)

	t.Run("returns empty timeline for session with no questions", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"/timeline", nil)
		w := httptest.NewRecorder()

		h.GetSessionTimeline(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, session.ID.String(), response["session_id"])

		timeline, ok := response["timeline"].([]interface{})
		require.True(t, ok)
		assert.Empty(t, timeline)
	})

	t.Run("returns timeline with questions and answers in chronological order", func(t *testing.T) {
		// Create questions and answers
		now := time.Now()

		q1 := &models.Question{
			ID:               uuid.New(),
			ArtifactID:       artifact.ID,
			SessionID:        session.ID,
			QuestionText:     "First question?",
			QuestionSource:   models.QuestionSourceText,
			CreatedAt:        now.Add(-5 * time.Minute),
			VideoTimeSeconds: intPtr(60),
		}
		err := db.CreateQuestion(ctx, q1)
		require.NoError(t, err)

		a1 := &models.Answer{
			ID:           uuid.New(),
			QuestionID:   q1.ID,
			AnswerText:   "First answer",
			AnswerStatus: models.AnswerStatusAnswered,
			Confidence:   0.8,
			Citations:    []models.Citation{},
			CreatedAt:    now.Add(-4 * time.Minute),
		}
		err = db.CreateAnswer(ctx, a1)
		require.NoError(t, err)

		q2 := &models.Question{
			ID:               uuid.New(),
			ArtifactID:       artifact.ID,
			SessionID:        session.ID,
			QuestionText:     "Second question?",
			QuestionSource:   models.QuestionSourceText,
			CreatedAt:        now.Add(-3 * time.Minute),
			VideoTimeSeconds: intPtr(120),
		}
		err = db.CreateQuestion(ctx, q2)
		require.NoError(t, err)

		a2 := &models.Answer{
			ID:           uuid.New(),
			QuestionID:   q2.ID,
			AnswerText:   "Second answer",
			AnswerStatus: models.AnswerStatusAnswered,
			Confidence:   0.9,
			Citations:    []models.Citation{},
			CreatedAt:    now.Add(-2 * time.Minute),
		}
		err = db.CreateAnswer(ctx, a2)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"/timeline", nil)
		w := httptest.NewRecorder()

		h.GetSessionTimeline(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, session.ID.String(), response["session_id"])

		timeline, ok := response["timeline"].([]interface{})
		require.True(t, ok)
		assert.Len(t, timeline, 2)

		// Verify chronological order (oldest first)
		entry1 := timeline[0].(map[string]interface{})
		entry2 := timeline[1].(map[string]interface{})

		question1 := entry1["question"].(map[string]interface{})
		question2 := entry2["question"].(map[string]interface{})

		assert.Equal(t, q1.ID.String(), question1["id"])
		assert.Equal(t, "First question?", question1["question_text"])
		assert.NotNil(t, entry1["video_time_seconds"])
		assert.Equal(t, float64(60), entry1["video_time_seconds"])

		assert.Equal(t, q2.ID.String(), question2["id"])
		assert.Equal(t, "Second question?", question2["question_text"])
		assert.NotNil(t, entry2["video_time_seconds"])
		assert.Equal(t, float64(120), entry2["video_time_seconds"])

		// Verify answers are included
		answer1 := entry1["answer"].(map[string]interface{})
		answer2 := entry2["answer"].(map[string]interface{})

		assert.Equal(t, "First answer", answer1["answer_text"])
		assert.Equal(t, "Second answer", answer2["answer_text"])
	})

	t.Run("respects limit query parameter", func(t *testing.T) {
		// Create a few more questions
		for i := 0; i < 5; i++ {
			q := &models.Question{
				ID:             uuid.New(),
				ArtifactID:     artifact.ID,
				SessionID:      session.ID,
				QuestionText:   fmt.Sprintf("Question %d?", i),
				QuestionSource: models.QuestionSourceText,
				CreatedAt:      time.Now(),
			}
			err := db.CreateQuestion(ctx, q)
			require.NoError(t, err)
		}

		req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"/timeline?limit=3", nil)
		w := httptest.NewRecorder()

		h.GetSessionTimeline(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		timeline, ok := response["timeline"].([]interface{})
		require.True(t, ok)
		assert.LessOrEqual(t, len(timeline), 3)
	})

	t.Run("returns 400 for invalid session ID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/sessions/invalid-id/timeline", nil)
		w := httptest.NewRecorder()

		h.GetSessionTimeline(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("returns 404 for non-existent session", func(t *testing.T) {
		nonExistentID := uuid.New()
		req := httptest.NewRequest(http.MethodGet, "/sessions/"+nonExistentID.String()+"/timeline", nil)
		w := httptest.NewRecorder()

		h.GetSessionTimeline(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("returns 405 for non-GET methods", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/sessions/"+session.ID.String()+"/timeline", nil)
		w := httptest.NewRecorder()

		h.GetSessionTimeline(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("returns 400 for invalid URL path", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"/invalid", nil)
		w := httptest.NewRecorder()

		h.GetSessionTimeline(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("does not return questions from other sessions", func(t *testing.T) {
		// Create another session with questions
		otherSession := createTestSessionForHandlers(t, db, "Other Session")
		otherArtifact, err := db.CreateArtifact(ctx, otherSession.ID, "Other Artifact", nil)
		require.NoError(t, err)

		otherQuestion := &models.Question{
			ID:             uuid.New(),
			ArtifactID:     otherArtifact.ID,
			SessionID:      otherSession.ID,
			QuestionText:   "Other session question?",
			QuestionSource: models.QuestionSourceText,
			CreatedAt:      time.Now(),
		}
		err = db.CreateQuestion(ctx, otherQuestion)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"/timeline", nil)
		w := httptest.NewRecorder()

		h.GetSessionTimeline(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		timeline, ok := response["timeline"].([]interface{})
		require.True(t, ok)

		// Verify no questions from other session are included
		for _, entry := range timeline {
			entryMap := entry.(map[string]interface{})
			question := entryMap["question"].(map[string]interface{})
			assert.NotEqual(t, otherQuestion.ID.String(), question["id"])
		}
	})
}

func TestSessionPlayback(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	db := h.DB
	ctx := context.Background()

	t.Run("returns 404 and VIDEO_NOT_INGESTED when session has no primary_video_artifact_id", func(t *testing.T) {
		session := createTestSessionForHandlers(t, db, "No primary video")
		req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"/playback", nil)
		w := httptest.NewRecorder()
		h.SessionPlayback(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
		var body SessionPlaybackErrorResponse
		err := json.Unmarshal(w.Body.Bytes(), &body)
		require.NoError(t, err)
		assert.Equal(t, "VIDEO_NOT_INGESTED", body.ReasonCode)
	})

	t.Run("returns 409 and VIDEO_INGEST_PENDING when primary artifact is pending", func(t *testing.T) {
		session := createTestSessionForHandlers(t, db, "Pending video")
		aid := uuid.New()
		fn := "zoom.mp4"
		fa := &models.FileArtifact{
			ID:              aid,
			SessionID:       &session.ID,
			Kind:            models.FileArtifactKindVideo,
			Filename:        &fn,
			ContentType:     "video/mp4",
			StorageProvider: "r2",
			StorageBucket:   "bucket",
			StorageKey:      "sessions/" + session.ID.String() + "/" + aid.String() + "/zoom.mp4",
			Status:          models.FileArtifactStatusPending,
		}
		err := db.CreateFileArtifact(ctx, fa)
		require.NoError(t, err)
		err = db.SetSessionPrimaryVideoArtifact(ctx, session.ID, &aid)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"/playback", nil)
		w := httptest.NewRecorder()
		h.SessionPlayback(w, req)
		assert.Equal(t, http.StatusConflict, w.Code)
		var body SessionPlaybackErrorResponse
		err = json.Unmarshal(w.Body.Bytes(), &body)
		require.NoError(t, err)
		assert.Equal(t, "VIDEO_INGEST_PENDING", body.ReasonCode)
	})

	t.Run("returns 422 and VIDEO_INGEST_FAILED when primary artifact is failed", func(t *testing.T) {
		session := createTestSessionForHandlers(t, db, "Failed video")
		aid := uuid.New()
		fn := "zoom.mp4"
		reason := "r2_put: timeout"
		fa := &models.FileArtifact{
			ID:              aid,
			SessionID:       &session.ID,
			Kind:            models.FileArtifactKindVideo,
			Filename:        &fn,
			ContentType:     "video/mp4",
			StorageProvider: "r2",
			StorageBucket:   "bucket",
			StorageKey:      "sessions/" + session.ID.String() + "/" + aid.String() + "/zoom.mp4",
			Status:          models.FileArtifactStatusFailed,
			FailureReason:   &reason,
		}
		err := db.CreateFileArtifact(ctx, fa)
		require.NoError(t, err)
		err = db.SetSessionPrimaryVideoArtifact(ctx, session.ID, &aid)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"/playback", nil)
		w := httptest.NewRecorder()
		h.SessionPlayback(w, req)
		assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
		var body SessionPlaybackErrorResponse
		err = json.Unmarshal(w.Body.Bytes(), &body)
		require.NoError(t, err)
		assert.Equal(t, "VIDEO_INGEST_FAILED", body.ReasonCode)
	})

	t.Run("returns 404 when primary artifact is ready but storage not configured (no Zoom fallback)", func(t *testing.T) {
		session := createTestSessionForHandlers(t, db, "Ready but no storage")
		aid := uuid.New()
		fn := "zoom.mp4"
		fa := &models.FileArtifact{
			ID:              aid,
			SessionID:       &session.ID,
			Kind:            models.FileArtifactKindVideo,
			Filename:        &fn,
			ContentType:     "video/mp4",
			StorageProvider: "r2",
			StorageBucket:   "bucket",
			StorageKey:      "sessions/" + session.ID.String() + "/" + aid.String() + "/zoom.mp4",
			Status:          models.FileArtifactStatusReady,
		}
		err := db.CreateFileArtifact(ctx, fa)
		require.NoError(t, err)
		err = db.UpdateFileArtifactToReady(ctx, aid, 1024, "video/mp4")
		require.NoError(t, err)
		err = db.SetSessionPrimaryVideoArtifact(ctx, session.ID, &aid)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"/playback", nil)
		w := httptest.NewRecorder()
		h.SessionPlayback(w, req)
		// Handlers have nil Storage in tests; playback returns 404 (R2-only, no Zoom).
		assert.Equal(t, http.StatusNotFound, w.Code)
		var body SessionPlaybackErrorResponse
		err = json.Unmarshal(w.Body.Bytes(), &body)
		require.NoError(t, err)
		assert.Equal(t, "VIDEO_NOT_INGESTED", body.ReasonCode)
	})
}

func TestDeleteSession(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()
	db := h.DB

	t.Run("returns 401 when not authenticated", func(t *testing.T) {
		session := createTestSessionForHandlers(t, db, "To Delete")
		req := httptest.NewRequest(http.MethodDelete, "/api/sessions/"+session.ID.String(), nil)
		w := httptest.NewRecorder()
		h.RequireAuth(h.DeleteSession)(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("returns 404 for non-existent session", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/sessions/"+uuid.New().String(), nil)
		addAdminSessionCookie(t, h, req)
		w := httptest.NewRecorder()
		h.RequireAuth(h.DeleteSession)(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("returns 403 when current user is not admin", func(t *testing.T) {
		session := createTestSessionForHandlers(t, db, "To Delete")
		req := httptest.NewRequest(http.MethodDelete, "/api/sessions/"+session.ID.String(), nil)
		addUserSessionCookie(t, h, req, "creator@example.com")
		w := httptest.NewRecorder()
		h.RequireAuth(h.DeleteSession)(w, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
		_, err := db.GetSession(ctx, session.ID)
		require.NoError(t, err)
	})

	t.Run("returns 204 and deletes session when current user is admin", func(t *testing.T) {
		session := createTestSessionForHandlers(t, db, "To Delete")
		adminEmail := "admin-delete-session@example.com"
		admin := &models.User{
			ID:          uuid.New(),
			Email:       adminEmail,
			DisplayName: "Admin",
			Status:      models.UserStatusActive,
			GlobalRole:  models.GlobalRoleAdmin,
		}
		err := db.CreateUser(ctx, admin)
		require.NoError(t, err)
		loginSession := &models.LoginSession{
			ID:        uuid.New(),
			UserID:    admin.ID,
			CreatedAt: time.Now(),
			ExpiresAt: time.Now().Add(24 * time.Hour),
		}
		err = db.CreateLoginSession(ctx, loginSession)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodDelete, "/api/sessions/"+session.ID.String(), nil)
		req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: loginSession.ID.String()})
		w := httptest.NewRecorder()
		h.RequireAuth(h.DeleteSession)(w, req)
		assert.Equal(t, http.StatusNoContent, w.Code)
		_, err = db.GetSession(ctx, session.ID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func intPtr(i int) *int {
	return &i
}

// TestUpdateSessionStatus_SCRUM227_EditorMatrix pins the SCRUM-227 contract:
// editor authority is granted by global Admin OR session_memberships.role=
// 'creator' — including users promoted to creator via PATCH (not just the
// original by-email creator). decision_maker and plain participant must 403.
func TestUpdateSessionStatus_SCRUM227_EditorMatrix(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	type principal struct {
		user      *models.User
		loginID   uuid.UUID
		expectOK  bool
		labelHint string
	}

	makeUser := func(t *testing.T, email string, role models.GlobalRole) *principal {
		t.Helper()
		u := &models.User{
			ID:          uuid.New(),
			Email:       email,
			DisplayName: email,
			Status:      models.UserStatusActive,
			GlobalRole:  role,
		}
		require.NoError(t, h.DB.CreateUser(ctx, u))
		ls := &models.LoginSession{
			ID:        uuid.New(),
			UserID:    u.ID,
			CreatedAt: time.Now(),
			ExpiresAt: time.Now().Add(24 * time.Hour),
		}
		require.NoError(t, h.DB.CreateLoginSession(ctx, ls))
		return &principal{user: u, loginID: ls.ID}
	}

	// Original creator (gets a creator membership automatically per SCRUM-226).
	original := makeUser(t, "scrum227-original-"+uuid.New().String()+"@test", models.GlobalRoleCreator)
	original.labelHint = "original_creator"
	original.expectOK = true

	sess := &models.Session{
		ID:        uuid.New(),
		Title:     "SCRUM-227 Editor Matrix",
		CreatedBy: &original.user.Email,
		Status:    models.SessionStatusOpen,
	}
	require.NoError(t, h.DB.CreateSession(ctx, sess))

	// Promoted creator: starts as a participant via session_memberships, then
	// gets PATCHed to creator. Same path as the Members-panel "Set as Creator".
	promoted := makeUser(t, "scrum227-promoted-"+uuid.New().String()+"@test", models.GlobalRoleParticipant)
	promoted.labelHint = "promoted_creator"
	promoted.expectOK = true
	require.NoError(t, h.DB.CreateSessionMembership(ctx, sess.ID, promoted.user.ID, "participant", &original.user.ID))
	require.NoError(t, h.DB.UpdateSessionMembershipRole(ctx, sess.ID, promoted.user.ID, "creator"))

	dm := makeUser(t, "scrum227-dm-"+uuid.New().String()+"@test", models.GlobalRoleParticipant)
	dm.labelHint = "decision_maker"
	dm.expectOK = false
	require.NoError(t, h.DB.CreateSessionMembership(ctx, sess.ID, dm.user.ID, "decision_maker", &original.user.ID))

	plain := makeUser(t, "scrum227-plain-"+uuid.New().String()+"@test", models.GlobalRoleParticipant)
	plain.labelHint = "participant"
	plain.expectOK = false
	require.NoError(t, h.DB.CreateSessionMembership(ctx, sess.ID, plain.user.ID, "participant", &original.user.ID))

	// Outsider: not a member of the session, not an admin.
	outsider := makeUser(t, "scrum227-outsider-"+uuid.New().String()+"@test", models.GlobalRoleCreator)
	outsider.labelHint = "outsider_creator_role"
	outsider.expectOK = false

	// Global admin: not a member of the session, but has admin role.
	admin := makeUser(t, "scrum227-admin-"+uuid.New().String()+"@test", models.GlobalRoleAdmin)
	admin.labelHint = "global_admin"
	admin.expectOK = true

	cases := []*principal{original, promoted, dm, plain, outsider, admin}
	for _, c := range cases {
		c := c
		t.Run(c.labelHint, func(t *testing.T) {
			premise := "edited by " + c.labelHint
			body, _ := json.Marshal(map[string]any{"premise": premise})
			req := httptest.NewRequest(http.MethodPatch, "/sessions/"+sess.ID.String(), bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: c.loginID.String()})
			w := httptest.NewRecorder()
			h.RequireAuth(h.UpdateSessionStatus)(w, req)

			if c.expectOK {
				assert.Equal(t, http.StatusOK, w.Code, "expected %s to be allowed; body=%s", c.labelHint, w.Body.String())
				updated, err := h.DB.GetSession(ctx, sess.ID)
				require.NoError(t, err)
				require.NotNil(t, updated.Premise)
				assert.Equal(t, premise, *updated.Premise)
			} else {
				assert.Equal(t, http.StatusForbidden, w.Code, "expected %s to be forbidden; body=%s", c.labelHint, w.Body.String())
			}
		})
	}
}
