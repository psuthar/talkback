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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// addDecisionMakerMember creates an active user with a login session and adds them to the
// session as a decision_maker. Returns the user, the login-session id, and the email.
func addDecisionMakerMember(t *testing.T, h *Handlers, sessionID uuid.UUID, suffix string) (*models.User, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	email := "dm+" + suffix + "@smoke.test"
	u := &models.User{
		ID:          uuid.New(),
		Email:       email,
		DisplayName: "DM " + suffix,
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleParticipant,
	}
	require.NoError(t, h.DB.CreateUser(ctx, u))
	require.NoError(t, h.DB.CreateSessionMembership(ctx, sessionID, u.ID, "decision_maker", nil))
	ls := &models.LoginSession{
		ID:        uuid.New(),
		UserID:    u.ID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	require.NoError(t, h.DB.CreateLoginSession(ctx, ls))
	return u, ls.ID
}

// TestGetDecisionMakerReadiness_DBLayer covers the readiness computation directly against the DB
// across the salient mixed-role scenarios.
func TestGetDecisionMakerReadiness_DBLayer(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	ctx := context.Background()
	_, _, sess := seedSessionWithDecision(t, h, "db-readiness")

	// 0 DMs: total=0, voted=0, ready=false (gate does not apply).
	r0, err := h.DB.GetDecisionMakerReadiness(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, r0.DecisionMakerTotal)
	assert.Equal(t, 0, r0.DecisionMakerVoted)
	assert.False(t, r0.ReadyToClose)

	// 2 DMs, 0 voted: ready=false.
	dm1, _ := addDecisionMakerMember(t, h, sess.ID, "db1")
	dm2, _ := addDecisionMakerMember(t, h, sess.ID, "db2")
	r1, err := h.DB.GetDecisionMakerReadiness(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, r1.DecisionMakerTotal)
	assert.Equal(t, 0, r1.DecisionMakerVoted)
	assert.False(t, r1.ReadyToClose)

	// 1 of 2 voted: ready=false.
	_, err = h.DB.CreateOrUpdateStance(ctx, sess.ID, dm1.ID, "agree", nil)
	require.NoError(t, err)
	r2, err := h.DB.GetDecisionMakerReadiness(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, r2.DecisionMakerTotal)
	assert.Equal(t, 1, r2.DecisionMakerVoted)
	assert.False(t, r2.ReadyToClose)

	// 2 of 2 voted, primary_decision set, no decision_outcome: ready=true.
	_, err = h.DB.CreateOrUpdateStance(ctx, sess.ID, dm2.ID, "disagree", nil)
	require.NoError(t, err)
	r3, err := h.DB.GetDecisionMakerReadiness(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, r3.DecisionMakerTotal)
	assert.Equal(t, 2, r3.DecisionMakerVoted)
	assert.True(t, r3.ReadyToClose)

	// decision_outcome set => ready=false (closeout already happened).
	outcome := "Approved by board."
	require.NoError(t, h.DB.UpdateSessionContext(ctx, sess.ID, nil, nil, &outcome))
	r4, err := h.DB.GetDecisionMakerReadiness(ctx, sess.ID)
	require.NoError(t, err)
	assert.False(t, r4.ReadyToClose)
}

// TestSmoke_Stances_GetIncludesReadiness verifies the GET stances endpoint exposes the readiness
// payload alongside aggregate / responses for both creator and decision_maker views.
func TestSmoke_Stances_GetIncludesReadiness(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	_, ls, sess := seedSessionWithDecision(t, h, "get-readiness")

	// Two decision_makers; one votes.
	dm1, _ := addDecisionMakerMember(t, h, sess.ID, "g1")
	_, _ = addDecisionMakerMember(t, h, sess.ID, "g2")
	_, err := h.DB.CreateOrUpdateStance(context.Background(), sess.ID, dm1.ID, "agree", nil)
	require.NoError(t, err)

	getReq := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sess.ID.String()+"/stances", nil)
	getReq.URL.Path = "/api/sessions/" + sess.ID.String() + "/stances"
	getReq.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	w := httptest.NewRecorder()
	h.RequireAuth(h.SessionGetStances)(w, getReq)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp stancesResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.NotNil(t, resp.Readiness)
	assert.Equal(t, 2, resp.Readiness.DecisionMakerTotal)
	assert.Equal(t, 1, resp.Readiness.DecisionMakerVoted)
	assert.False(t, resp.Readiness.ReadyToClose)
}

