package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/auth"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrchestrationRecommendations_Unauthorized(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	s := createTestSessionForHandlers(t, h.DB, "orch-list-unauth")
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+s.ID.String()+"/orchestration/recommendations", nil)
	w := httptest.NewRecorder()
	h.RequireAuth(h.ListOrchestrationRecommendations)(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestOrchestrationRecommendations_ListAndUpdate(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	_, ls, sess := seedCreatorAndSession(t, h, "orch-list")
	ctx := context.Background()
	rec := &models.OrchestrationRecommendation{
		ID:                 uuid.New(),
		SessionID:          sess.ID,
		RecommendationType: models.RecommendationTypeReviewDraftAnswer,
		Status:             models.RecommendationStatusNew,
		Summary:            "Draft answer awaiting review",
	}
	require.NoError(t, h.DB.CreateOrchestrationRecommendation(ctx, rec))

	listReq := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sess.ID.String()+"/orchestration/recommendations", nil)
	listReq.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	listW := httptest.NewRecorder()
	h.RequireAuth(h.ListOrchestrationRecommendations)(listW, listReq)
	require.Equal(t, http.StatusOK, listW.Code, listW.Body.String())
	var listResp struct {
		Recommendations []models.OrchestrationRecommendation `json:"recommendations"`
	}
	require.NoError(t, json.NewDecoder(listW.Body).Decode(&listResp))
	require.Len(t, listResp.Recommendations, 1)
	assert.Equal(t, rec.ID, listResp.Recommendations[0].ID)

	upBody, _ := json.Marshal(map[string]string{"status": string(models.RecommendationStatusDismissed)})
	upReq := httptest.NewRequest(http.MethodPatch, "/api/sessions/"+sess.ID.String()+"/orchestration/recommendations/"+rec.ID.String(), bytes.NewReader(upBody))
	upReq.Header.Set("Content-Type", "application/json")
	upReq.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	upW := httptest.NewRecorder()
	h.RequireAuth(h.UpdateOrchestrationRecommendationStatus)(upW, upReq)
	require.Equal(t, http.StatusOK, upW.Code, upW.Body.String())

	stored, err := h.DB.ListOrchestrationRecommendationsBySessionID(ctx, sess.ID)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	assert.Equal(t, models.RecommendationStatusDismissed, stored[0].Status)
}

func TestOrchestrationRecommendations_ForbiddenForNonCreator(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()
	ownerEmail := "owner-orch@example.com"
	sess := &models.Session{
		ID:        uuid.New(),
		Title:     "Orch ACL",
		CreatedBy: &ownerEmail,
		Status:    models.SessionStatusOpen,
	}
	require.NoError(t, h.DB.CreateSession(ctx, sess))
	req := httptest.NewRequest(http.MethodGet, "/sessions/"+sess.ID.String()+"/orchestration/recommendations", nil)
	addUserSessionCookie(t, h, req, "other-orch@example.com")
	w := httptest.NewRecorder()
	h.RequireAuth(h.ListOrchestrationRecommendations)(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestOrchestrationRecommendations_Sync(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	_, ls, sess := seedCreatorAndSession(t, h, "orch-sync")
	ctx := context.Background()
	artifact, err := h.DB.CreateArtifact(ctx, sess.ID, "A", nil)
	require.NoError(t, err)
	q := &models.Question{
		ID:             uuid.New(),
		ArtifactID:     artifact.ID,
		SessionID:      sess.ID,
		QuestionText:   "Is there an answer yet?",
		QuestionSource: models.QuestionSourceText,
	}
	require.NoError(t, h.DB.CreateQuestion(ctx, q))

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sess.ID.String()+"/orchestration/recommendations/sync", nil)
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	w := httptest.NewRecorder()
	h.RequireAuth(h.SyncOrchestrationRecommendations)(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	recs, err := h.DB.ListOrchestrationRecommendationsBySessionID(ctx, sess.ID)
	require.NoError(t, err)
	require.NotEmpty(t, recs)
}

