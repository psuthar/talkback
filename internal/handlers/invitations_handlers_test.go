package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
	pendingInv := findInvitationByEmail(t, pendingList, inviteeEmail)
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
	acceptedInv := findInvitationByEmail(t, acceptedList, inviteeEmail)
	assert.Equal(t, "accepted", acceptedInv["status"])
	assert.Equal(t, invitee.ID.String(), acceptedInv["accepted_by_user_id"], "accepted_by_user_id must be the invitee's user id")
}

// TestListInvitations_IncludesCreator pins the SCRUM-226 contract: the
// Members listing surfaces the session creator alongside invitees, even
// though the creator has no session_invitations row. The synthetic row is
// sourced from session_memberships and carries the creator's email,
// role='creator', and status='accepted'.
func TestListInvitations_IncludesCreator(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersWithInvitations(t)
	defer cleanup()

	inviteeEmail := "invitee-creator-listing@smoke.test"
	creator, ls, sess, _ := seedInvitationContext(t, h, inviteeEmail, "creatorlisting")

	listReq := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sess.ID.String()+"/invitations", nil)
	listReq.URL.Path = "/api/sessions/" + sess.ID.String() + "/invitations"
	listReq.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	listW := httptest.NewRecorder()
	h.RequireAuth(h.ListInvitations)(listW, listReq)
	require.Equal(t, http.StatusOK, listW.Code, listW.Body.String())
	var listResp map[string]any
	require.NoError(t, json.NewDecoder(listW.Body).Decode(&listResp))
	list, _ := listResp["invitations"].([]any)

	creatorRow := findInvitationByEmail(t, list, creator.Email)
	assert.Equal(t, "accepted", creatorRow["status"], "synthetic creator row must be marked accepted")
	assert.Equal(t, "creator", creatorRow["invited_role"])
	assert.Equal(t, creator.ID.String(), creatorRow["accepted_by_user_id"], "synthetic creator row must reference the creator's user id")
}

// findInvitationByEmail returns the listing row whose invited_email matches the
// supplied address (case-insensitive). The Members listing now includes a
// synthetic row for the session creator (SCRUM-226), so tests that previously
// indexed list[0] must look up by email instead.
func findInvitationByEmail(t *testing.T, list []any, email string) map[string]any {
	t.Helper()
	for _, item := range list {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if got, _ := row["invited_email"].(string); strings.EqualFold(got, email) {
			return row
		}
	}
	t.Fatalf("invitation for %q not found in listing of %d items", email, len(list))
	return nil
}

// TestListInvitations_RoleReflectsMembershipAfterPATCH originally covered
// SCRUM-217 (decision_maker promotion). SCRUM-225 generalises the contract:
// the listing endpoint must reflect the live session_memberships.role on the
// matching accepted row regardless of which target role is chosen, including
// 'creator' which was the regression that motivated SCRUM-225. The test now
// walks participant -> decision_maker -> creator and asserts the listing
// follows each PATCH.
func TestListInvitations_RoleReflectsMembershipAfterPATCH(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersWithInvitations(t)
	defer cleanup()

	ctx := context.Background()
	inviteeEmail := "rolepatch+listing@smoke.test"
	_, creatorLS, sess, _ := seedInvitationContext(t, h, inviteeEmail, "rolepatch")

	// Create the invitee user + login session and accept the invite by ID.
	invitee := &models.User{
		ID:          uuid.New(),
		Email:       inviteeEmail,
		DisplayName: "Role Patch Invitee",
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

	// Look up invitation id from the listing.
	listReq := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sess.ID.String()+"/invitations", nil)
	listReq.URL.Path = "/api/sessions/" + sess.ID.String() + "/invitations"
	listReq.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: creatorLS.ID.String()})
	listW := httptest.NewRecorder()
	h.RequireAuth(h.ListInvitations)(listW, listReq)
	require.Equal(t, http.StatusOK, listW.Code, listW.Body.String())
	var listResp map[string]any
	require.NoError(t, json.NewDecoder(listW.Body).Decode(&listResp))
	pending, _ := listResp["invitations"].([]any)
	pendingInv := findInvitationByEmail(t, pending, inviteeEmail)
	assert.Equal(t, "participant", pendingInv["invited_role"])
	invIDStr, _ := pendingInv["id"].(string)
	require.NotEmpty(t, invIDStr)

	// Invitee accepts via /api/invitations/:id/accept — creates the membership row.
	acceptReq := httptest.NewRequest(http.MethodPost, "/api/invitations/"+invIDStr+"/accept", nil)
	acceptReq.URL.Path = "/api/invitations/" + invIDStr + "/accept"
	acceptReq.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: inviteeLS.ID.String()})
	acceptW := httptest.NewRecorder()
	h.RequireAuth(h.AcceptInvitationByID)(acceptW, acceptReq)
	require.Equal(t, http.StatusOK, acceptW.Code, acceptW.Body.String())

	patchAndAssertListing := func(targetRole string) {
		t.Helper()
		patchBody, _ := json.Marshal(map[string]string{"role": targetRole})
		patchURL := "/api/sessions/" + sess.ID.String() + "/memberships/" + invitee.ID.String()
		patchReq := httptest.NewRequest(http.MethodPatch, patchURL, bytes.NewReader(patchBody))
		patchReq.URL.Path = patchURL
		patchReq.Header.Set("Content-Type", "application/json")
		patchReq.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: creatorLS.ID.String()})
		patchW := httptest.NewRecorder()
		h.RequireAuth(h.UpdateSessionMembership)(patchW, patchReq)
		require.Equal(t, http.StatusOK, patchW.Code, patchW.Body.String())

		listReq := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sess.ID.String()+"/invitations", nil)
		listReq.URL.Path = "/api/sessions/" + sess.ID.String() + "/invitations"
		listReq.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: creatorLS.ID.String()})
		listW := httptest.NewRecorder()
		h.RequireAuth(h.ListInvitations)(listW, listReq)
		require.Equal(t, http.StatusOK, listW.Code, listW.Body.String())
		var listResp map[string]any
		require.NoError(t, json.NewDecoder(listW.Body).Decode(&listResp))
		accepted, _ := listResp["invitations"].([]any)
		acceptedInv := findInvitationByEmail(t, accepted, inviteeEmail)
		assert.Equal(t, "accepted", acceptedInv["status"])
		assert.Equal(t, targetRole, acceptedInv["invited_role"], "ListInvitations must reflect the post-PATCH role on accepted rows for target=%s", targetRole)
		assert.Equal(t, invitee.ID.String(), acceptedInv["accepted_by_user_id"])

		m, err := h.DB.GetSessionMembership(ctx, sess.ID, invitee.ID)
		require.NoError(t, err)
		require.NotNil(t, m)
		assert.Equal(t, targetRole, m.Role)
	}

	patchAndAssertListing("decision_maker")
	patchAndAssertListing("creator") // SCRUM-225 regression coverage
}

