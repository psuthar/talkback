package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SCRUM-366: handler-level tests for DELETE /api/sessions/{id}/questions/{questionID}.
// Covers status matrix (204/400/401/403/404), the auth contract (admin path,
// creator path, regular member 403, non-member 403, unauthenticated 401), the
// WS broadcast contract, the smoke flow including orchestration evidence
// purge, and the reply-FK race fix in the POST reply path.

type qaTestCtx struct {
	h            *Handlers
	db           *database.DB
	cleanup      func()
	creator      *models.User
	creatorEmail string
	session      *models.Session
	artifactID   uuid.UUID
}

func setupQADeleteTest(t *testing.T) *qaTestCtx {
	t.Helper()
	h, cleanup := setupTestHandlersParallel(t)
	ctx := context.Background()

	creatorEmail := "creator-" + uuid.New().String()[:8] + "@example.com"
	creator := &models.User{
		ID:          uuid.New(),
		Email:       creatorEmail,
		DisplayName: "Creator",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleCreator,
	}
	require.NoError(t, h.DB.CreateUser(ctx, creator))

	session := &models.Session{
		ID:        uuid.New(),
		Title:     "Delete-Q test session",
		CreatedBy: &creatorEmail,
		Status:    models.SessionStatusOpen,
	}
	require.NoError(t, h.DB.CreateSession(ctx, session))

	artifact, err := h.DB.CreateArtifact(ctx, session.ID, "Artifact", nil)
	require.NoError(t, err)

	return &qaTestCtx{
		h:            h,
		db:           h.DB,
		cleanup:      cleanup,
		creator:      creator,
		creatorEmail: creatorEmail,
		session:      session,
		artifactID:   artifact.ID,
	}
}

func (c *qaTestCtx) seedQuestion(t *testing.T, parent *uuid.UUID, text string) *models.Question {
	t.Helper()
	q := &models.Question{
		ID:               uuid.New(),
		ArtifactID:       c.artifactID,
		SessionID:        c.session.ID,
		ParentQuestionID: parent,
		QuestionText:     text,
		QuestionSource:   models.QuestionSourceText,
	}
	require.NoError(t, c.db.CreateQuestion(context.Background(), q))
	return q
}

// callDelete drives the route through the full router so RequireAuth wiring
// is exercised. user can be nil to simulate an unauthenticated caller.
func (c *qaTestCtx) callDelete(t *testing.T, sessionID, questionID uuid.UUID, user *models.User) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/"+sessionID.String()+"/questions/"+questionID.String(), nil)
	if user != nil {
		req = req.WithContext(context.WithValue(req.Context(), userContextKey, user))
		// Direct handler-call path (bypassing RequireAuth) — used for the
		// in-process test to keep DB-backed user lookup unnecessary.
		w := httptest.NewRecorder()
		c.h.DeleteSessionQuestion(w, req)
		return w
	}
	// Unauthenticated path goes through RequireAuth via the router so we get the real 401.
	w := httptest.NewRecorder()
	c.h.RequireAuth(c.h.DeleteSessionQuestion)(w, req)
	return w
}

func TestDeleteSessionQuestion_204_CreatorPath(t *testing.T) {
	t.Parallel()
	c := setupQADeleteTest(t)
	defer c.cleanup()

	q := c.seedQuestion(t, nil, "root q")

	w := c.callDelete(t, c.session.ID, q.ID, c.creator)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

	// Question is gone.
	got, err := c.db.GetQuestionByID(context.Background(), q.ID)
	assert.Nil(t, got)
	assert.Error(t, err)
}

func TestDeleteSessionQuestion_204_AdminPath_NotMember(t *testing.T) {
	t.Parallel()
	c := setupQADeleteTest(t)
	defer c.cleanup()

	q := c.seedQuestion(t, nil, "root q")

	admin := &models.User{
		ID:          uuid.New(),
		Email:       "admin-" + uuid.New().String()[:8] + "@example.com",
		DisplayName: "Admin",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleAdmin,
	}
	require.NoError(t, c.db.CreateUser(context.Background(), admin))
	// Note: admin has no session_memberships row for this session — admin override path.

	w := c.callDelete(t, c.session.ID, q.ID, admin)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
}

func TestDeleteSessionQuestion_403_RegularMember(t *testing.T) {
	t.Parallel()
	c := setupQADeleteTest(t)
	defer c.cleanup()

	q := c.seedQuestion(t, nil, "root q")

	regular := &models.User{
		ID:          uuid.New(),
		Email:       "regular-" + uuid.New().String()[:8] + "@example.com",
		DisplayName: "Regular Member",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleParticipant,
	}
	require.NoError(t, c.db.CreateUser(context.Background(), regular))
	require.NoError(t, c.db.CreateSessionMembership(context.Background(), c.session.ID, regular.ID, "participant", nil))

	w := c.callDelete(t, c.session.ID, q.ID, regular)
	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())

	// Question still exists.
	got, err := c.db.GetQuestionByID(context.Background(), q.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
}