// TestSmoke_Stances_PostFlipsReadinessReady verifies that when the last decision_maker submits
// their stance, the POST stance response carries ready_to_close=true.
func TestSmoke_Stances_PostFlipsReadinessReady(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	_, _, sess := seedSessionWithDecision(t, h, "post-readiness-flip")

	dm1, ls1 := addDecisionMakerMember(t, h, sess.ID, "f1")
	dm2, ls2 := addDecisionMakerMember(t, h, sess.ID, "f2")
	_ = dm2

	// First DM votes — ready_to_close should still be false.
	body, _ := json.Marshal(map[string]any{"stance": "agree"})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sess.ID.String()+"/stance", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.URL.Path = "/api/sessions/" + sess.ID.String() + "/stance"
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls1.String()})
	w := httptest.NewRecorder()
	h.RequireAuth(h.SessionSubmitStance)(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var first stancesResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&first))
	require.NotNil(t, first.Readiness)
	assert.Equal(t, 2, first.Readiness.DecisionMakerTotal)
	assert.Equal(t, 1, first.Readiness.DecisionMakerVoted)
	assert.False(t, first.Readiness.ReadyToClose)
	_ = dm1

	// Second DM votes — ready_to_close should flip to true.
	body2, _ := json.Marshal(map[string]any{"stance": "disagree"})
	req2 := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sess.ID.String()+"/stance", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	req2.URL.Path = "/api/sessions/" + sess.ID.String() + "/stance"
	req2.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls2.String()})
	w2 := httptest.NewRecorder()
	h.RequireAuth(h.SessionSubmitStance)(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code, w2.Body.String())
	var second stancesResponse
	require.NoError(t, json.NewDecoder(w2.Body).Decode(&second))
	require.NotNil(t, second.Readiness)
	assert.Equal(t, 2, second.Readiness.DecisionMakerTotal)
	assert.Equal(t, 2, second.Readiness.DecisionMakerVoted)
	assert.True(t, second.Readiness.ReadyToClose)
}

// TestSmoke_Closeout_DecisionMakerCannotClose verifies that a decision_maker member is
// rejected when attempting to record a decision_outcome (only creator/admin can close).
func TestSmoke_Closeout_DecisionMakerCannotClose(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	_, _, sess := seedSessionWithDecision(t, h, "dm-cannot-close")
	_, dmLS := addDecisionMakerMember(t, h, sess.ID, "closer")

	body, _ := json.Marshal(map[string]any{"decision_outcome": "DM tries to close."})
	req := httptest.NewRequest(http.MethodPatch, "/sessions/"+sess.ID.String(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: dmLS.String()})
	w := httptest.NewRecorder()
	h.RequireAuth(h.UpdateSessionStatus)(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())

	// Verify decision_outcome was NOT written.
	dbSess, err := h.DB.GetSession(context.Background(), sess.ID)
	require.NoError(t, err)
	require.NotNil(t, dbSess)
	assert.Nil(t, dbSess.DecisionOutcome)
}

// TestSmoke_Closeout_StanceLockRegression confirms that once a creator records decision_outcome,
// stance submissions and deletions remain blocked with 409 (existing post-closeout lock semantics).
func TestSmoke_Closeout_StanceLockRegression(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	_, ls, sess := seedSessionWithDecision(t, h, "stance-lock-regression")
	_, dmLS := addDecisionMakerMember(t, h, sess.ID, "lock-dm")

	// Creator records decision_outcome.
	body1, _ := json.Marshal(map[string]any{"decision_outcome": "Final."})
	req1 := httptest.NewRequest(http.MethodPatch, "/sessions/"+sess.ID.String(), bytes.NewReader(body1))
	req1.Header.Set("Content-Type", "application/json")
	req1.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	w1 := httptest.NewRecorder()
	h.RequireAuth(h.UpdateSessionStatus)(w1, req1)
	require.Equal(t, http.StatusOK, w1.Code, w1.Body.String())

	// DM tries to submit a stance — should be blocked with 409.
	body2, _ := json.Marshal(map[string]any{"stance": "agree"})
	req2 := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sess.ID.String()+"/stance", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	req2.URL.Path = "/api/sessions/" + sess.ID.String() + "/stance"
	req2.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: dmLS.String()})
	w2 := httptest.NewRecorder()
	h.RequireAuth(h.SessionSubmitStance)(w2, req2)
	assert.Equal(t, http.StatusConflict, w2.Code)

	// DM tries to delete — also blocked with 409.
	req3 := httptest.NewRequest(http.MethodDelete, "/api/sessions/"+sess.ID.String()+"/stance", nil)
	req3.URL.Path = "/api/sessions/" + sess.ID.String() + "/stance"
	req3.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: dmLS.String()})
	w3 := httptest.NewRecorder()
	h.RequireAuth(h.SessionDeleteStance)(w3, req3)
	assert.Equal(t, http.StatusConflict, w3.Code)

	// Readiness should report ready_to_close=false because decision_outcome is set.
	getReq := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sess.ID.String()+"/stances", nil)
	getReq.URL.Path = "/api/sessions/" + sess.ID.String() + "/stances"
	getReq.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	gW := httptest.NewRecorder()
	h.RequireAuth(h.SessionGetStances)(gW, getReq)
	require.Equal(t, http.StatusOK, gW.Code, gW.Body.String())
	var resp stancesResponse
	require.NoError(t, json.NewDecoder(gW.Body).Decode(&resp))
	require.NotNil(t, resp.Readiness)
	assert.False(t, resp.Readiness.ReadyToClose)
}
