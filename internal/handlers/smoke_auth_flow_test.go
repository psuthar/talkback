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

// TestSmoke_Auth_LoginHappyPath covers the full login flow:
// sign up → log out → log in → verify session cookie is set and /me returns correct user.
func TestSmoke_Auth_LoginHappyPath(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	const email = "login+smoke@smoke.test"
	const password = "SmokeLogin123!"
	const displayName = "Smoke Login User"

	// --- Step 1: Sign up ---
	signupBody, _ := json.Marshal(map[string]any{
		"email":        email,
		"password":     password,
		"display_name": displayName,
	})
	signupReq := httptest.NewRequest(http.MethodPost, "/api/auth/signup", bytes.NewReader(signupBody))
	signupReq.Header.Set("Content-Type", "application/json")
	signupW := httptest.NewRecorder()
	h.AuthSignup(signupW, signupReq)
	require.Equal(t, http.StatusCreated, signupW.Code, signupW.Body.String())

	// --- Step 2: Log in ---
	loginBody, _ := json.Marshal(map[string]any{
		"email":    email,
		"password": password,
	})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	h.AuthLogin(loginW, loginReq)

	require.Equal(t, http.StatusOK, loginW.Code, loginW.Body.String())
	var loginResp map[string]any
	require.NoError(t, json.NewDecoder(loginW.Body).Decode(&loginResp))
	assert.Equal(t, email, loginResp["email"])
	assert.Equal(t, displayName, loginResp["display_name"])
	assert.Equal(t, "creator", loginResp["global_role"])
	assert.NotEmpty(t, loginResp["id"])

	// Session cookie must be set.
	cookies := loginW.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == auth.Config.SessionCookieName {
			sessionCookie = c
			break
		}
	}
	require.NotNil(t, sessionCookie, "login must set session cookie")

	// --- Step 3: /me with the new session cookie returns correct user ---
	meReq := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	meReq.AddCookie(sessionCookie)
	meW := httptest.NewRecorder()
	h.RequireAuth(h.AuthMe)(meW, meReq)

	require.Equal(t, http.StatusOK, meW.Code, meW.Body.String())
	var meResp map[string]any
	require.NoError(t, json.NewDecoder(meW.Body).Decode(&meResp))
	assert.Equal(t, email, meResp["email"])
}

// TestSmoke_Auth_LogoutInvalidatesSession verifies that after logout the session cookie
// no longer grants access to /me.
func TestSmoke_Auth_LogoutInvalidatesSession(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	ctx := context.Background()
	user := &models.User{
		ID:          uuid.New(),
		Email:       "logout+smoke@smoke.test",
		DisplayName: "Logout User",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleCreator,
	}
	require.NoError(t, h.DB.CreateUser(ctx, user))
	ls := &models.LoginSession{
		ID:        uuid.New(),
		UserID:    user.ID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	require.NoError(t, h.DB.CreateLoginSession(ctx, ls))
	cookie := &http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()}

	// Confirm /me works before logout.
	meReq := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	meReq.AddCookie(cookie)
	meW := httptest.NewRecorder()
	h.RequireAuth(h.AuthMe)(meW, meReq)
	require.Equal(t, http.StatusOK, meW.Code, "pre-logout /me must return 200")

	// Log out.
	logoutReq := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	logoutReq.AddCookie(cookie)
	logoutW := httptest.NewRecorder()
	h.AuthLogout(logoutW, logoutReq)
	require.Equal(t, http.StatusOK, logoutW.Code, logoutW.Body.String())

	// After logout, same session cookie must return 401.
	meReq2 := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	meReq2.AddCookie(cookie)
	meW2 := httptest.NewRecorder()
	h.RequireAuth(h.AuthMe)(meW2, meReq2)
	assert.Equal(t, http.StatusUnauthorized, meW2.Code, "post-logout /me must return 401")
}

// TestSmoke_Auth_ParticipantListSessionsSeesOnlyInvited verifies that a participant's
// GET /api/sessions returns only sessions they are a member of, not all sessions.
func TestSmoke_Auth_ParticipantListSessionsSeesOnlyInvited(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	ctx := context.Background()

	// Create a participant user.
	participantEmail := "participant+listscope@smoke.test"
	participant := &models.User{
		ID:          uuid.New(),
		Email:       participantEmail,
		DisplayName: "Scoped Participant",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleParticipant,
	}
	require.NoError(t, h.DB.CreateUser(ctx, participant))
	participantLS := &models.LoginSession{
		ID:        uuid.New(),
		UserID:    participant.ID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	require.NoError(t, h.DB.CreateLoginSession(ctx, participantLS))

	creatorEmail := "creator+listscope@smoke.test"

	// Session the participant is invited to.
	invitedSession := &models.Session{
		ID:        uuid.New(),
		Title:     "Invited Session",
		CreatedBy: &creatorEmail,
		Status:    models.SessionStatusOpen,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, h.DB.CreateSession(ctx, invitedSession))
	require.NoError(t, h.DB.CreateSessionMembership(ctx, invitedSession.ID, participant.ID, "participant", nil))

	// Another session the participant is NOT invited to.
	otherSession := &models.Session{
		ID:        uuid.New(),
		Title:     "Not Invited Session",
		CreatedBy: &creatorEmail,
		Status:    models.SessionStatusOpen,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, h.DB.CreateSession(ctx, otherSession))

	// Call GET /api/sessions as the participant.
	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req.URL.Path = "/api/sessions"
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: participantLS.ID.String()})
	w := httptest.NewRecorder()
	h.RequireAuth(h.ListSessions)(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp []SessionWithRole
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))

	var seenIDs []string
	for _, sr := range resp {
		seenIDs = append(seenIDs, sr.Session.ID.String())
		assert.Equal(t, "participant", sr.MyRole, "participant must see sessions with role 'participant'")
	}
	assert.Contains(t, seenIDs, invitedSession.ID.String(), "participant must see invited session")
	assert.NotContains(t, seenIDs, otherSession.ID.String(), "participant must not see uninvited session")
}

// TestSmoke_Auth_ExpiredSessionReturns401 verifies that an expired login session is
// rejected with 401 by RequireAuth.
func TestSmoke_Auth_ExpiredSessionReturns401(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	ctx := context.Background()
	user := &models.User{
		ID:          uuid.New(),
		Email:       "expired+smoke@smoke.test",
		DisplayName: "Expired User",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleCreator,
	}
	require.NoError(t, h.DB.CreateUser(ctx, user))
	// Create a session that is already expired.
	expiredLS := &models.LoginSession{
		ID:        uuid.New(),
		UserID:    user.ID,
		CreatedAt: time.Now().Add(-48 * time.Hour),
		ExpiresAt: time.Now().Add(-1 * time.Hour), // already expired
	}
	require.NoError(t, h.DB.CreateLoginSession(ctx, expiredLS))

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: expiredLS.ID.String()})
	w := httptest.NewRecorder()
	h.RequireAuth(h.AuthMe)(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