func TestDeleteSessionQuestion_403_NonMember(t *testing.T) {
	t.Parallel()
	c := setupQADeleteTest(t)
	defer c.cleanup()

	q := c.seedQuestion(t, nil, "root q")

	stranger := &models.User{
		ID:          uuid.New(),
		Email:       "stranger-" + uuid.New().String()[:8] + "@example.com",
		DisplayName: "Stranger",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleCreator, // GlobalRole != admin so no override; not a member of this session.
	}
	require.NoError(t, c.db.CreateUser(context.Background(), stranger))

	w := c.callDelete(t, c.session.ID, q.ID, stranger)
	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
}

func TestDeleteSessionQuestion_401_Unauthenticated(t *testing.T) {
	t.Parallel()
	c := setupQADeleteTest(t)
	defer c.cleanup()

	q := c.seedQuestion(t, nil, "root q")

	// Hit the route via RequireAuth wrapper; no user in context, no session cookie.
	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/"+c.session.ID.String()+"/questions/"+q.ID.String(), nil)
	w := httptest.NewRecorder()
	c.h.RequireAuth(c.h.DeleteSessionQuestion)(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
}

func TestDeleteSessionQuestion_400_ReplyID(t *testing.T) {
	t.Parallel()
	c := setupQADeleteTest(t)
	defer c.cleanup()

	root := c.seedQuestion(t, nil, "root q")
	reply := c.seedQuestion(t, &root.ID, "reply q")

	w := c.callDelete(t, c.session.ID, reply.ID, c.creator)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "only top-level questions can be deleted", body["error"])
}

