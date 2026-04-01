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
	"github.com/psuthar/talkback/internal/auth"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/orchestration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedArtifactAndQuestion is a test helper that creates an artifact and a question for a session.
func seedArtifactAndQuestion(t *testing.T, h *Handlers, sess *models.Session, suffix string) *models.Question {
	t.Helper()
	ctx := context.Background()
	artifact, err := h.DB.CreateArtifact(ctx, sess.ID, "Artifact-"+suffix, nil)
	require.NoError(t, err)
	q := &models.Question{
		ID:             uuid.New(),
		ArtifactID:     artifact.ID,
		SessionID:      sess.ID,
		QuestionText:   "Test question " + suffix + "?",
		QuestionSource: models.QuestionSourceText,
	}
	require.NoError(t, h.DB.CreateQuestion(ctx, q))
	return q
}

// seedIndexableMaterial inserts a material with ExtractedText so sessionHasIndexableContent is true (SCRUM-20).
func seedIndexableMaterial(t *testing.T, h *Handlers, sess *models.Session, artifact *models.Artifact) {
	t.Helper()
	ctx := context.Background()
	txt := "Indexable text for orchestration draft RAG and guard tests."
	m := &models.Material{
		ID:            uuid.New(),
		ArtifactID:    artifact.ID,
		SessionID:     sess.ID,
		Kind:          string(models.MaterialKindDocument),
		Filename:      "doc.txt",
		ContentType:   "text/plain",
		StorageURL:    "",
		TextStatus:    models.MaterialTextStatusReady,
		ExtractedText: strPtr(txt),
	}
	require.NoError(t, h.DB.CreateMaterial(ctx, m))
}

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
	seedIndexableMaterial(t, h, sess, artifact)
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
	seedIndexableMaterial(t, h, sess, artifact)
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

// TestOrchestrationDraft_CreatorApproveAndRejectFlow verifies creator can approve a draft answer
// and can reject (delete) an unconfirmed draft in the review flow.
func TestOrchestrationDraft_CreatorApproveAndRejectFlow(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	_, ls, sess := seedCreatorAndSession(t, h, "draft-review-flow")
	ctx := context.Background()
	artifact, err := h.DB.CreateArtifact(ctx, sess.ID, "Artifact", nil)
	require.NoError(t, err)
	q := &models.Question{
		ID:             uuid.New(),
		ArtifactID:     artifact.ID,
		SessionID:      sess.ID,
		QuestionText:   "Approve this draft?",
		QuestionSource: models.QuestionSourceText,
	}
	require.NoError(t, h.DB.CreateQuestion(ctx, q))
	dm := orchestration.DraftAnswerModel
	a := &models.Answer{
		ID:           uuid.New(),
		QuestionID:   q.ID,
		AnswerText:   "suggested",
		AnswerStatus: models.AnswerStatusAnswered,
		Confidence:   0.7,
		Citations:    []models.Citation{},
		Model:        &dm,
		Confirmed:    false,
	}
	require.NoError(t, h.DB.CreateAnswer(ctx, a))

	// Approve (confirm=true)
	approveBody, _ := json.Marshal(map[string]bool{"confirmed": true})
	approveReq := httptest.NewRequest(http.MethodPatch, "/sessions/"+sess.ID.String()+"/answers/"+a.ID.String()+"/confirm", bytes.NewReader(approveBody))
	approveReq.Header.Set("Content-Type", "application/json")
	approveReq.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	approveW := httptest.NewRecorder()
	h.RequireAuth(h.UpdateAnswerConfirmed)(approveW, approveReq)
	require.Equal(t, http.StatusOK, approveW.Code, approveW.Body.String())

	approved, err := h.DB.GetAnswerByID(ctx, a.ID)
	require.NoError(t, err)
	require.NotNil(t, approved)
	assert.True(t, approved.Confirmed)

	// New unconfirmed draft can be rejected (deleted)
	a2 := &models.Answer{
		ID:           uuid.New(),
		QuestionID:   q.ID,
		AnswerText:   "another suggested",
		AnswerStatus: models.AnswerStatusAnswered,
		Confidence:   0.6,
		Citations:    []models.Citation{},
		Model:        &dm,
		Confirmed:    false,
	}
	require.NoError(t, h.DB.CreateAnswer(ctx, a2))
	rejectReq := httptest.NewRequest(http.MethodDelete, "/sessions/"+sess.ID.String()+"/orchestration/draft-answers/"+a2.ID.String(), nil)
	rejectReq.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	rejectW := httptest.NewRecorder()
	h.RequireAuth(h.DeleteOrchestrationDraftAnswer)(rejectW, rejectReq)
	require.Equal(t, http.StatusNoContent, rejectW.Code, rejectW.Body.String())

	afterReject, err := h.DB.GetAnswerByID(ctx, a2.ID)
	require.NoError(t, err)
	assert.Nil(t, afterReject)
}

