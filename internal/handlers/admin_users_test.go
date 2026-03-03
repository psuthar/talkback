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

// addAdminSessionCookie creates an admin user and login session, adds the session cookie to req.
func addAdminSessionCookie(t *testing.T, h *Handlers, req *http.Request) {
	t.Helper()
	ctx := context.Background()
	admin := &models.User{
		ID:          uuid.New(),
		Email:       "admin@example.com",
		DisplayName: "Admin",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleAdmin,
	}
	require.NoError(t, h.DB.CreateUser(ctx, admin))
	session := &models.LoginSession{
		ID:        uuid.New(),
		UserID:    admin.ID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	require.NoError(t, h.DB.CreateLoginSession(ctx, session))
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: session.ID.String()})
}

// addUserSessionCookie creates a non-admin user and login session, adds the session cookie to req.
func addUserSessionCookie(t *testing.T, h *Handlers, req *http.Request, email string) *models.User {
	t.Helper()
	ctx := context.Background()
	u := &models.User{
		ID:          uuid.New(),
		Email:       email,
		DisplayName: "User",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleCreator,
	}
	require.NoError(t, h.DB.CreateUser(ctx, u))
	session := &models.LoginSession{
		ID:        uuid.New(),
		UserID:    u.ID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	require.NoError(t, h.DB.CreateLoginSession(ctx, session))
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: session.ID.String()})
	return u
}

// addParticipantSessionCookie creates a participant (join-only) user and login session, adds the session cookie to req.
func addParticipantSessionCookie(t *testing.T, h *Handlers, req *http.Request) *models.User {
	t.Helper()
	ctx := context.Background()
	u := &models.User{
		ID:          uuid.New(),
		Email:       "participant@example.com",
		DisplayName: "Participant",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleParticipant,
	}
	require.NoError(t, h.DB.CreateUser(ctx, u))
	session := &models.LoginSession{
		ID:        uuid.New(),
		UserID:    u.ID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	require.NoError(t, h.DB.CreateLoginSession(ctx, session))
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: session.ID.String()})
	return u
}

