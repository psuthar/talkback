package handlers

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSessionImportAttachHandlers_CrossTenantIdentity locks in the
// SCRUM-417 fix: the X-Creator-Identity request header must match the
// authenticated user's email. Without this check, an editor of session X
// could pass user B's identity and trigger the worker to fetch the
// recording using B's OAuth token, injecting B's content into X.
func TestSessionImportAttachHandlers_CrossTenantIdentity(t *testing.T) {
	t.Setenv("ENABLE_GOOGLE_MEET", "true")
	t.Setenv("ENABLE_TEAMS", "true")
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	ctx := context.Background()

	editor := &models.User{
		ID:          uuid.New(),
		Email:       "alice-" + uuid.NewString() + "@example.com",
		DisplayName: "Alice",
		GlobalRole:  models.GlobalRoleCreator,
		Status:      models.UserStatusActive,
	}
	require.NoError(t, h.DB.CreateUser(ctx, editor))

	victim := &models.User{
		ID:          uuid.New(),
		Email:       "bob-" + uuid.NewString() + "@example.com",
		DisplayName: "Bob",
		GlobalRole:  models.GlobalRoleCreator,
		Status:      models.UserStatusActive,
	}
	require.NoError(t, h.DB.CreateUser(ctx, victim))

	session := createTestSessionForHandlers(t, h.DB, "xtenant identity session")
	require.NoError(t, h.DB.CreateSessionMembership(ctx, session.ID, editor.ID, "creator", nil))

	handlers := []struct {
		name     string
		fn       func(http.ResponseWriter, *http.Request)
		path     func(sid string) string
		bodyJSON string
	}{
		{"Zoom", h.SessionImportZoom, func(sid string) string { return "/api/sessions/" + sid + "/import/zoom" }, `{"meeting_uuid":"meeting-1"}`},
		{"GoogleMeet", h.SessionImportGoogleMeet, func(sid string) string { return "/api/sessions/" + sid + "/import/google-meet" }, `{"conference_record":"cr-1","recording":"r-1"}`},
		{"Teams", h.SessionImportTeams, func(sid string) string { return "/api/sessions/" + sid + "/import/teams" }, `{"meeting_id":"m-1","recording_id":"r-1"}`},
	}

	newReq := func(p string, identity string, user *models.User, body string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, p, bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Creator-Identity", identity)
		req = req.WithContext(context.WithValue(req.Context(), userContextKey, user))
		return req
	}

	for _, p := range handlers {
		p := p

		t.Run(p.name+"/editor passing their OWN email is accepted past the cross-tenant gate", func(t *testing.T) {
			req := newReq(p.path(session.ID.String()), editor.Email, editor, p.bodyJSON)
			w := httptest.NewRecorder()
			p.fn(w, req)
			// We do NOT assert 202 because downstream may 401 (no OAuth token
			// in test env) or other; the point is that the cross-tenant check
			// does not 403 with the matching identity.
			require.NotEqual(t, http.StatusForbidden, w.Code, "matching identity must not be 403'd: body=%s", w.Body.String())
		})

		t.Run(p.name+"/editor passing the victim's email returns 403", func(t *testing.T) {
			req := newReq(p.path(session.ID.String()), victim.Email, editor, p.bodyJSON)
			w := httptest.NewRecorder()
			p.fn(w, req)
			require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			assert.Contains(t, strings.ToLower(w.Body.String()), "creator_identity")
		})

		t.Run(p.name+"/editor passing a different-case email is accepted (case-insensitive)", func(t *testing.T) {
			req := newReq(p.path(session.ID.String()), strings.ToUpper(editor.Email), editor, p.bodyJSON)
			w := httptest.NewRecorder()
			p.fn(w, req)
			require.NotEqual(t, http.StatusForbidden, w.Code, "case-insensitive identity match: body=%s", w.Body.String())
		})

		t.Run(p.name+"/editor passing identity with whitespace is accepted (trimmed)", func(t *testing.T) {
			req := newReq(p.path(session.ID.String()), "  "+editor.Email+"  ", editor, p.bodyJSON)
			w := httptest.NewRecorder()
			p.fn(w, req)
			require.NotEqual(t, http.StatusForbidden, w.Code, "trimmed identity match: body=%s", w.Body.String())
		})
	}
}
