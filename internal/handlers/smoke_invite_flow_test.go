package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/auth"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSmoke_InviteFlow_CreateResolveRegisterAndAccept covers Flow 3: full invite lifecycle.
//
//  1. Creator creates invitation for participant email.
//  2. Token is resolved (simulating frontend token lookup).
//  3. New user registers + accepts invitation via RegisterAndAcceptInvitation.
//  4. Session membership is confirmed in the DB.
func TestSmoke_InviteFlow_CreateResolveRegisterAndAccept(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersWithInvitations(t)
	defer cleanup()

	ctx := context.Background()

	// Seed: creator user and a session they own.
	creatorEmail := "creator+inviteflow@smoke.test"
	creator := &models.User{
		ID:          uuid.New(),
		Email:       creatorEmail,
		DisplayName: "Smoke Creator",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleCreator,
	}
	require.NoError(t, h.DB.CreateUser(ctx, creator))
	sess := &models.LoginSession{
		ID:        uuid.New(),
		UserID:    creator.ID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	require.NoError(t, h.DB.CreateLoginSession(ctx, sess))
	session := &models.Session{
		ID:        uuid.New(),
		Title:     "Invite Smoke Session",
		CreatedBy: &creatorEmail,
		Status:    models.SessionStatusOpen,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, h.DB.CreateSession(ctx, session))

	// --- Step 1: Creator sends invitation ---
	invBody, _ := json.Marshal(map[string]any{"email": "participant+inviteflow@smoke.test", "role": "participant"})
	invReq := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID.String()+"/invitations", bytes.NewReader(invBody))
	invReq.Header.Set("Content-Type", "application/json")
	invReq.URL.Path = "/api/sessions/" + session.ID.String() + "/invitations"
	invReq.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: sess.ID.String()})
	invW := httptest.NewRecorder()
	h.RequireAuth(h.CreateInvitation)(invW, invReq)

	require.Equal(t, http.StatusCreated, invW.Code, invW.Body.String())
	var invResp map[string]any
	require.NoError(t, json.NewDecoder(invW.Body).Decode(&invResp))
	inv, ok := invResp["invitation"].(map[string]any)
	require.True(t, ok, "response missing 'invitation' key")
	assert.Equal(t, "participant+inviteflow@smoke.test", inv["invited_email"])
	assert.Equal(t, "participant", inv["invited_role"])
	assert.Equal(t, "pending", inv["status"])

	// Extract token from accept_url ("/accept-invite?token=XXX")
	acceptURL, _ := inv["accept_url"].(string)
	require.NotEmpty(t, acceptURL, "expected accept_url in invitation response")
	parsed, err := url.Parse(acceptURL)
	require.NoError(t, err)
	token := parsed.Query().Get("token")
	require.NotEmpty(t, token, "expected token in accept_url query string")

	// --- Step 2: Resolve the token (simulates frontend pre-flight) ---
	resolveReq := httptest.NewRequest(http.MethodGet, "/api/invitations/resolve?token="+token, nil)
	resolveW := httptest.NewRecorder()
	h.ResolveInvitation(resolveW, resolveReq)

	require.Equal(t, http.StatusOK, resolveW.Code, resolveW.Body.String())
	var resolveResp map[string]any
	require.NoError(t, json.NewDecoder(resolveW.Body).Decode(&resolveResp))
	resolvedInv, ok := resolveResp["invitation"].(map[string]any)
	require.True(t, ok, "resolve response missing 'invitation' key")
	assert.Equal(t, "participant+inviteflow@smoke.test", resolvedInv["invited_email"])

	// --- Step 3: New participant registers and accepts ---
	regBody, _ := json.Marshal(map[string]any{
		"token":    token,
		"name":     "Smoke Participant",
		"password": "SmokePass123!",
	})
	regReq := httptest.NewRequest(http.MethodPost, "/api/invitations/register-and-accept", bytes.NewReader(regBody))
	regReq.Header.Set("Content-Type", "application/json")
	regW := httptest.NewRecorder()
	h.RegisterAndAcceptInvitation(regW, regReq)

	require.Equal(t, http.StatusOK, regW.Code, regW.Body.String())
	var regResp map[string]any
	require.NoError(t, json.NewDecoder(regW.Body).Decode(&regResp))
	assert.Equal(t, true, regResp["ok"])
	assert.Equal(t, session.ID.String(), regResp["session_id"])

	userPayload, ok := regResp["user"].(map[string]any)
	require.True(t, ok, "expected 'user' in register-and-accept response")
	assert.Equal(t, "participant+inviteflow@smoke.test", userPayload["email"])
	assert.Equal(t, "participant", userPayload["global_role"])
	assert.Equal(t, "active", userPayload["status"])

	// --- Step 4: Confirm session membership in DB ---
	newUser, err := h.DB.GetUserByEmail(ctx, "participant+inviteflow@smoke.test")
	require.NoError(t, err)
	require.NotNil(t, newUser, "new user should exist after registration")
	assert.Equal(t, models.GlobalRoleParticipant, newUser.GlobalRole)

	memberships, err := h.DB.GetSessionMemberships(ctx, session.ID)
	require.NoError(t, err)
	var found bool
	for _, m := range memberships {
		if m.UserID == newUser.ID {
			found = true
			assert.Equal(t, "participant", m.Role)
		}
	}
	assert.True(t, found, "expected new participant to be a session member")
}

// TestSmoke_InviteFlow_DuplicateInviteConflicts verifies that inviting an existing member returns 409.
func TestSmoke_InviteFlow_DuplicateInviteConflicts(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersWithInvitations(t)
	defer cleanup()

	ctx := context.Background()
	creatorEmail := "creator+dup@smoke.test"
	creator := &models.User{
		ID:          uuid.New(),
		Email:       creatorEmail,
		DisplayName: "Dup Creator",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleCreator,
	}
	require.NoError(t, h.DB.CreateUser(ctx, creator))
	member := &models.User{
		ID:          uuid.New(),
		Email:       "member+dup@smoke.test",
		DisplayName: "Existing Member",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleParticipant,
	}
	require.NoError(t, h.DB.CreateUser(ctx, member))
	session := &models.Session{
		ID:        uuid.New(),
		Title:     "Dup Invite Session",
		CreatedBy: &creatorEmail,
		Status:    models.SessionStatusOpen,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, h.DB.CreateSession(ctx, session))
	require.NoError(t, h.DB.CreateSessionMembership(ctx, session.ID, member.ID, "participant", &creator.ID))

	sess := &models.LoginSession{
		ID:        uuid.New(),
		UserID:    creator.ID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	require.NoError(t, h.DB.CreateLoginSession(ctx, sess))

	body, _ := json.Marshal(map[string]any{"email": "member+dup@smoke.test", "role": "participant"})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID.String()+"/invitations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.URL.Path = "/api/sessions/" + session.ID.String() + "/invitations"
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: sess.ID.String()})
	w := httptest.NewRecorder()
	h.RequireAuth(h.CreateInvitation)(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}
