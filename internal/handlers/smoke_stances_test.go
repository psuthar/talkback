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

// seedSessionWithDecision creates a session that has a primary_decision set,
// which is required for stances to be accepted.
func seedSessionWithDecision(t *testing.T, h *Handlers, suffix string) (*models.User, *models.LoginSession, *models.Session) {
	t.Helper()
	ctx := context.Background()
	email := "stancecreator+" + suffix + "@smoke.test"
	creator := &models.User{
		ID:          uuid.New(),
		Email:       email,
		DisplayName: "Stance Creator " + suffix,
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleCreator,
	}
	require.NoError(t, h.DB.CreateUser(ctx, creator))
	ls := &models.LoginSession{
		ID:        uuid.New(),
		UserID:    creator.ID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	require.NoError(t, h.DB.CreateLoginSession(ctx, ls))
	sess := &models.Session{
		ID:        uuid.New(),
		Title:     "Stance Session " + suffix,
		CreatedBy: &email,
		Status:    models.SessionStatusOpen,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, h.DB.CreateSession(ctx, sess))
	// Set primary_decision via UpdateSessionContext (CreateSession INSERT omits this field).
	decision := "Approve the APAC expansion budget."
	require.NoError(t, h.DB.UpdateSessionContext(ctx, sess.ID, nil, &decision, nil))
	sess.PrimaryDecision = &decision
	return creator, ls, sess
}

// TestSmoke_Stances_SubmitAndRetrieve covers the decision-intelligence stance flow:
// authenticated user submits a stance → aggregate is updated → GET stances reflects it.
func TestSmoke_Stances_SubmitAndRetrieve(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	creator, ls, sess := seedSessionWithDecision(t, h, "submit-retrieve")

	// --- Step 1: Submit a stance ---
	rationale := "The budget is justified by Q3 data."
	body, _ := json.Marshal(map[string]any{
		"stance":    "agree",
		"rationale": rationale,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sess.ID.String()+"/stance", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.URL.Path = "/api/sessions/" + sess.ID.String() + "/stance"
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	w := httptest.NewRecorder()
	h.RequireAuth(h.SessionSubmitStance)(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var submitResp stancesResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&submitResp))
	require.NotNil(t, submitResp.MyStance)
	assert.Equal(t, "agree", submitResp.MyStance.Stance)
	require.NotNil(t, submitResp.Aggregate)

	// --- Step 2: GET stances reflects the submitted stance ---
	getReq := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sess.ID.String()+"/stances", nil)
	getReq.URL.Path = "/api/sessions/" + sess.ID.String() + "/stances"
	getReq.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	gW := httptest.NewRecorder()
	h.RequireAuth(h.SessionGetStances)(gW, getReq)

	require.Equal(t, http.StatusOK, gW.Code, gW.Body.String())
	var getResp stancesResponse
	require.NoError(t, json.NewDecoder(gW.Body).Decode(&getResp))
	require.NotNil(t, getResp.Aggregate)
	require.NotNil(t, getResp.MyStance)
	assert.Equal(t, "agree", getResp.MyStance.Stance)

	// Verify the aggregate has at least 1 agree.
	assert.GreaterOrEqual(t, getResp.Aggregate.Agree, 1)

	// Verify responses list contains the creator's stance.
	assert.NotNil(t, getResp.Responses)
	var found bool
	for _, r := range getResp.Responses {
		if r.UserID == creator.ID {
			found = true
			assert.Equal(t, "agree", r.Stance)
		}
	}
	assert.True(t, found, "creator's stance must appear in responses")
}

// TestSmoke_Stances_UpdateStance verifies that submitting a different stance overwrites the
// previous one (upsert behavior).
func TestSmoke_Stances_UpdateStance(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	_, ls, sess := seedSessionWithDecision(t, h, "update-stance")

	submitStance := func(stance string) stancesResponse {
		body, _ := json.Marshal(map[string]any{"stance": stance})
		req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sess.ID.String()+"/stance", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.URL.Path = "/api/sessions/" + sess.ID.String() + "/stance"
		req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
		w := httptest.NewRecorder()
		h.RequireAuth(h.SessionSubmitStance)(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var resp stancesResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		return resp
	}

	resp1 := submitStance("agree")
	require.NotNil(t, resp1.MyStance)
	assert.Equal(t, "agree", resp1.MyStance.Stance)
	assert.Equal(t, 1, resp1.Aggregate.Agree)
	assert.Equal(t, 0, resp1.Aggregate.Disagree)

	resp2 := submitStance("disagree")
	require.NotNil(t, resp2.MyStance)
	assert.Equal(t, "disagree", resp2.MyStance.Stance)
	// After update: agree count drops, disagree rises.
	assert.Equal(t, 0, resp2.Aggregate.Agree)
	assert.Equal(t, 1, resp2.Aggregate.Disagree)
}

// TestSmoke_Stances_DeleteStance verifies that deleting a stance clears it.
func TestSmoke_Stances_DeleteStance(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	_, ls, sess := seedSessionWithDecision(t, h, "delete-stance")

	// Submit a stance first.
	body, _ := json.Marshal(map[string]any{"stance": "agree"})
	postReq := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sess.ID.String()+"/stance", bytes.NewReader(body))
	postReq.Header.Set("Content-Type", "application/json")
	postReq.URL.Path = "/api/sessions/" + sess.ID.String() + "/stance"
	postReq.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	postW := httptest.NewRecorder()
	h.RequireAuth(h.SessionSubmitStance)(postW, postReq)
	require.Equal(t, http.StatusOK, postW.Code, postW.Body.String())

	// Delete it.
	delReq := httptest.NewRequest(http.MethodDelete, "/api/sessions/"+sess.ID.String()+"/stance", nil)
	delReq.URL.Path = "/api/sessions/" + sess.ID.String() + "/stance"
	delReq.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	delW := httptest.NewRecorder()
	h.RequireAuth(h.SessionDeleteStance)(delW, delReq)

	require.Equal(t, http.StatusOK, delW.Code, delW.Body.String())
	var delResp stancesResponse
	require.NoError(t, json.NewDecoder(delW.Body).Decode(&delResp))
	assert.Nil(t, delResp.MyStance)
	assert.Equal(t, 0, delResp.Aggregate.Agree)
}

// TestSmoke_Stances_NoDecisionReturns422 verifies that submitting a stance to a session
// without a primary_decision returns 422 Unprocessable Entity.
func TestSmoke_Stances_NoDecisionReturns422(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	// Create a session WITHOUT a primary_decision.
	ctx := context.Background()
	email := "nodecision+stance@smoke.test"
	creator := &models.User{
		ID:         uuid.New(),
		Email:      email,
		Status:     models.UserStatusActive,
		GlobalRole: models.GlobalRoleCreator,
	}
	require.NoError(t, h.DB.CreateUser(ctx, creator))
	ls := &models.LoginSession{
		ID:        uuid.New(),
		UserID:    creator.ID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	require.NoError(t, h.DB.CreateLoginSession(ctx, ls))
	sess := &models.Session{
		ID:        uuid.New(),
		Title:     "No Decision Session",
		CreatedBy: &email,
		Status:    models.SessionStatusOpen,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, h.DB.CreateSession(ctx, sess))

	body, _ := json.Marshal(map[string]any{"stance": "agree"})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sess.ID.String()+"/stance", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.URL.Path = "/api/sessions/" + sess.ID.String() + "/stance"
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	w := httptest.NewRecorder()
	h.RequireAuth(h.SessionSubmitStance)(w, req)

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
}

// TestSmoke_Stances_InvalidStanceValueReturns400 verifies that an unknown stance value is rejected.
func TestSmoke_Stances_InvalidStanceValueReturns400(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	_, ls, sess := seedSessionWithDecision(t, h, "invalid-stance")

	body, _ := json.Marshal(map[string]any{"stance": "maybe"})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sess.ID.String()+"/stance", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.URL.Path = "/api/sessions/" + sess.ID.String() + "/stance"
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	w := httptest.NewRecorder()
	h.RequireAuth(h.SessionSubmitStance)(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
