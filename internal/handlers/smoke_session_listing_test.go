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

// TestSmoke_SessionListing_MemberCountAndCreatorDisplayName verifies the wire
// shape added by SCRUM-221: each row carries `member_count` and
// `created_by_display_name`. Also exercises the missing-display-name path
// (sessions whose `created_by` email has no matching users row).
func TestSmoke_SessionListing_MemberCountAndCreatorDisplayName(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	ctx := context.Background()
	creatorEmail := "shape-creator-" + uuid.New().String() + "@smoke.test"
	creator := &models.User{
		ID:          uuid.New(),
		Email:       creatorEmail,
		DisplayName: "Shape Creator",
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

	// Owned session (display_name should resolve) with two extra members.
	owned := &models.Session{
		ID:        uuid.New(),
		Title:     "Owned with members",
		CreatedBy: &creatorEmail,
		Status:    models.SessionStatusOpen,
	}
	require.NoError(t, h.DB.CreateSession(ctx, owned))
	for i := 0; i < 2; i++ {
		memberEmail := "member-" + uuid.New().String() + "@smoke.test"
		m := &models.User{ID: uuid.New(), Email: memberEmail, DisplayName: "M", Status: models.UserStatusActive, GlobalRole: models.GlobalRoleCreator}
		require.NoError(t, h.DB.CreateUser(ctx, m))
		require.NoError(t, h.DB.CreateSessionMembership(ctx, owned.ID, m.ID, "participant", nil))
	}

	// Decode response.
	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: cookieValue})
	w := httptest.NewRecorder()
	h.RequireAuth(h.ListSessions)(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Parse as a generic []map so we lock the JSON keys (member_count,
	// created_by_display_name) regardless of the Go struct field names.
	var rows []map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&rows))
	require.Len(t, rows, 1)
	row := rows[0]
	mc, ok := row["member_count"]
	require.True(t, ok, "response row must include member_count")
	// SCRUM-226: db.CreateSession also inserts the creator's session_memberships
	// row, so the owned session counts 2 invited members + 1 creator = 3.
	assert.Equal(t, float64(3), mc, "owned session should report 2 invited + 1 creator = 3 members")
	dn, ok := row["created_by_display_name"]
	require.True(t, ok, "response row must include created_by_display_name (or null)")
	assert.Equal(t, "Shape Creator", dn)

	// Missing-display-name path: bring in a session whose created_by has no
	// matching users row, and view it as a global admin.
	orphanEmail := "orphan-" + uuid.New().String() + "@smoke.test"
	orphan := &models.Session{
		ID:        uuid.New(),
		Title:     "Orphan creator",
		CreatedBy: &orphanEmail,
		Status:    models.SessionStatusOpen,
	}
	require.NoError(t, h.DB.CreateSession(ctx, orphan))

	adminEmail := "admin-" + uuid.New().String() + "@smoke.test"
	admin := &models.User{ID: uuid.New(), Email: adminEmail, DisplayName: "A", Status: models.UserStatusActive, GlobalRole: models.GlobalRoleAdmin}
	require.NoError(t, h.DB.CreateUser(ctx, admin))
	adminSession := &models.LoginSession{ID: uuid.New(), UserID: admin.ID, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(24 * time.Hour)}
	require.NoError(t, h.DB.CreateLoginSession(ctx, adminSession))

	req2 := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req2.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: adminSession.ID.String()})
	w2 := httptest.NewRecorder()
	h.RequireAuth(h.ListSessions)(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code, w2.Body.String())
	var adminRows []map[string]any
	require.NoError(t, json.NewDecoder(w2.Body).Decode(&adminRows))
	var orphanFound bool
	for _, r := range adminRows {
		sess, _ := r["session"].(map[string]any)
		if sess == nil {
			continue
		}
		if cb, _ := sess["created_by"].(string); cb == orphanEmail {
			orphanFound = true
			// created_by_display_name is omitempty -> absent when nil.
			_, hasDN := r["created_by_display_name"]
			assert.False(t, hasDN, "orphan creator should have no display_name in response")
			assert.Equal(t, float64(0), r["member_count"], "orphan session has zero memberships")
		}
	}
	require.True(t, orphanFound, "admin should see orphan-created session")
}