// TestOrchestrationDraft_ParticipantCannotSeeUnconfirmedDraft verifies that an unconfirmed
// orchestration draft is never returned to callers of GET /sessions/{id}/questions or
// GET /sessions/{id}/timeline. Once the draft is confirmed (approved), it becomes visible.
func TestOrchestrationDraft_ParticipantCannotSeeUnconfirmedDraft(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	_, ls, sess := seedCreatorAndSession(t, h, "vis-hidden")
	ctx := context.Background()
	q := seedArtifactAndQuestion(t, h, sess, "vis-hidden")

	dm := orchestration.DraftAnswerModel
	draft := &models.Answer{
		ID:           uuid.New(),
		QuestionID:   q.ID,
		AnswerText:   "This is a draft answer",
		AnswerStatus: models.AnswerStatusAnswered,
		Confidence:   0.8,
		Citations:    []models.Citation{},
		Model:        &dm,
		Confirmed:    false,
	}
	require.NoError(t, h.DB.CreateAnswer(ctx, draft))

	// GET /sessions/{id}/questions — answers array must be empty (draft filtered out)
	questionsReq := httptest.NewRequest(http.MethodGet, "/sessions/"+sess.ID.String()+"/questions", nil)
	questionsW := httptest.NewRecorder()
	h.GetSessionQuestions(questionsW, questionsReq)
	require.Equal(t, http.StatusOK, questionsW.Code, questionsW.Body.String())
	var qResp GetQuestionsResponse
	require.NoError(t, json.NewDecoder(questionsW.Body).Decode(&qResp))
	require.Len(t, qResp.Questions, 1, "question itself must still appear")
	assert.Empty(t, qResp.Answers, "unconfirmed orchestration draft must not appear in answers array")

	// GET /sessions/{id}/timeline — entry for the question must have answer=nil
	timelineReq := httptest.NewRequest(http.MethodGet, "/sessions/"+sess.ID.String()+"/timeline", nil)
	timelineW := httptest.NewRecorder()
	h.GetSessionTimeline(timelineW, timelineReq)
	require.Equal(t, http.StatusOK, timelineW.Code, timelineW.Body.String())
	var tlResp struct {
		Timeline []struct {
			Question *models.Question `json:"question"`
			Answer   *models.Answer   `json:"answer"`
		} `json:"timeline"`
	}
	require.NoError(t, json.NewDecoder(timelineW.Body).Decode(&tlResp))
	require.Len(t, tlResp.Timeline, 1, "question must still appear in timeline")
	assert.Nil(t, tlResp.Timeline[0].Answer, "unconfirmed orchestration draft must not appear in timeline entry")

	// Approve the draft: PATCH /sessions/{id}/answers/{aid}/confirm
	approveBody, _ := json.Marshal(map[string]bool{"confirmed": true})
	approveReq := httptest.NewRequest(http.MethodPatch, "/sessions/"+sess.ID.String()+"/answers/"+draft.ID.String()+"/confirm", bytes.NewReader(approveBody))
	approveReq.Header.Set("Content-Type", "application/json")
	approveReq.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	approveW := httptest.NewRecorder()
	h.RequireAuth(h.UpdateAnswerConfirmed)(approveW, approveReq)
	require.Equal(t, http.StatusOK, approveW.Code, approveW.Body.String())

	// After approval, answers array must contain the now-confirmed answer
	questionsReq2 := httptest.NewRequest(http.MethodGet, "/sessions/"+sess.ID.String()+"/questions", nil)
	questionsW2 := httptest.NewRecorder()
	h.GetSessionQuestions(questionsW2, questionsReq2)
	require.Equal(t, http.StatusOK, questionsW2.Code, questionsW2.Body.String())
	var qResp2 GetQuestionsResponse
	require.NoError(t, json.NewDecoder(questionsW2.Body).Decode(&qResp2))
	require.Len(t, qResp2.Answers, 1, "confirmed draft must appear in answers after approval")
	assert.Equal(t, draft.ID, qResp2.Answers[0].ID)
}

