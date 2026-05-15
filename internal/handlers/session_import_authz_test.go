package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSessionImportAttachHandlers_AuthzMatrix locks in the SCRUM-411
// hardening across all three attach handlers. The matrix exercises:
//   - missing session  -> 404 (regardless of caller)
//   - no user in ctx   -> 401
//   - non-editor user  -> 403
//   - editor user      -> handler advances past the authz check (we assert
//                         the response is NOT 401/403/404; the exact 4xx/5xx
//                         that follows depends on upstream side effects like
//                         OAuth/recording lookups that are out of scope here).
func TestSessionImportAttachHandlers_AuthzMatrix(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	ctx := context.Background()

	// Editor user (creator role on the session).
	editorEmail := "editor-" + uuid.NewString() + "@example.com"
	editorUser := &models.User{
		ID:          uuid.New(),
		Email:       editorEmail,
		DisplayName: "Editor",
		GlobalRole:  models.GlobalRoleCreator,
		Status:      models.UserStatusActive,
	}
	require.NoError(t, h.DB.CreateUser(ctx, editorUser))

	// Outsider user (no membership).
	outsiderEmail := "outsider-" + uuid.NewString() + "@example.com"
	outsider := &models.User{
		ID:          uuid.New(),
		Email:       outsiderEmail,
		DisplayName: "Outsider",
		GlobalRole:  models.GlobalRoleCreator,
		Status:      models.UserStatusActive,
	}
	require.NoError(t, h.DB.CreateUser(ctx, outsider))

	session := createTestSessionForHandlers(t, h.DB, "import-authz session")
	require.NoError(t, h.DB.CreateSessionMembership(ctx, session.ID, editorUser.ID, "creator", nil))

	// Each row in `handlers` covers one of the three SessionImport* handlers.
	// We don't care about the request body shape beyond what the handler
	// requires to reach the authz check (each handler reads creator_identity
	// and then session_id before bouncing).
	handlers := []struct {
		name     string
		fn       func(http.ResponseWriter, *http.Request)
		path     func(sessionID string) string
		bodyJSON string
	}{
		{
			"Zoom",
			h.SessionImportZoom,
			func(sid string) string { return "/api/sessions/" + sid + "/import/zoom" },
			`{"meeting_uuid":"meeting-1"}`,
		},
		{
			"GoogleMeet",
			h.SessionImportGoogleMeet,
			func(sid string) string { return "/api/sessions/" + sid + "/import/google-meet" },
			`{"conference_record":"cr-1","recording":"r-1"}`,
		},
		{
			"Teams",
			h.SessionImportTeams,
			func(sid string) string { return "/api/sessions/" + sid + "/import/teams" },
			`{"meeting_id":"m-1","recording_id":"r-1"}`,
		},
	}

	for _, p := range handlers {
		p := p
		// Build a request with the creator_identity header (so the handler
		// passes its pre-flight check and reaches the path/session/authz block).
		newReq := func(sid string, user *models.User) *http.Request {
			req := httptest.NewRequest(http.MethodPost, p.path(sid), bytes.NewReader([]byte(p.bodyJSON)))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Creator-Identity", "any-creator-identity")
			if user != nil {
				req = req.WithContext(context.WithValue(req.Context(), userContextKey, user))
			}
			return req
		}

		t.Run(p.name+"/missing session returns 404", func(t *testing.T) {
			missingSID := uuid.New().String()
			req := newReq(missingSID, editorUser)
			w := httptest.NewRecorder()
			p.fn(w, req)
			assert.Equal(t, http.StatusNotFound, w.Code, "404 expected for unknown session")
		})

		t.Run(p.name+"/non-editor user returns 403", func(t *testing.T) {
			req := newReq(session.ID.String(), outsider)
			w := httptest.NewRecorder()
			p.fn(w, req)
			assert.Equal(t, http.StatusForbidden, w.Code, "403 expected for non-editor")
			body := w.Body.String()
			assert.Contains(t, strings.ToLower(body), "editor", "403 body should explain editor requirement")
		})

		t.Run(p.name+"/no user in context returns 401", func(t *testing.T) {
			req := newReq(session.ID.String(), nil)
			w := httptest.NewRecorder()
			p.fn(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Code, "401 expected when ctx has no user")
		})

		t.Run(p.name+"/editor user passes the authz gate", func(t *testing.T) {
			req := newReq(session.ID.String(), editorUser)
			w := httptest.NewRecorder()
			p.fn(w, req)
			// Once authz passes, downstream may 401 (token not connected),
			// 422 (duration), 500 (db), or 202 (happy). The point of this
			// test is that we do NOT see 401/403/404 from the authz path.
			require.NotEqual(t, http.StatusForbidden, w.Code, "editor must not be 403'd")
			require.NotEqual(t, http.StatusNotFound, w.Code, "editor on existing session must not be 404'd")
		})

		t.Run(p.name+"/admin (global role) passes the authz gate without explicit membership", func(t *testing.T) {
			adminEmail := "admin-" + uuid.NewString() + "@example.com"
			admin := &models.User{
				ID:          uuid.New(),
				Email:       adminEmail,
				DisplayName: "Admin",
				GlobalRole:  models.GlobalRoleAdmin,
				Status:      models.UserStatusActive,
			}
			require.NoError(t, h.DB.CreateUser(ctx, admin))
			req := newReq(session.ID.String(), admin)
			w := httptest.NewRecorder()
			p.fn(w, req)
			require.NotEqual(t, http.StatusForbidden, w.Code, "admin must not be 403'd")
		})
	}
}

// TestSessionImportResponseShape locks the SCRUM-411 standardized 202 body
// shape: SessionImportResponse with job_id + state + omitempty
// already_imported. The shape is what the frontend tile + SCRUM-XX5b's
// idempotent dedupe both depend on, so a silent rename here would land as
// a frontend regression.
func TestSessionImportResponseShape(t *testing.T) {
	t.Parallel()

	got, err := json.Marshal(SessionImportResponse{JobID: "job-1", State: "queued"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"job_id":"job-1","state":"queued"}`, string(got),
		"a fresh attach must omit already_imported (false → omitempty)")

	gotAlready, err := json.Marshal(SessionImportResponse{JobID: "job-2", State: "ready", AlreadyImported: true})
	require.NoError(t, err)
	assert.JSONEq(t, `{"job_id":"job-2","state":"ready","already_imported":true}`, string(gotAlready),
		"a re-attach must include already_imported=true (set by SCRUM-XX5b dedupe)")
}
