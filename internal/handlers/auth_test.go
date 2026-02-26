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

func TestAuthSignup(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	body := map[string]string{
		"email":        "test@example.com",
		"password":     "secret123",
		"display_name": "Test User",
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/signup", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.AuthSignup(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	var me map[string]interface{}
	err := json.NewDecoder(w.Body).Decode(&me)
	require.NoError(t, err)
	assert.Equal(t, "test@example.com", me["email"])
	assert.Equal(t, "Test User", me["display_name"])
	assert.Equal(t, "creator", me["global_role"])
	assert.Equal(t, "active", me["status"])
	assert.NotEmpty(t, me["id"])

	// Cookie should be set
	cookies := w.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == auth.Config.SessionCookieName {
			sessionCookie = c
			break
		}
	}
	require.NotNil(t, sessionCookie, "session cookie should be set")
	assert.True(t, sessionCookie.HttpOnly)
}

func TestAuthLogin_BadPassword(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()
	ctx := context.Background()

	// Create user with password
	user := &models.User{
		ID:          uuid.New(),
		Email:       "login@example.com",
		DisplayName: "Login User",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleCreator,
	}
	require.NoError(t, h.DB.CreateUser(ctx, user))
	hash, _ := auth.HashPassword("correct")
	cred := &models.PasswordCredential{UserID: user.ID, PasswordHash: hash, PasswordUpdatedAt: time.Now()}
	require.NoError(t, h.DB.CreatePasswordCredential(ctx, cred))

	body := map[string]string{"email": "login@example.com", "password": "wrong"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.AuthLogin(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	var errResp map[string]string
	_ = json.NewDecoder(w.Body).Decode(&errResp)
	assert.Equal(t, "invalid credentials", errResp["error"])
}

func TestAuthMe_RequiresAuth(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	w := httptest.NewRecorder()

	h.RequireAuth(h.AuthMe)(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMe_WithValidSession(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()
	ctx := context.Background()

	user := &models.User{
		ID:          uuid.New(),
		Email:       "me@example.com",
		DisplayName: "Me User",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleCreator,
	}
	require.NoError(t, h.DB.CreateUser(ctx, user))
	expiresAt := time.Now().Add(24 * time.Hour)
	session := &models.LoginSession{
		ID:        uuid.New(),
		UserID:    user.ID,
		CreatedAt: time.Now(),
		ExpiresAt: expiresAt,
	}
	require.NoError(t, h.DB.CreateLoginSession(ctx, session))

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: session.ID.String()})
	w := httptest.NewRecorder()

	h.RequireAuth(h.AuthMe)(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var me map[string]interface{}
	err := json.NewDecoder(w.Body).Decode(&me)
	require.NoError(t, err)
	assert.Equal(t, "me@example.com", me["email"])
	assert.Equal(t, "Me User", me["display_name"])
}

func TestAuthMe_DisabledUserBlocked(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()
	ctx := context.Background()

	user := &models.User{
		ID:          uuid.New(),
		Email:       "disabled@example.com",
		DisplayName: "Disabled User",
		Status:      models.UserStatusDisabled,
		GlobalRole:  models.GlobalRoleCreator,
	}
	require.NoError(t, h.DB.CreateUser(ctx, user))
	expiresAt := time.Now().Add(24 * time.Hour)
	session := &models.LoginSession{
		ID:        uuid.New(),
		UserID:    user.ID,
		CreatedAt: time.Now(),
		ExpiresAt: expiresAt,
	}
	require.NoError(t, h.DB.CreateLoginSession(ctx, session))

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: session.ID.String()})
	w := httptest.NewRecorder()

	h.RequireAuth(h.AuthMe)(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	var errResp map[string]string
	_ = json.NewDecoder(w.Body).Decode(&errResp)
	assert.Equal(t, "account disabled", errResp["error"])
}
