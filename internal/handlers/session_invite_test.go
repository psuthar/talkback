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

func TestSessionInvite_AsCreator_Returns201(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
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
	invitee := &models.User{
		ID:          uuid.New(),
		Email:       "invitee@example.com",
		DisplayName: "Invitee",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleCreator,
	}
	require.NoError(t, h.DB.CreateUser(ctx, invitee))

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

	body := map[string]string{"user_id": invitee.ID.String()}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID.String()+"/invite", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.URL.Path = "/api/sessions/" + session.ID.String() + "/invite"
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: sess.ID.String()})
	w := httptest.NewRecorder()

	h.RequireAuth(h.SessionInvite)(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	exists, err := h.DB.InvitationExists(ctx, session.ID, invitee.ID)
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestSessionInvite_AsInvitedUser_Returns201(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	creatorEmail := "creator2@example.com"
	creator := &models.User{
		ID:          uuid.New(),
		Email:       creatorEmail,
		DisplayName: "Creator",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleCreator,
	}
	require.NoError(t, h.DB.CreateUser(ctx, creator))
	invitedUser := &models.User{
		ID:          uuid.New(),
		Email:       "invited@example.com",
		DisplayName: "Invited",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleCreator,
	}
	require.NoError(t, h.DB.CreateUser(ctx, invitedUser))
	invitee := &models.User{
		ID:          uuid.New(),
		Email:       "newinvitee@example.com",
		DisplayName: "New Invitee",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleCreator,
	}
	require.NoError(t, h.DB.CreateUser(ctx, invitee))

	session := &models.Session{
		ID:         uuid.New(),
		Title:      "Test Session 2",
		CreatedBy:  &creatorEmail,
		Status:     models.SessionStatusOpen,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	require.NoError(t, h.DB.CreateSession(ctx, session))

	// Creator invited "invitedUser"; now invitedUser can invite invitee
	inv := &models.SessionInvitation{
		ID:              uuid.New(),
		SessionID:       session.ID,
		UserID:          invitedUser.ID,
		InvitedByUserID: &creator.ID,
		CreatedAt:       time.Now(),
	}
	require.NoError(t, h.DB.CreateSessionInvitation(ctx, inv))

	sess := &models.LoginSession{
		ID:        uuid.New(),
		UserID:    invitedUser.ID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	require.NoError(t, h.DB.CreateLoginSession(ctx, sess))

	body := map[string]string{"email": invitee.Email}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID.String()+"/invite", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.URL.Path = "/api/sessions/" + session.ID.String() + "/invite"
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: sess.ID.String()})
	w := httptest.NewRecorder()

	h.RequireAuth(h.SessionInvite)(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	exists, err := h.DB.InvitationExists(ctx, session.ID, invitee.ID)
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestSessionInvite_NotCreatorNorInvited_Returns403(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	creatorEmail := "creator3@example.com"
	creator := &models.User{
		ID:          uuid.New(),
		Email:       creatorEmail,
		DisplayName: "Creator",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleCreator,
	}
	require.NoError(t, h.DB.CreateUser(ctx, creator))
	otherUser := &models.User{
		ID:          uuid.New(),
		Email:       "other@example.com",
		DisplayName: "Other",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleCreator,
	}
	require.NoError(t, h.DB.CreateUser(ctx, otherUser))
	invitee := &models.User{
		ID:          uuid.New(),
		Email:       "invitee3@example.com",
		DisplayName: "Invitee",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleCreator,
	}
	require.NoError(t, h.DB.CreateUser(ctx, invitee))

	session := &models.Session{
		ID:         uuid.New(),
		Title:      "Test Session 3",
		CreatedBy:  &creatorEmail,
		Status:     models.SessionStatusOpen,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	require.NoError(t, h.DB.CreateSession(ctx, session))

	sess := &models.LoginSession{
		ID:        uuid.New(),
		UserID:    otherUser.ID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	require.NoError(t, h.DB.CreateLoginSession(ctx, sess))

	body := map[string]string{"user_id": invitee.ID.String()}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID.String()+"/invite", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.URL.Path = "/api/sessions/" + session.ID.String() + "/invite"
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: sess.ID.String()})
	w := httptest.NewRecorder()

	h.RequireAuth(h.SessionInvite)(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestSessionInvite_AlreadyInvited_Returns409(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	creatorEmail := "creator4@example.com"
	creator := &models.User{
		ID:          uuid.New(),
		Email:       creatorEmail,
		DisplayName: "Creator",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleCreator,
	}
	require.NoError(t, h.DB.CreateUser(ctx, creator))
	invitee := &models.User{
		ID:          uuid.New(),
		Email:       "invitee4@example.com",
		DisplayName: "Invitee",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleCreator,
	}
	require.NoError(t, h.DB.CreateUser(ctx, invitee))

	session := &models.Session{
		ID:         uuid.New(),
		Title:      "Test Session 4",
		CreatedBy:  &creatorEmail,
		Status:     models.SessionStatusOpen,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	require.NoError(t, h.DB.CreateSession(ctx, session))

	inv := &models.SessionInvitation{
		ID:              uuid.New(),
		SessionID:       session.ID,
		UserID:          invitee.ID,
		InvitedByUserID: &creator.ID,
		CreatedAt:       time.Now(),
	}
	require.NoError(t, h.DB.CreateSessionInvitation(ctx, inv))

	sess := &models.LoginSession{
		ID:        uuid.New(),
		UserID:    creator.ID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	require.NoError(t, h.DB.CreateLoginSession(ctx, sess))

	body := map[string]string{"user_id": invitee.ID.String()}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID.String()+"/invite", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.URL.Path = "/api/sessions/" + session.ID.String() + "/invite"
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: sess.ID.String()})
	w := httptest.NewRecorder()

	h.RequireAuth(h.SessionInvite)(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}
