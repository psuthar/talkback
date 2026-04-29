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

func TestCreateInvitation_RequiresAuth(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersWithInvitations(t)
	defer cleanup()

	ctx := context.Background()
	sessionID := uuid.New()
	session := &models.Session{
		ID:     sessionID,
		Title:  "Test",
		Status: models.SessionStatusOpen,
	}
	require.NoError(t, h.DB.CreateSession(ctx, session))

	body := map[string]string{"email": "invitee@example.com", "role": "participant"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID.String()+"/invitations", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.URL.Path = "/api/sessions/" + sessionID.String() + "/invitations"
	w := httptest.NewRecorder()

	h.RequireAuth(h.CreateInvitation)(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestCreateInvitation_Success(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersWithInvitations(t)
	defer cleanup()

	ctx := context.Background()
	creatorEmail := "creator@example.com"
	creator := &models.User{
		ID:          uuid.New(),
		Email:       creatorEmail,
		DisplayName: "Creator",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleCreator,
	}
	require.NoError(t, h.DB.CreateUser(ctx, creator))
	session := &models.Session{
		ID:         uuid.New(),
		Title:      "Test Session",
		CreatedBy:  &creatorEmail,
		Status:     models.SessionStatusOpen,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	require.NoError(t, h.DB.CreateSession(ctx, session))
	sess := &models.LoginSession{
		ID:        uuid.New(),
		UserID:    creator.ID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	require.NoError(t, h.DB.CreateLoginSession(ctx, sess))

	body := map[string]string{"email": "invitee@example.com", "role": "participant"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID.String()+"/invitations", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.URL.Path = "/api/sessions/" + session.ID.String() + "/invitations"
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: sess.ID.String()})
	w := httptest.NewRecorder()

	h.RequireAuth(h.CreateInvitation)(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	var res map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&res))
	inv, ok := res["invitation"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "invitee@example.com", inv["invited_email"])
	assert.Equal(t, "participant", inv["invited_role"])
	assert.Equal(t, "pending", inv["status"])
	assert.Contains(t, inv["accept_url"], "/accept-invite?token=")
	assert.NotEmpty(t, inv["session_title"])
}

func TestCreateInvitation_AlreadyMember_Conflict(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersWithInvitations(t)
	defer cleanup()

	ctx := context.Background()
	creatorEmail := "creator@example.com"
	creator := &models.User{
		ID:          uuid.New(),
		Email:       creatorEmail,
		DisplayName: "Creator",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleCreator,
	}
	require.NoError(t, h.DB.CreateUser(ctx, creator))
	memberEmail := "member@example.com"
	member := &models.User{
		ID:          uuid.New(),
		Email:       memberEmail,
		DisplayName: "Member",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleCreator,
	}
	require.NoError(t, h.DB.CreateUser(ctx, member))
	session := &models.Session{
		ID:         uuid.New(),
		Title:      "Test",
		CreatedBy:  &creatorEmail,
		Status:     models.SessionStatusOpen,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
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

	body := map[string]string{"email": memberEmail, "role": "participant"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID.String()+"/invitations", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.URL.Path = "/api/sessions/" + session.ID.String() + "/invitations"
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: sess.ID.String()})
	w := httptest.NewRecorder()

	h.RequireAuth(h.CreateInvitation)(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestCreateInvitation_DecisionMakerRole_Success(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersWithInvitations(t)
	defer cleanup()

	ctx := context.Background()
	creatorEmail := "creator-dm@example.com"
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
		Title:     "Decision Session",
		CreatedBy: &creatorEmail,
		Status:    models.SessionStatusOpen,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, h.DB.CreateSession(ctx, session))
	sess := &models.LoginSession{
		ID:        uuid.New(),
		UserID:    creator.ID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	require.NoError(t, h.DB.CreateLoginSession(ctx, sess))

	body := map[string]string{"email": "dm@example.com", "role": "decision_maker"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID.String()+"/invitations", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.URL.Path = "/api/sessions/" + session.ID.String() + "/invitations"
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: sess.ID.String()})
	w := httptest.NewRecorder()

	h.RequireAuth(h.CreateInvitation)(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	var res map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&res))
	inv, ok := res["invitation"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "decision_maker", inv["invited_role"])
	assert.Equal(t, "pending", inv["status"])
}

func TestCreateInvitation_InvalidRole_BadRequest(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersWithInvitations(t)
	defer cleanup()

	ctx := context.Background()
	creatorEmail := "creator-bad@example.com"
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
		Title:     "Bad Role Session",
		CreatedBy: &creatorEmail,
		Status:    models.SessionStatusOpen,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, h.DB.CreateSession(ctx, session))
	sess := &models.LoginSession{
		ID:        uuid.New(),
		UserID:    creator.ID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	require.NoError(t, h.DB.CreateLoginSession(ctx, sess))

	body := map[string]string{"email": "bad@example.com", "role": "admin"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID.String()+"/invitations", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.URL.Path = "/api/sessions/" + session.ID.String() + "/invitations"
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: sess.ID.String()})
	w := httptest.NewRecorder()

	h.RequireAuth(h.CreateInvitation)(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var res map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&res))
	assert.Contains(t, res["error"], "decision_maker")
}

// TestCreateSessionMembership_DecisionMakerRole verifies the migrated CHECK constraint
// accepts decision_maker rows directly at the DB layer.
func TestCreateSessionMembership_DecisionMakerRole(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersWithInvitations(t)
	defer cleanup()

	ctx := context.Background()
	creatorEmail := "owner-dm@example.com"
	owner := &models.User{
		ID:          uuid.New(),
		Email:       creatorEmail,
		DisplayName: "Owner",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleCreator,
	}
	require.NoError(t, h.DB.CreateUser(ctx, owner))
	memberEmail := "dm-member@example.com"
	member := &models.User{
		ID:          uuid.New(),
		Email:       memberEmail,
		DisplayName: "DM Member",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleParticipant,
	}
	require.NoError(t, h.DB.CreateUser(ctx, member))
	session := &models.Session{
		ID:        uuid.New(),
		Title:     "Membership Session",
		CreatedBy: &creatorEmail,
		Status:    models.SessionStatusOpen,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, h.DB.CreateSession(ctx, session))

	require.NoError(t, h.DB.CreateSessionMembership(ctx, session.ID, member.ID, "decision_maker", &owner.ID))
}

// TestListInvitations_IncludesAcceptedByUserID verifies that after an invitation is
// accepted, the list response surfaces the new accepted_by_user_id field with the
// correct user UUID. Pending rows must report a nil/null value.
func TestListInvitations_IncludesAcceptedByUserID(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersWithInvitations(t)
	defer cleanup()

	ctx := context.Background()
	inviteeEmail := "acceptedby+listing@smoke.test"
	_, ls, sess, _ := seedInvitationContext(t, h, inviteeEmail, "acceptedby")

	// Create the invitee user and a login session so they can accept.
	invitee := &models.User{
		ID:          uuid.New(),
		Email:       inviteeEmail,
		DisplayName: "Accepted By Listing",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleParticipant,
	}
	require.NoError(t, h.DB.CreateUser(ctx, invitee))
	inviteeLS := &models.LoginSession{
		ID:        uuid.New(),
		UserID:    invitee.ID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	require.NoError(t, h.DB.CreateLoginSession(ctx, inviteeLS))

	// Look up invitation ID from the creator's list view.
	listReq := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sess.ID.String()+"/invitations", nil)
	listReq.URL.Path = "/api/sessions/" + sess.ID.String() + "/invitations"
	listReq.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	listW := httptest.NewRecorder()
	h.RequireAuth(h.ListInvitations)(listW, listReq)
	require.Equal(t, http.StatusOK, listW.Code, listW.Body.String())
	var listResp map[string]any
	require.NoError(t, json.NewDecoder(listW.Body).Decode(&listResp))
	pendingList, _ := listResp["invitations"].([]any)
	require.Len(t, pendingList, 1)
	pendingInv := pendingList[0].(map[string]any)
	// Pending invite -> accepted_by_user_id must be nil/absent.
	assert.Nil(t, pendingInv["accepted_by_user_id"], "pending invitation must report no accepter")
	invIDStr, _ := pendingInv["id"].(string)
	require.NotEmpty(t, invIDStr)

	// Invitee accepts via /api/invitations/:id/accept.
	acceptReq := httptest.NewRequest(http.MethodPost, "/api/invitations/"+invIDStr+"/accept", nil)
	acceptReq.URL.Path = "/api/invitations/" + invIDStr + "/accept"
	acceptReq.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: inviteeLS.ID.String()})
	acceptW := httptest.NewRecorder()
	h.RequireAuth(h.AcceptInvitationByID)(acceptW, acceptReq)
	require.Equal(t, http.StatusOK, acceptW.Code, acceptW.Body.String())

	// Re-list and assert the accepted row reports the correct accepted_by_user_id.
	listReq2 := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sess.ID.String()+"/invitations", nil)
	listReq2.URL.Path = "/api/sessions/" + sess.ID.String() + "/invitations"
	listReq2.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	listW2 := httptest.NewRecorder()
	h.RequireAuth(h.ListInvitations)(listW2, listReq2)
	require.Equal(t, http.StatusOK, listW2.Code, listW2.Body.String())
	var listResp2 map[string]any
	require.NoError(t, json.NewDecoder(listW2.Body).Decode(&listResp2))
	acceptedList, _ := listResp2["invitations"].([]any)
	require.Len(t, acceptedList, 1)
	acceptedInv := acceptedList[0].(map[string]any)
	assert.Equal(t, "accepted", acceptedInv["status"])
	assert.Equal(t, invitee.ID.String(), acceptedInv["accepted_by_user_id"], "accepted_by_user_id must be the invitee's user id")
}

func TestResolveInvitation_NoToken_BadRequest(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersWithInvitations(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/invitations/resolve", nil)
	w := httptest.NewRecorder()

	h.ResolveInvitation(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