// TestListInvitations_AcceptedRowReflectsLiveMembershipRoleEvenIfInvitedRoleStale
// pins SCRUM-225's contract: the listing reads role from session_memberships
// for accepted rows, so even if session_invitations.invited_role drifts (e.g.
// a leftover snapshot from an older code path), the Members panel still sees
// the correct live role. This is the test that would have caught the original
// SCRUM-217 bug class without needing per-role coverage.
func TestListInvitations_AcceptedRowReflectsLiveMembershipRoleEvenIfInvitedRoleStale(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersWithInvitations(t)
	defer cleanup()

	ctx := context.Background()
	inviteeEmail := "stale+listing@smoke.test"
	_, creatorLS, sess, _ := seedInvitationContext(t, h, inviteeEmail, "stale")

	invitee := &models.User{
		ID:          uuid.New(),
		Email:       inviteeEmail,
		DisplayName: "Stale Invitee",
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

	// Look up invitation id and accept.
	listReq := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sess.ID.String()+"/invitations", nil)
	listReq.URL.Path = "/api/sessions/" + sess.ID.String() + "/invitations"
	listReq.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: creatorLS.ID.String()})
	listW := httptest.NewRecorder()
	h.RequireAuth(h.ListInvitations)(listW, listReq)
	require.Equal(t, http.StatusOK, listW.Code, listW.Body.String())
	var listResp map[string]any
	require.NoError(t, json.NewDecoder(listW.Body).Decode(&listResp))
	pending := listResp["invitations"].([]any)
	invIDStr, _ := findInvitationByEmail(t, pending, inviteeEmail)["id"].(string)
	require.NotEmpty(t, invIDStr)

	acceptReq := httptest.NewRequest(http.MethodPost, "/api/invitations/"+invIDStr+"/accept", nil)
	acceptReq.URL.Path = "/api/invitations/" + invIDStr + "/accept"
	acceptReq.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: inviteeLS.ID.String()})
	acceptW := httptest.NewRecorder()
	h.RequireAuth(h.AcceptInvitationByID)(acceptW, acceptReq)
	require.Equal(t, http.StatusOK, acceptW.Code, acceptW.Body.String())

	// Promote to creator via PATCH.
	patchBody, _ := json.Marshal(map[string]string{"role": "creator"})
	patchURL := "/api/sessions/" + sess.ID.String() + "/memberships/" + invitee.ID.String()
	patchReq := httptest.NewRequest(http.MethodPatch, patchURL, bytes.NewReader(patchBody))
	patchReq.URL.Path = patchURL
	patchReq.Header.Set("Content-Type", "application/json")
	patchReq.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: creatorLS.ID.String()})
	patchW := httptest.NewRecorder()
	h.RequireAuth(h.UpdateSessionMembership)(patchW, patchReq)
	require.Equal(t, http.StatusOK, patchW.Code, patchW.Body.String())

	// Force session_invitations.invited_role to a stale 'participant' value
	// directly in the database, simulating either a regression in dual-write
	// coverage or the natural state now that the dual-write was removed.
	_, err := h.DB.Pool.Exec(ctx, `
		UPDATE session_invitations
		SET invited_role = 'participant'
		WHERE session_id = $1 AND accepted_by_user_id = $2
	`, sess.ID, invitee.ID)
	require.NoError(t, err)

	// Listing must still report 'creator' because it reads from session_memberships.
	listReq2 := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sess.ID.String()+"/invitations", nil)
	listReq2.URL.Path = "/api/sessions/" + sess.ID.String() + "/invitations"
	listReq2.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: creatorLS.ID.String()})
	listW2 := httptest.NewRecorder()
	h.RequireAuth(h.ListInvitations)(listW2, listReq2)
	require.Equal(t, http.StatusOK, listW2.Code, listW2.Body.String())
	var listResp2 map[string]any
	require.NoError(t, json.NewDecoder(listW2.Body).Decode(&listResp2))
	accepted := listResp2["invitations"].([]any)
	acceptedInv := findInvitationByEmail(t, accepted, inviteeEmail)
	assert.Equal(t, "creator", acceptedInv["invited_role"], "listing must source role from memberships, not from session_invitations.invited_role")
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

