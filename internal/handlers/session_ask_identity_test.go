package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildAskSessionContext_AssignsParticipantRoleForInvitedAsker(t *testing.T) {
	creator := "creator@example.com"
	asker := "invitee@example.com"
	session := &models.Session{CreatedBy: &creator}

	ctx := buildAskSessionContext(session, &asker)
	require.NotNil(t, ctx.AskerRole)
	assert.Equal(t, "participant", *ctx.AskerRole)
	require.NotNil(t, ctx.AskerEmail)
	assert.Equal(t, asker, *ctx.AskerEmail)
	require.NotNil(t, ctx.SessionCreatorEmail)
	assert.Equal(t, creator, *ctx.SessionCreatorEmail)
}

func TestBuildAskSessionContext_AssignsCreatorRoleWhenAskerMatchesCreator(t *testing.T) {
	creator := "creator@example.com"
	asker := " Creator@Example.com "
	session := &models.Session{CreatedBy: &creator}

	ctx := buildAskSessionContext(session, &asker)
	require.NotNil(t, ctx.AskerRole)
	assert.Equal(t, "creator", *ctx.AskerRole)
}

func TestSessionAsk_PersistsAskedByFromAuthenticatedUserContext(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	ctx := context.Background()
	creatorEmail := "creator@example.com"
	session := &models.Session{
		ID:        uuid.New(),
		Title:     "SCRUM-184 identity test",
		CreatedBy: &creatorEmail,
		Status:    models.SessionStatusOpen,
	}
	require.NoError(t, h.DB.CreateSession(ctx, session))
	_, err := h.DB.CreateArtifact(ctx, session.ID, "Identity Artifact", nil)
	require.NoError(t, err)

	participantEmail := "invitee@example.com"
	user := &models.User{ID: uuid.New(), Email: participantEmail}

	body, _ := json.Marshal(map[string]string{"question_text": "Why am I invited to this meeting?"})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID.String()+"/ask", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), userContextKey, user))

	w := httptest.NewRecorder()
	h.SessionAsk(w, req)

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var resp SessionAskResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.NotNil(t, resp.Question)

	questionID, err := uuid.Parse(resp.Question.ID)
	require.NoError(t, err)
	q, err := h.DB.GetQuestionByID(ctx, questionID)
	require.NoError(t, err)
	require.NotNil(t, q.AskedBy)
	assert.Equal(t, participantEmail, *q.AskedBy)
	assert.NotEqual(t, creatorEmail, *q.AskedBy)
}
