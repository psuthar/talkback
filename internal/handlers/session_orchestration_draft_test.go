package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/auth"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/orchestration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOrchestrationDraft_Unauthorized verifies POST without session cookie returns 401.
func TestOrchestrationDraft_Unauthorized(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	sess := createTestSessionForHandlers(t, h.DB, "draft unauthorized")
	body, _ := json.Marshal(map[string]string{"question_id": uuid.New().String()})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sess.ID.String()+"/orchestration/draft-answers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.RequireAuth(h.CreateOrchestrationDraftAnswer)(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestOrchestrationDraft_ForbiddenForNonCreator verifies a non-owner creator gets 403.
func TestOrchestrationDraft_ForbiddenForNonCreator(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	ctx := context.Background()
	ownerEmail := "owner-draft@example.com"
	sess := &models.Session{
		ID:        uuid.New(),
		Title:     "Draft ACL",
		CreatedBy: &ownerEmail,
		Status:    models.SessionStatusOpen,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, h.DB.CreateSession(ctx, sess))
	artifact, err := h.DB.CreateArtifact(ctx, sess.ID, "A", nil)
	require.NoError(t, err)
	q := &models.Question{
		ID:            uuid.New(),
		ArtifactID:    artifact.ID,
		SessionID:     sess.ID,
		QuestionText:  "What is the budget?",
		QuestionSource: models.QuestionSourceText,
	}
	require.NoError(t, h.DB.CreateQuestion(ctx, q))

	body, _ := json.Marshal(map[string]string{"question_id": q.ID.String()})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sess.ID.String()+"/orchestration/draft-answers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	addUserSessionCookie(t, h, req, "other@example.com")
	w := httptest.NewRecorder()
	h.RequireAuth(h.CreateOrchestrationDraftAnswer)(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// TestOrchestrationDraft_CreatorCreatesDraft_NoBroadcastPath uses deterministic no-chunk path;
// draft is persisted with orchestration-draft model and confirmed=false.
func TestOrchestrationDraft_CreatorCreatesDraft_NoBroadcastPath(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	_, ls, sess := seedCreatorAndSession(t, h, "draft-create")
	ctx := context.Background()
	artifact, err := h.DB.CreateArtifact(ctx, sess.ID, "Artifact", nil)
	require.NoError(t, err)
	q := &models.Question{
		ID:             uuid.New(),
		ArtifactID:     artifact.ID,
		SessionID:      sess.ID,
		QuestionText:   "What is the Meridian proposal?",
		QuestionSource: models.QuestionSourceText,
	}
	require.NoError(t, h.DB.CreateQuestion(ctx, q))

	body, _ := json.Marshal(map[string]string{"question_id": q.ID.String()})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sess.ID.String()+"/orchestration/draft-answers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	w := httptest.NewRecorder()
	h.RequireAuth(h.CreateOrchestrationDraftAnswer)(w, req)

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var ans models.Answer
	require.NoError(t, json.NewDecoder(w.Body).Decode(&ans))
	require.NotNil(t, ans.Model)
	assert.Equal(t, orchestration.DraftAnswerModel, *ans.Model)
	assert.False(t, ans.Confirmed)

	stored, err := h.DB.GetLatestAnswerByQuestionID(ctx, q.ID)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.False(t, stored.Confirmed)
	require.NotNil(t, stored.Model)
	assert.Equal(t, orchestration.DraftAnswerModel, *stored.Model)
}

// TestOrchestrationDraft_ConflictWhenManualAnswerExists returns 409 if question already answered.
func TestOrchestrationDraft_ConflictWhenManualAnswerExists(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	_, ls, sess := seedCreatorAndSession(t, h, "draft-conflict")
	ctx := context.Background()
	artifact, err := h.DB.CreateArtifact(ctx, sess.ID, "Artifact", nil)
	require.NoError(t, err)
	q := &models.Question{
		ID:             uuid.New(),
		ArtifactID:     artifact.ID,
		SessionID:      sess.ID,
		QuestionText:   "Existing?",
		QuestionSource: models.QuestionSourceText,
	}
	require.NoError(t, h.DB.CreateQuestion(ctx, q))
	manual := "manual"
	prev := &models.Answer{
		ID:           uuid.New(),
		QuestionID:   q.ID,
		AnswerText:   "Yes",
		AnswerStatus: models.AnswerStatusAnswered,
		Confidence:   1,
		Citations:    nil,
		Model:        &manual,
		Confirmed:    true,
	}
	require.NoError(t, h.DB.CreateAnswer(ctx, prev))

	body, _ := json.Marshal(map[string]string{"question_id": q.ID.String()})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sess.ID.String()+"/orchestration/draft-answers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	w := httptest.NewRecorder()
	h.RequireAuth(h.CreateOrchestrationDraftAnswer)(w, req)
	assert.Equal(t, http.StatusConflict, w.Code)
}

// TestOrchestrationDraft_DeleteDraft removes unconfirmed orchestration draft.
func TestOrchestrationDraft_DeleteDraft(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	_, ls, sess := seedCreatorAndSession(t, h, "draft-delete")
	ctx := context.Background()
	artifact, err := h.DB.CreateArtifact(ctx, sess.ID, "Artifact", nil)
	require.NoError(t, err)
	q := &models.Question{
		ID:             uuid.New(),
		ArtifactID:     artifact.ID,
		SessionID:      sess.ID,
		QuestionText:   "Delete me?",
		QuestionSource: models.QuestionSourceText,
	}
	require.NoError(t, h.DB.CreateQuestion(ctx, q))
	dm := orchestration.DraftAnswerModel
	a := &models.Answer{
		ID:           uuid.New(),
		QuestionID:   q.ID,
		AnswerText:   "draft",
		AnswerStatus: models.AnswerStatusAnswered,
		Confidence:   0.5,
		Citations:    nil,
		Model:        &dm,
		Confirmed:    false,
	}
	require.NoError(t, h.DB.CreateAnswer(ctx, a))

	delURL := "/sessions/" + sess.ID.String() + "/orchestration/draft-answers/" + a.ID.String()
	req := httptest.NewRequest(http.MethodDelete, delURL, nil)
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	w := httptest.NewRecorder()
	h.RequireAuth(h.DeleteOrchestrationDraftAnswer)(w, req)
	require.Equal(t, http.StatusNoContent, w.Code)

	again, err := h.DB.GetLatestAnswerByQuestionID(ctx, q.ID)
	require.NoError(t, err)
	assert.Nil(t, again)
}