// TestOrchestrationDraft_NonDraftUnconfirmedAnswerRemainsBehaviorUnchanged verifies that
// existing answers without the orchestration-draft model are not affected by the draft filter,
// preserving backward-compatible behavior for all non-draft answer paths.
func TestOrchestrationDraft_NonDraftUnconfirmedAnswerRemainsBehaviorUnchanged(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	_, _, sess := seedCreatorAndSession(t, h, "vis-nondraft")
	ctx := context.Background()
	q := seedArtifactAndQuestion(t, h, sess, "vis-nondraft")

	// Regular RAG answer — model is NOT orchestration-draft, confirmed=false (legacy default)
	ragModel := "gpt-4o-mini"
	ragAnswer := &models.Answer{
		ID:           uuid.New(),
		QuestionID:   q.ID,
		AnswerText:   "RAG-generated answer",
		AnswerStatus: models.AnswerStatusAnswered,
		Confidence:   0.9,
		Citations:    []models.Citation{},
		Model:        &ragModel,
		Confirmed:    false,
	}
	require.NoError(t, h.DB.CreateAnswer(ctx, ragAnswer))

	// GET /sessions/{id}/questions — non-draft answer must still appear (backward compatible)
	req := httptest.NewRequest(http.MethodGet, "/sessions/"+sess.ID.String()+"/questions", nil)
	w := httptest.NewRecorder()
	h.GetSessionQuestions(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp GetQuestionsResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Len(t, resp.Questions, 1)
	require.Len(t, resp.Answers, 1, "non-draft answer must still be visible regardless of confirmed flag")
	assert.Equal(t, ragAnswer.ID, resp.Answers[0].ID)
}

// TestOrchestrationDraft_NoIndexableContent returns 400 when the session has no transcript/material text to index.
func TestOrchestrationDraft_NoIndexableContent(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	_, ls, sess := seedCreatorAndSession(t, h, "draft-no-idx")
	ctx := context.Background()
	artifact, err := h.DB.CreateArtifact(ctx, sess.ID, "Artifact", nil)
	require.NoError(t, err)
	q := &models.Question{
		ID:             uuid.New(),
		ArtifactID:     artifact.ID,
		SessionID:      sess.ID,
		QuestionText:   "Any question?",
		QuestionSource: models.QuestionSourceText,
	}
	require.NoError(t, h.DB.CreateQuestion(ctx, q))

	body, _ := json.Marshal(map[string]string{"question_id": q.ID.String()})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sess.ID.String()+"/orchestration/draft-answers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	w := httptest.NewRecorder()
	h.RequireAuth(h.CreateOrchestrationDraftAnswer)(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	var resp map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "no_indexable_content", resp["error_code"])
}

// TestOrchestrationDraft_RateLimit returns 429 after ORCHESTRATION_DRAFT_MAX_PER_MIN draft POSTs for the same user+session.
func TestOrchestrationDraft_RateLimit(t *testing.T) {
	t.Setenv("ORCHESTRATION_DRAFT_MAX_PER_MIN", "2")
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	_, ls, sess := seedCreatorAndSession(t, h, "draft-rate")
	ctx := context.Background()
	artifact, err := h.DB.CreateArtifact(ctx, sess.ID, "Artifact", nil)
	require.NoError(t, err)
	seedIndexableMaterial(t, h, sess, artifact)

	postDraft := func(questionID uuid.UUID) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"question_id": questionID.String()})
		req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sess.ID.String()+"/orchestration/draft-answers", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
		w := httptest.NewRecorder()
		h.RequireAuth(h.CreateOrchestrationDraftAnswer)(w, req)
		return w
	}

	for i := 0; i < 2; i++ {
		q := &models.Question{
			ID:             uuid.New(),
			ArtifactID:     artifact.ID,
			SessionID:      sess.ID,
			QuestionText:   fmt.Sprintf("Rate limit Q%d?", i),
			QuestionSource: models.QuestionSourceText,
		}
		require.NoError(t, h.DB.CreateQuestion(ctx, q))
		w := postDraft(q.ID)
		require.Equal(t, http.StatusCreated, w.Code, "iteration %d: %s", i, w.Body.String())
	}

	q3 := &models.Question{
		ID:             uuid.New(),
		ArtifactID:     artifact.ID,
		SessionID:      sess.ID,
		QuestionText:   "Third question?",
		QuestionSource: models.QuestionSourceText,
	}
	require.NoError(t, h.DB.CreateQuestion(ctx, q3))
	w3 := postDraft(q3.ID)
	require.Equal(t, http.StatusTooManyRequests, w3.Code, w3.Body.String())
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w3.Body).Decode(&resp))
	assert.Equal(t, "draft_rate_limited", resp["error_code"])
}