func TestAdminUsers_GET_Unauthorized(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
	w := httptest.NewRecorder()

	h.RequireAuth(h.RequireAdmin(h.AdminUsersHandler))(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAdminUsers_GET_ForbiddenWhenNotAdmin(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
	addUserSessionCookie(t, h, req, "user@example.com")
	w := httptest.NewRecorder()

	h.RequireAuth(h.RequireAdmin(h.AdminUsersHandler))(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestAdminUsers_GET_Success(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	// Create a user so list is non-empty
	u := &models.User{
		ID:          uuid.New(),
		Email:       "existing@example.com",
		DisplayName: "Existing",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleCreator,
	}
	require.NoError(t, h.DB.CreateUser(ctx, u))

	req := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
	addAdminSessionCookie(t, h, req)
	w := httptest.NewRecorder()

	h.RequireAuth(h.RequireAdmin(h.AdminUsersHandler))(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var list []map[string]interface{}
	err := json.NewDecoder(w.Body).Decode(&list)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(list), 1)
	var found bool
	for _, item := range list {
		if item["email"] == "existing@example.com" {
			found = true
			assert.Equal(t, "Existing", item["display_name"])
			assert.Equal(t, "active", item["status"])
			assert.Equal(t, "creator", item["global_role"])
			break
		}
	}
	assert.True(t, found, "expected to find existing user in list")
}

func TestAdminUsers_POST_CreatesUser(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	body := map[string]string{
		"email":        "newuser@example.com",
		"display_name": "New User",
		"password":     "secret123",
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/users", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	addAdminSessionCookie(t, h, req)
	w := httptest.NewRecorder()

	h.RequireAuth(h.RequireAdmin(h.AdminUsersHandler))(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	var me map[string]interface{}
	err := json.NewDecoder(w.Body).Decode(&me)
	require.NoError(t, err)
	assert.Equal(t, "newuser@example.com", me["email"])
	assert.Equal(t, "New User", me["display_name"])
	assert.Equal(t, "creator", me["global_role"])
	assert.Equal(t, "active", me["status"])
	assert.NotEmpty(t, me["id"])
	_, hasPassword := me["password"]
	assert.False(t, hasPassword)
}

func TestAdminUsers_POST_ConflictWhenEmailExists(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	existing := &models.User{
		ID:          uuid.New(),
		Email:       "dup@example.com",
		DisplayName: "Existing",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleCreator,
	}
	require.NoError(t, h.DB.CreateUser(ctx, existing))

	body := map[string]string{
		"email":        "dup@example.com",
		"display_name": "Duplicate",
		"password":     "secret123",
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/users", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	addAdminSessionCookie(t, h, req)
	w := httptest.NewRecorder()

	h.RequireAuth(h.RequireAdmin(h.AdminUsersHandler))(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestAdminUsers_DELETE_Success(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	toDelete := &models.User{
		ID:          uuid.New(),
		Email:       "todelete@example.com",
		DisplayName: "To Delete",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleCreator,
	}
	require.NoError(t, h.DB.CreateUser(ctx, toDelete))

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/users/"+toDelete.ID.String(), nil)
	req.URL.Path = "/api/admin/users/" + toDelete.ID.String()
	addAdminSessionCookie(t, h, req)
	w := httptest.NewRecorder()

	h.RequireAuth(h.RequireAdmin(h.AdminUsersHandler))(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)

	got, _ := h.DB.GetUserByID(ctx, toDelete.ID)
	assert.Nil(t, got)
}

func TestAdminUsers_DELETE_NotFound(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	randomID := uuid.New()
	req := httptest.NewRequest(http.MethodDelete, "/api/admin/users/"+randomID.String(), nil)
	req.URL.Path = "/api/admin/users/" + randomID.String()
	addAdminSessionCookie(t, h, req)
	w := httptest.NewRecorder()

	h.RequireAuth(h.RequireAdmin(h.AdminUsersHandler))(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAdminUsers_DELETE_LastAdmin_Forbidden(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	// Create only one user and make them admin (the "last admin")
	admin := &models.User{
		ID:          uuid.New(),
		Email:       "onlyadmin@example.com",
		DisplayName: "Only Admin",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleAdmin,
	}
	require.NoError(t, h.DB.CreateUser(ctx, admin))
	// Create session for a different admin user that will do the DELETE (so we need 2 admins: one to act, one to be deleted)
	// Actually we need: one admin performing the delete, and one admin being deleted. If we have only one admin (the one being deleted),
	// we can't have an admin session. So we need at least 2 admins, then try to delete one - and we should succeed.
	// The "last admin" check: when we try to delete a user who is admin, and after deletion there would be 0 admins, we forbid.
	// So create 2 admins: admin1 (will do the delete), admin2 (will be deleted - but then we'd have 1 admin left). So we need
	// only one admin in the DB, and we try to delete them. But then who is making the request? We need an admin session to call DELETE.
	// So we need: admin1 (has session, does DELETE), admin2 (only other admin). When we try to delete admin2, count is 2, so we allow.
	// For "last admin" we need: only one admin in DB. So the deleter must be that admin (deleting themselves?). So we have one admin,
	// they're logged in, they try to DELETE themselves (their own id). Then we should return 403.
	// So: create one admin user, add session for them, request DELETE /api/admin/users/:id with that admin's id.
	sess := &models.LoginSession{
		ID:        uuid.New(),
		UserID:    admin.ID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	require.NoError(t, h.DB.CreateLoginSession(ctx, sess))

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/users/"+admin.ID.String(), nil)
	req.URL.Path = "/api/admin/users/" + admin.ID.String()
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: sess.ID.String()})
	w := httptest.NewRecorder()

	h.RequireAuth(h.RequireAdmin(h.AdminUsersHandler))(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	var body map[string]string
	err := json.NewDecoder(w.Body).Decode(&body)
	require.NoError(t, err)
	assert.Contains(t, body["error"], "last admin")
}

func TestAdminUsers_POST_WithRoleParticipant(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	body := map[string]interface{}{
		"email":        "participant@example.com",
		"display_name": "Participant User",
		"password":     "secret123",
		"global_role":  "participant",
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/users", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	addAdminSessionCookie(t, h, req)
	w := httptest.NewRecorder()

	h.RequireAuth(h.RequireAdmin(h.AdminUsersHandler))(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	var me map[string]interface{}
	err := json.NewDecoder(w.Body).Decode(&me)
	require.NoError(t, err)
	assert.Equal(t, "participant@example.com", me["email"])
	assert.Equal(t, "participant", me["global_role"])
}

func TestAdminUsers_PATCH_UpdateRole(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	u := &models.User{
		ID:          uuid.New(),
		Email:       "patchme@example.com",
		DisplayName: "Patch Me",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleCreator,
	}
	require.NoError(t, h.DB.CreateUser(ctx, u))

	body := map[string]string{"global_role": "participant"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/users/"+u.ID.String(), bytes.NewReader(b))
	req.URL.Path = "/api/admin/users/" + u.ID.String()
	req.Header.Set("Content-Type", "application/json")
	addAdminSessionCookie(t, h, req)
	w := httptest.NewRecorder()

	h.RequireAuth(h.RequireAdmin(h.AdminUsersHandler))(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var payload map[string]interface{}
	err := json.NewDecoder(w.Body).Decode(&payload)
	require.NoError(t, err)
	assert.Equal(t, "participant", payload["global_role"])

	got, _ := h.DB.GetUserByID(ctx, u.ID)
	require.NotNil(t, got)
	assert.Equal(t, models.GlobalRoleParticipant, got.GlobalRole)
}

func TestAdminUsers_PATCH_LastAdmin_Forbidden(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	admin := &models.User{
		ID:          uuid.New(),
		Email:       "onlyadmin@example.com",
		DisplayName: "Only Admin",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleAdmin,
	}
	require.NoError(t, h.DB.CreateUser(ctx, admin))
	sess := &models.LoginSession{
		ID:        uuid.New(),
		UserID:    admin.ID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	require.NoError(t, h.DB.CreateLoginSession(ctx, sess))

	body := map[string]string{"global_role": "creator"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/users/"+admin.ID.String(), bytes.NewReader(b))
	req.URL.Path = "/api/admin/users/" + admin.ID.String()
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: sess.ID.String()})
	w := httptest.NewRecorder()

	h.RequireAuth(h.RequireAdmin(h.AdminUsersHandler))(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	var resBody map[string]string
	err := json.NewDecoder(w.Body).Decode(&resBody)
	require.NoError(t, err)
	assert.Contains(t, resBody["error"], "last admin")
}