// TestListInvitations_BatchedUserLookup_SCRUM506 pins the SCRUM-506 contract:
// ListInvitations resolves all inviter and member display names / emails via
// a single batched GetUsersByIDs call rather than N+1 GetUserByID lookups
// inside the per-invitation and per-membership loops. The N+1 pattern was the
// likely cause of the 341–444 ms p95 latency regression observed in obs#158
// and obs#219.
//
// The test creates a session with 5 invitations from the same creator and
// asserts the response is correct after the batched refactor — same data
// shape, no missing inviter_name fields, creator surfaced once via the
// SCRUM-226 synthetic-member path. The previous N+1 implementation would
// have executed 5 + 1 = 6 GetUserByID round trips; v2 executes one.
func TestListInvitations_BatchedUserLookup_SCRUM506(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersWithInvitations(t)
	defer cleanup()

	// Seed creator + session + first invitation via the existing helper.
	firstInviteeEmail := "scrum506-invitee-0@smoke.test"
	creator, ls, sess, _ := seedInvitationContext(t, h, firstInviteeEmail, "scrum506")

	// Create 4 additional invitations against the same session by the same
	// creator. Each one previously incurred a separate GetUserByID lookup for
	// the inviter; under the batched refactor, all five share the one query.
	for i := 1; i < 5; i++ {
		email := fmt.Sprintf("scrum506-invitee-%d@smoke.test", i)
		body, _ := json.Marshal(map[string]any{"email": email, "role": "participant"})
		invReq := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sess.ID.String()+"/invitations", bytes.NewReader(body))
		invReq.Header.Set("Content-Type", "application/json")
		invReq.URL.Path = "/api/sessions/" + sess.ID.String() + "/invitations"
		invReq.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
		invW := httptest.NewRecorder()
		h.RequireAuth(h.CreateInvitation)(invW, invReq)
		require.Equal(t, http.StatusCreated, invW.Code, "creating invitation %d: %s", i, invW.Body.String())
	}

	// List invitations and verify all 5 are returned with the correct
	// inviter_name (set, because the batched lookup resolved the creator).
	listReq := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sess.ID.String()+"/invitations", nil)
	listReq.URL.Path = "/api/sessions/" + sess.ID.String() + "/invitations"
	listReq.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	listW := httptest.NewRecorder()
	h.RequireAuth(h.ListInvitations)(listW, listReq)
	require.Equal(t, http.StatusOK, listW.Code, listW.Body.String())

	var resp map[string]any
	require.NoError(t, json.NewDecoder(listW.Body).Decode(&resp))
	list, ok := resp["invitations"].([]any)
	require.True(t, ok)

	// Expected: 5 pending invitations + 1 synthetic creator membership row
	// (SCRUM-226: the creator has a session_memberships row but no
	// session_invitations row, so they're surfaced as a synthetic row).
	require.Len(t, list, 6, "expected 5 invitations + 1 synthetic creator row")

	// Every pending invitation must carry the creator's DisplayName as
	// inviter_name. A regression to N+1 would still satisfy this assertion;
	// the value of this test is that the batched path correctly populates
	// inviter_name for every row (regression-bed for the refactor itself).
	pendingCount := 0
	for _, raw := range list {
		item, _ := raw.(map[string]any)
		switch item["status"] {
		case "pending":
			pendingCount++
			assert.Equal(t, creator.DisplayName, item["inviter_name"],
				"pending invitation %s missing inviter_name", item["invited_email"])
		case "accepted":
			// Synthetic creator row — empty inviter_name, accepted_at set.
			assert.Equal(t, "", item["inviter_name"], "synthetic creator row has empty inviter_name")
			assert.Equal(t, creator.Email, item["invited_email"],
				"synthetic creator row carries creator email")
		default:
			t.Fatalf("unexpected status: %v", item["status"])
		}
	}
	assert.Equal(t, 5, pendingCount, "expected 5 pending invitations")
}
