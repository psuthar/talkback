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

// seedMembershipScenario creates: a creator user (also a creator-role membership),
// a session, a target user with the given starting role, and a login session
// for the creator. Returns the actors so the caller can drive PATCH requests.
func seedMembershipScenario(
	t *testing.T,
	h *Handlers,
	suffix string,
	targetRole string,
) (creator *models.User, creatorLS *models.LoginSession, target *models.User, sess *models.Session) {
	t.Helper()
	ctx := context.Background()

	creatorEmail := "membership-creator+" + suffix + "@smoke.test"
	creator = &models.User{
		ID:          uuid.New(),
		Email:       creatorEmail,
		DisplayName: "Membership Creator " + suffix,
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleCreator,
	}
	require.NoError(t, h.DB.CreateUser(ctx, creator))

	creatorLS = &models.LoginSession{
		ID:        uuid.New(),
		UserID:    creator.ID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	require.NoError(t, h.DB.CreateLoginSession(ctx, creatorLS))

	sess = &models.Session{
		ID:        uuid.New(),
		Title:     "Membership Session " + suffix,
		CreatedBy: &creatorEmail,
		Status:    models.SessionStatusOpen,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, h.DB.CreateSession(ctx, sess))
	// Self-membership for the creator so the last-creator guardrail has a row to read.
	require.NoError(t, h.DB.CreateSessionMembership(ctx, sess.ID, creator.ID, "creator", nil))

	target = &models.User{
		ID:          uuid.New(),
		Email:       "membership-target+" + suffix + "@smoke.test",
		DisplayName: "Membership Target " + suffix,
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleParticipant,
	}
	require.NoError(t, h.DB.CreateUser(ctx, target))
	require.NoError(t, h.DB.CreateSessionMembership(ctx, sess.ID, target.ID, targetRole, &creator.ID))
	return creator, creatorLS, target, sess
}

func patchMembership(t *testing.T, h *Handlers, sessionID, userID uuid.UUID, body any, lsID *uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	url := "/api/sessions/" + sessionID.String() + "/memberships/" + userID.String()
	req := httptest.NewRequest(http.MethodPatch, url, bytes.NewReader(b))
	req.URL.Path = url
	req.Header.Set("Content-Type", "application/json")
	if lsID != nil {
		req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: lsID.String()})
	}
	w := httptest.NewRecorder()
	h.RequireAuth(h.UpdateSessionMembership)(w, req)
	return w
}

func TestUpdateSessionMembership_PromoteParticipantToDecisionMaker(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	_, creatorLS, target, sess := seedMembershipScenario(t, h, "promote", "participant")
	w := patchMembership(t, h, sess.ID, target.ID, map[string]string{"role": "decision_maker"}, &creatorLS.ID)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	m, _ := resp["membership"].(map[string]any)
	require.NotNil(t, m)
	assert.Equal(t, "decision_maker", m["role"])
}

func TestUpdateSessionMembership_AdminCanChangeRole(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	ctx := context.Background()
	_, _, target, sess := seedMembershipScenario(t, h, "admin", "participant")
	admin := &models.User{
		ID:          uuid.New(),
		Email:       "admin-membership@smoke.test",
		DisplayName: "Admin",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleAdmin,
	}
	require.NoError(t, h.DB.CreateUser(ctx, admin))
	adminLS := &models.LoginSession{
		ID:        uuid.New(),
		UserID:    admin.ID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	require.NoError(t, h.DB.CreateLoginSession(ctx, adminLS))

	w := patchMembership(t, h, sess.ID, target.ID, map[string]string{"role": "creator"}, &adminLS.ID)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestUpdateSessionMembership_NonCreatorForbidden(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	ctx := context.Background()
	_, _, target, sess := seedMembershipScenario(t, h, "forbidden", "participant")
	intruder := &models.User{
		ID:          uuid.New(),
		Email:       "intruder@smoke.test",
		DisplayName: "Intruder",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleCreator,
	}
	require.NoError(t, h.DB.CreateUser(ctx, intruder))
	intruderLS := &models.LoginSession{
		ID:        uuid.New(),
		UserID:    intruder.ID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	require.NoError(t, h.DB.CreateLoginSession(ctx, intruderLS))

	w := patchMembership(t, h, sess.ID, target.ID, map[string]string{"role": "decision_maker"}, &intruderLS.ID)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestUpdateSessionMembership_InvalidRoleBadRequest(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	_, creatorLS, target, sess := seedMembershipScenario(t, h, "invalid", "participant")
	w := patchMembership(t, h, sess.ID, target.ID, map[string]string{"role": "admin"}, &creatorLS.ID)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	var body map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Contains(t, body["error"], "decision_maker")
}

func TestUpdateSessionMembership_MembershipNotFound(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	_, creatorLS, _, sess := seedMembershipScenario(t, h, "missing", "participant")
	w := patchMembership(t, h, sess.ID, uuid.New(), map[string]string{"role": "creator"}, &creatorLS.ID)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUpdateSessionMembership_LastCreatorGuardrail(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	// The seed gives us exactly one creator membership (the actor's own).
	// Demoting their target role does not exercise the guard, so we PATCH the
	// creator's own membership row to participant — that should hit the 409.
	creator, creatorLS, _, sess := seedMembershipScenario(t, h, "last-creator", "participant")
	w := patchMembership(t, h, sess.ID, creator.ID, map[string]string{"role": "participant"}, &creatorLS.ID)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	var body map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Contains(t, body["error"], "creator")
}

func TestUpdateSessionMembership_StanceSurvivesDemotion(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	ctx := context.Background()
	_, creatorLS, target, sess := seedMembershipScenario(t, h, "stance-survive", "decision_maker")
	// Set primary_decision and seed a stance for the decision_maker.
	pd := "Approve the change?"
	require.NoError(t, h.DB.UpdateSessionContext(ctx, sess.ID, nil, &pd, nil))
	rationale := "Reasonable risk."
	stance, err := h.DB.CreateOrUpdateStance(ctx, sess.ID, target.ID, "agree", &rationale)
	require.NoError(t, err)
	require.NotNil(t, stance)

	// Demote decision_maker -> participant.
	w := patchMembership(t, h, sess.ID, target.ID, map[string]string{"role": "participant"}, &creatorLS.ID)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Stance row must still exist.
	survived, err := h.DB.GetStanceByUserAndSession(ctx, sess.ID, target.ID)
	require.NoError(t, err)
	require.NotNil(t, survived)
	assert.Equal(t, "agree", survived.Stance)
}

func TestUpdateSessionMembership_NoOpSameRoleSucceeds(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	_, creatorLS, target, sess := seedMembershipScenario(t, h, "noop", "participant")
	w := patchMembership(t, h, sess.ID, target.ID, map[string]string{"role": "participant"}, &creatorLS.ID)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}