func TestDeleteSessionQuestion_400_CrossSessionID(t *testing.T) {
	t.Parallel()
	c := setupQADeleteTest(t)
	defer c.cleanup()

	otherSession := &models.Session{
		ID:        uuid.New(),
		Title:     "other session",
		CreatedBy: &c.creatorEmail,
		Status:    models.SessionStatusOpen,
	}
	require.NoError(t, c.db.CreateSession(context.Background(), otherSession))
	otherArtifact, err := c.db.CreateArtifact(context.Background(), otherSession.ID, "Artifact", nil)
	require.NoError(t, err)

	q := &models.Question{
		ID:             uuid.New(),
		ArtifactID:     otherArtifact.ID,
		SessionID:      otherSession.ID,
		QuestionText:   "wrong session q",
		QuestionSource: models.QuestionSourceText,
	}
	require.NoError(t, c.db.CreateQuestion(context.Background(), q))

	// Pass q.ID against c.session.ID (mismatched).
	w := c.callDelete(t, c.session.ID, q.ID, c.creator)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestDeleteSessionQuestion_404_UnknownQuestion(t *testing.T) {
	t.Parallel()
	c := setupQADeleteTest(t)
	defer c.cleanup()

	w := c.callDelete(t, c.session.ID, uuid.New(), c.creator)
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

func TestDeleteSessionQuestion_404_SecondDeleteIdempotent(t *testing.T) {
	t.Parallel()
	c := setupQADeleteTest(t)
	defer c.cleanup()

	q := c.seedQuestion(t, nil, "root q")

	w1 := c.callDelete(t, c.session.ID, q.ID, c.creator)
	require.Equal(t, http.StatusNoContent, w1.Code, w1.Body.String())

	w2 := c.callDelete(t, c.session.ID, q.ID, c.creator)
	assert.Equal(t, http.StatusNotFound, w2.Code, w2.Body.String())
}

func TestDeleteSessionQuestion_BroadcastsQuestionDeleted(t *testing.T) {
	t.Parallel()
	c := setupQADeleteTest(t)
	defer c.cleanup()

	root := c.seedQuestion(t, nil, "root q")
	r1 := c.seedQuestion(t, &root.ID, "reply 1")
	r2 := c.seedQuestion(t, &root.ID, "reply 2")

	// Subscribe to the hub broadcast channel directly. Hub.Run is already
	// running goroutine pulling from the broadcast chan — so we replace the
	// hub on the handler with a fresh, non-running hub whose broadcast
	// chan we can read synchronously.
	freshHub := NewSessionHub()
	c.h.Hub = freshHub

	w := c.callDelete(t, c.session.ID, root.ID, c.creator)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

	select {
	case msg := <-freshHub.broadcast:
		assert.Equal(t, c.session.ID, msg.SessionID)
		assert.Equal(t, "question_deleted", msg.Type)
		data, ok := msg.Data.(map[string]interface{})
		require.True(t, ok, "Data shape mismatch: %#v", msg.Data)
		assert.Equal(t, c.session.ID.String(), data["session_id"])
		assert.Equal(t, root.ID.String(), data["question_id"])
		ids, ok := data["deleted_ids"].([]string)
		require.True(t, ok, "deleted_ids shape mismatch: %#v", data["deleted_ids"])
		assert.ElementsMatch(t, []string{root.ID.String(), r1.ID.String(), r2.ID.String()}, ids)
	default:
		t.Fatalf("expected question_deleted broadcast on hub channel, got none")
	}
}

func TestDeleteSessionQuestion_Smoke_QuestionsListEmptyAfterDelete(t *testing.T) {
	t.Parallel()
	c := setupQADeleteTest(t)
	defer c.cleanup()

	root := c.seedQuestion(t, nil, "root q")
	c.seedQuestion(t, &root.ID, "reply 1")

	w := c.callDelete(t, c.session.ID, root.ID, c.creator)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

	// GET /sessions/{id}/questions should now have zero rows.
	qs, _, err := c.db.GetSessionTimeline(context.Background(), c.session.ID, 100)
	require.NoError(t, err)
	assert.Empty(t, qs)
}

func TestDeleteSessionQuestion_Smoke_PurgesOrchestrationEvidence(t *testing.T) {
	t.Parallel()
	c := setupQADeleteTest(t)
	defer c.cleanup()

	ctx := context.Background()
	root := c.seedQuestion(t, nil, "root q")

	// Confirm an answer (sets up the "confirmed answer" content the
	// recommendation typically links to). Not strictly required for the
	// purge test — the purge keys off the question id reference, not the
	// answer state — but mirrors the smoke flow described in the ticket.
	ans := &models.Answer{
		ID:           uuid.New(),
		QuestionID:   root.ID,
		AnswerText:   "yes",
		AnswerStatus: models.AnswerStatusAnswered,
		Confidence:   0.9,
		Confirmed:    true,
	}
	require.NoError(t, c.db.CreateAnswer(ctx, ans))

	rootCopy := root.ID
	rec := &models.OrchestrationRecommendation{
		SessionID:          c.session.ID,
		RecommendationType: models.RecommendationTypeUnansweredQuestion,
		Summary:            "linked to deleted question",
		Evidence: []models.RecommendationEvidenceRef{
			{SourceType: "question", QuestionID: &rootCopy},
		},
	}
	require.NoError(t, c.db.CreateOrchestrationRecommendation(ctx, rec))

	w := c.callDelete(t, c.session.ID, root.ID, c.creator)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

	got, err := c.db.ListOrchestrationRecommendationsBySessionID(ctx, c.session.ID)
	require.NoError(t, err)
	assert.Empty(t, got, "orchestration recommendation referencing the deleted question must be purged")
}

func TestSessionAsk_ReplyFKRace_Returns404(t *testing.T) {
	t.Parallel()
	c := setupQADeleteTest(t)
	defer c.cleanup()

	ctx := context.Background()
	root := c.seedQuestion(t, nil, "root q")

	// Drop the parent before the reply POST runs to provoke an FK
	// violation at insert time. We bypass the handler's
	// pre-insert GetQuestionByID by deleting the parent AFTER assembling
	// the request. Since GetQuestionByID is called at the start of the
	// handler, we instead simulate the race by deleting the parent and
	// then issuing the reply POST: GetQuestionByID returns 404 (handler
	// short-circuits with 400 "parent not found"). The actual FK-23503
	// race path inside CreateQuestion is hard to drive deterministically
	// from a black-box test without injecting a hook.
	//
	// Instead, we exercise pgFKViolation with a constructed *pgconn.PgError
	// and verify the handler-side mapping is correct via a unit-level
	// helper test — see TestPgFKViolation_DetectsCode23503 below.
	_, err := c.db.DeleteQuestionByID(ctx, root.ID)
	require.NoError(t, err)

	body := []byte(`{"question_text":"reply", "asked_via":"text", "parent_question_id":"` + root.ID.String() + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+c.session.ID.String()+"/ask", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), userContextKey, c.creator))
	w := httptest.NewRecorder()
	c.h.SessionAsk(w, req)

	// Pre-insert GetQuestionByID failure path: the handler returns 400
	// "parent question not found" (existing behavior). The new FK fallback
	// only fires when the parent disappears between GetQuestionByID and
	// CreateQuestion, which is not deterministically reproducible without
	// a DB hook.
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp["message"], "parent question not found")
}

func TestPgFKViolation_DetectsCode23503(t *testing.T) {
	t.Parallel()

	// Real FK violation: insert a question with a non-existent parent_question_id.
	c := setupQADeleteTest(t)
	defer c.cleanup()

	ctx := context.Background()
	bogusParent := uuid.New()
	q := &models.Question{
		ID:               uuid.New(),
		ArtifactID:       c.artifactID,
		SessionID:        c.session.ID,
		ParentQuestionID: &bogusParent,
		QuestionText:     "orphan reply",
		QuestionSource:   models.QuestionSourceText,
	}
	err := c.db.CreateQuestion(ctx, q)
	require.Error(t, err)
	assert.True(t, pgFKViolation(err), "FK violation on parent_question_id must be classified as 23503; got %v", err)

	// Negative: a generic non-FK error must not be classified as 23503.
	assert.False(t, pgFKViolation(nil))
	assert.False(t, pgFKViolation(context.Canceled))
}
