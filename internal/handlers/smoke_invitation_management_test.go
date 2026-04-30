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

// seedInvitationContext creates a creator + session + sends a pending invitation.
// Returns the creator, their login session, the session, and the invitation token.
func seedInvitationContext(t *testing.T, h *Handlers, inviteeEmail, suffix string) (*models.User, *models.LoginSession, *models.Session, string) {
	t.Helper()
	ctx := context.Background()
	creatorEmail := "creator+inv" + suffix + "@smoke.test"
	creator := &models.User{
		ID:          uuid.New(),
		Email:       creatorEmail,
		DisplayName: "Inv Creator " + suffix,
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
		Title:     "Inv Session " + suffix,
		CreatedBy: &creatorEmail,
		Status:    models.SessionStatusOpen,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, h.DB.CreateSession(ctx, sess))

	// Create invitation via handler.
	body, _ := json.Marshal(map[string]any{"email": inviteeEmail, "role": "participant"})
	invReq := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sess.ID.String()+"/invitations", bytes.NewReader(body))
	invReq.Header.Set("Content-Type", "application/json")
	invReq.URL.Path = "/api/sessions/" + sess.ID.String() + "/invitations"
	invReq.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	invW := httptest.NewRecorder()
	h.RequireAuth(h.CreateInvitation)(invW, invReq)
	require.Equal(t, http.StatusCreated, invW.Code, invW.Body.String())

	var invResp map[string]any
	require.NoError(t, json.NewDecoder(invW.Body).Decode(&invResp))
	inv, ok := invResp["invitation"].(map[string]any)
	require.True(t, ok)
	acceptURL, _ := inv["accept_url"].(string)
	parsed, err := url.Parse(acceptURL)
	require.NoError(t, err)
	token := parsed.Query().Get("token")
	require.NotEmpty(t, token)

	return creator, ls, sess, token
}

// TestSmoke_InvitationMgmt_ListInvitationsForSession verifies GET /api/sessions/:id/invitations
// returns the invitation list for the session creator.
func TestSmoke_InvitationMgmt_ListInvitationsForSession(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersWithInvitations(t)
	defer cleanup()

	inviteeEmail := "listinvitee+smoke@smoke.test"
	_, ls, sess, _ := seedInvitationContext(t, h, inviteeEmail, "list")

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sess.ID.String()+"/invitations", nil)
	req.URL.Path = "/api/sessions/" + sess.ID.String() + "/invitations"
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	w := httptest.NewRecorder()
	h.RequireAuth(h.ListInvitations)(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	list, ok := resp["invitations"].([]any)
	require.True(t, ok, "response must have 'invitations' array")
	// SCRUM-226: listing also includes the synthetic creator row, so the
	// invitee is no longer guaranteed to be at index 0; look up by email.
	inv := findInvitationByEmail(t, list, inviteeEmail)
	assert.Equal(t, inviteeEmail, inv["invited_email"])
	assert.Equal(t, "pending", inv["status"])
}

// TestSmoke_InvitationMgmt_ListPendingForInvitee verifies GET /api/invitations/pending
// returns the pending invitation for the invited user's email.
func TestSmoke_InvitationMgmt_ListPendingForInvitee(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersWithInvitations(t)
	defer cleanup()

	ctx := context.Background()
	inviteeEmail := "pendinginvitee+smoke@smoke.test"
	_, _, _, _ = seedInvitationContext(t, h, inviteeEmail, "pending")

	// Create the invitee user so they can authenticate.
	invitee := &models.User{
		ID:          uuid.New(),
		Email:       inviteeEmail,
		DisplayName: "Pending Invitee",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleParticipant,
	}
	require.NoError(t, h.DB.CreateUser(ctx, invitee))
	inviteels := &models.LoginSession{
		ID:        uuid.New(),
		UserID:    invitee.ID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	require.NoError(t, h.DB.CreateLoginSession(ctx, inviteels))

	req := httptest.NewRequest(http.MethodGet, "/api/invitations/pending", nil)
	req.URL.Path = "/api/invitations/pending"
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: inviteels.ID.String()})
	w := httptest.NewRecorder()
	h.RequireAuth(h.ListPendingInvitations)(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	list, ok := resp["invitations"].([]any)
	require.True(t, ok)
	require.Len(t, list, 1, "invitee must see their pending invitation")
}

