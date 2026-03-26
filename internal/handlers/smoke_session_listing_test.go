package handlers

import (
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

// TestSmoke_SessionListing_Pagination verifies that GET /api/sessions honours
// limit and offset query params. The test seeds 3 sessions for a single creator
// and asserts that page 1 (limit=2&offset=0) returns 2 items and page 2
// (limit=2&offset=2) returns the remaining 1 item.
func TestSmoke_SessionListing_Pagination(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	ctx := context.Background()
	creatorEmail := "pagination-creator@smoke.test"

	// Create the creator user once to avoid unique-email collisions on repeat calls.
	creator := &models.User{
		ID:          uuid.New(),
		Email:       creatorEmail,
		DisplayName: "Pagination Creator",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleCreator,
	}
	require.NoError(t, h.DB.CreateUser(ctx, creator))

	loginSession := &models.LoginSession{
		ID:        uuid.New(),
		UserID:    creator.ID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	require.NoError(t, h.DB.CreateLoginSession(ctx, loginSession))
	cookieValue := loginSession.ID.String()

	// Seed 3 sessions owned by the creator directly via DB.
	for i := 0; i < 3; i++ {
		email := creatorEmail
		sess := &models.Session{
			ID:        uuid.New(),
			Title:     "Pagination Session " + string(rune('A'+i)),
			CreatedBy: &email,
			Status:    models.SessionStatusOpen,
		}
		require.NoError(t, h.DB.CreateSession(ctx, sess))
	}

	// makeReq issues an authenticated GET /api/sessions with the given query string
	// and decodes the response body into a []SessionWithRole slice.
	makeReq := func(query string) []SessionWithRole {
		url := "/api/sessions"
		if query != "" {
			url += "?" + query
		}
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: cookieValue})
		w := httptest.NewRecorder()
		h.RequireAuth(h.ListSessions)(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var result []SessionWithRole
		require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
		return result
	}

	// Page 1: limit=2&offset=0 — expect exactly 2 sessions.
	page1 := makeReq("limit=2&offset=0")
	assert.Len(t, page1, 2, "page 1 should return 2 sessions")

	// Page 2: limit=2&offset=2 — expect exactly 1 remaining session.
	page2 := makeReq("limit=2&offset=2")
	assert.Len(t, page2, 1, "page 2 should return the 1 remaining session")

	// No overlap: page1 and page2 session IDs must be disjoint.
	seen := make(map[string]bool)
	for _, s := range page1 {
		seen[s.Session.ID.String()] = true
	}
	for _, s := range page2 {
		assert.False(t, seen[s.Session.ID.String()],
			"session %s appeared on both pages", s.Session.ID)
	}

	// Without pagination the full list contains all 3 sessions.
	all := makeReq("")
	assert.Len(t, all, 3, "unpaginated list should return all 3 sessions")
}