// TestSmoke_InvitationMgmt_RevokeInvitation verifies that the creator can revoke a pending
// invitation — subsequent resolve returns an error, and list shows it as revoked.
func TestSmoke_InvitationMgmt_RevokeInvitation(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersWithInvitations(t)
	defer cleanup()

	inviteeEmail := "revokeinvitee+smoke@smoke.test"
	_, ls, sess, token := seedInvitationContext(t, h, inviteeEmail, "revoke")

	// Get invitation ID from the list.
	listReq := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sess.ID.String()+"/invitations", nil)
	listReq.URL.Path = "/api/sessions/" + sess.ID.String() + "/invitations"
	listReq.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	listW := httptest.NewRecorder()
	h.RequireAuth(h.ListInvitations)(listW, listReq)
	require.Equal(t, http.StatusOK, listW.Code, listW.Body.String())
	var listResp map[string]any
	require.NoError(t, json.NewDecoder(listW.Body).Decode(&listResp))
	invList, _ := listResp["invitations"].([]any)
	inv := findInvitationByEmail(t, invList, inviteeEmail)
	invIDStr, _ := inv["id"].(string)
	require.NotEmpty(t, invIDStr)

	// Revoke the invitation.
	revokeReq := httptest.NewRequest(http.MethodPost, "/api/invitations/"+invIDStr+"/revoke", nil)
	revokeReq.URL.Path = "/api/invitations/" + invIDStr + "/revoke"
	revokeReq.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	revokeW := httptest.NewRecorder()
	h.RequireAuth(h.RevokeInvitation)(revokeW, revokeReq)

	require.Equal(t, http.StatusOK, revokeW.Code, revokeW.Body.String())
	var revokeResp map[string]any
	require.NoError(t, json.NewDecoder(revokeW.Body).Decode(&revokeResp))
	assert.Equal(t, true, revokeResp["ok"])

	// Resolving the revoked token must return error (not 200).
	resolveReq := httptest.NewRequest(http.MethodGet, "/api/invitations/resolve?token="+token, nil)
	resolveW := httptest.NewRecorder()
	h.ResolveInvitation(resolveW, resolveReq)
	assert.NotEqual(t, http.StatusOK, resolveW.Code, "revoked invitation token must not resolve to 200")
}

// TestSmoke_InvitationMgmt_GetInvitationLink verifies GET /api/invitations/:id/link returns
// the accept URL for a pending invitation when called by the session creator.
func TestSmoke_InvitationMgmt_GetInvitationLink(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersWithInvitations(t)
	defer cleanup()

	inviteeEmail := "linkget+smoke@smoke.test"
	_, ls, sess, _ := seedInvitationContext(t, h, inviteeEmail, "getlink")

	// Retrieve invitation ID.
	listReq := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sess.ID.String()+"/invitations", nil)
	listReq.URL.Path = "/api/sessions/" + sess.ID.String() + "/invitations"
	listReq.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	listW := httptest.NewRecorder()
	h.RequireAuth(h.ListInvitations)(listW, listReq)
	require.Equal(t, http.StatusOK, listW.Code)
	var listResp map[string]any
	require.NoError(t, json.NewDecoder(listW.Body).Decode(&listResp))
	invList, _ := listResp["invitations"].([]any)
	inv := findInvitationByEmail(t, invList, inviteeEmail)
	invIDStr, _ := inv["id"].(string)
	require.NotEmpty(t, invIDStr)

	// Get the invite link.
	linkReq := httptest.NewRequest(http.MethodGet, "/api/invitations/"+invIDStr+"/link", nil)
	linkReq.URL.Path = "/api/invitations/" + invIDStr + "/link"
	linkReq.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	linkW := httptest.NewRecorder()
	h.RequireAuth(h.GetInvitationLink)(linkW, linkReq)

	require.Equal(t, http.StatusOK, linkW.Code, linkW.Body.String())
	var linkResp map[string]any
	require.NoError(t, json.NewDecoder(linkW.Body).Decode(&linkResp))
	acceptURL, _ := linkResp["accept_url"].(string)
	assert.NotEmpty(t, acceptURL, "GetInvitationLink must return a non-empty accept_url")
	// Token must be parseable from the URL.
	parsed, err := url.Parse(acceptURL)
	require.NoError(t, err)
	assert.NotEmpty(t, parsed.Query().Get("token"), "accept_url must contain a token query param")
}
