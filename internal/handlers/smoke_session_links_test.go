package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/psuthar/talkback/internal/auth"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSmoke_SessionLinks_AddListAndDelete covers the full session-link lifecycle:
// creator adds a link → list returns it → delete removes it.
func TestSmoke_SessionLinks_AddListAndDelete(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	_, ls, sess := seedCreatorAndSession(t, h, "links-lifecycle")

	// --- Step 1: Add a link ---
	body, _ := json.Marshal(map[string]any{"url": "https://example.com/report"})
	addReq := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sess.ID.String()+"/links", bytes.NewReader(body))
	addReq.Header.Set("Content-Type", "application/json")
	addReq.URL.Path = "/api/sessions/" + sess.ID.String() + "/links"
	addReq.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	addW := httptest.NewRecorder()
	h.RequireAuth(h.AddSessionLink)(addW, addReq)

	require.Equal(t, http.StatusCreated, addW.Code, addW.Body.String())
	var link models.SessionLink
	require.NoError(t, json.NewDecoder(addW.Body).Decode(&link))
	assert.NotEmpty(t, link.ID)
	assert.Equal(t, sess.ID, link.SessionID)
	assert.Equal(t, models.SessionLinkStatusPending, link.Status)

	// --- Step 2: List confirms link is present ---
	listReq := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sess.ID.String()+"/links", nil)
	listReq.URL.Path = "/api/sessions/" + sess.ID.String() + "/links"
	listW := httptest.NewRecorder()
	h.ListSessionLinks(listW, listReq)

	require.Equal(t, http.StatusOK, listW.Code, listW.Body.String())
	var links []*models.SessionLink
	require.NoError(t, json.NewDecoder(listW.Body).Decode(&links))
	require.Len(t, links, 1)
	assert.Equal(t, link.ID, links[0].ID)

	// --- Step 3: Delete the link ---
	delReq := httptest.NewRequest(http.MethodDelete, "/api/sessions/"+sess.ID.String()+"/links/"+link.ID.String(), nil)
	delReq.URL.Path = "/api/sessions/" + sess.ID.String() + "/links/" + link.ID.String()
	delReq.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	delW := httptest.NewRecorder()
	h.RequireAuth(h.DeleteSessionLink)(delW, delReq)

	require.Equal(t, http.StatusNoContent, delW.Code, delW.Body.String())

	// --- Step 4: List confirms link is gone ---
	listReq2 := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sess.ID.String()+"/links", nil)
	listReq2.URL.Path = "/api/sessions/" + sess.ID.String() + "/links"
	listW2 := httptest.NewRecorder()
	h.ListSessionLinks(listW2, listReq2)

	require.Equal(t, http.StatusOK, listW2.Code)
	var links2 []*models.SessionLink
	require.NoError(t, json.NewDecoder(listW2.Body).Decode(&links2))
	assert.Len(t, links2, 0)
}

// TestSmoke_SessionLinks_ListEmptyForNewSession verifies list returns empty array when no links exist.
func TestSmoke_SessionLinks_ListEmptyForNewSession(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	_, _, sess := seedCreatorAndSession(t, h, "links-empty")

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sess.ID.String()+"/links", nil)
	req.URL.Path = "/api/sessions/" + sess.ID.String() + "/links"
	w := httptest.NewRecorder()
	h.ListSessionLinks(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var links []*models.SessionLink
	require.NoError(t, json.NewDecoder(w.Body).Decode(&links))
	assert.Len(t, links, 0)
}

// TestSmoke_SessionLinks_InvalidURLRejected verifies that a malformed URL returns 400.
func TestSmoke_SessionLinks_InvalidURLRejected(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	_, ls, sess := seedCreatorAndSession(t, h, "links-bad-url")

	body, _ := json.Marshal(map[string]any{"url": "not a valid url!!"})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sess.ID.String()+"/links", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.URL.Path = "/api/sessions/" + sess.ID.String() + "/links"
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	w := httptest.NewRecorder()
	h.RequireAuth(h.AddSessionLink)(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestSmoke_SessionLinks_NonCreatorForbiddenToAdd verifies that a user who is not the
// session creator cannot add a link — returns 403.
func TestSmoke_SessionLinks_NonCreatorForbiddenToAdd(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	_, _, sess := seedCreatorAndSession(t, h, "links-forbidden")

	body, _ := json.Marshal(map[string]any{"url": "https://example.com/leak"})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sess.ID.String()+"/links", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.URL.Path = "/api/sessions/" + sess.ID.String() + "/links"
	// Different creator — not the session owner.
	addUserSessionCookie(t, h, req, "intruder+links@smoke.test")
	w := httptest.NewRecorder()
	h.RequireAuth(h.AddSessionLink)(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

// TestSmoke_SessionLinks_NonCreatorForbiddenToDelete verifies that a non-owner cannot
// delete a link — returns 403.
func TestSmoke_SessionLinks_NonCreatorForbiddenToDelete(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	_, ls, sess := seedCreatorAndSession(t, h, "links-del-forbidden")

	// Creator adds a link.
	addBody, _ := json.Marshal(map[string]any{"url": "https://example.com/del-test"})
	addReq := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sess.ID.String()+"/links", bytes.NewReader(addBody))
	addReq.Header.Set("Content-Type", "application/json")
	addReq.URL.Path = "/api/sessions/" + sess.ID.String() + "/links"
	addReq.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	addW := httptest.NewRecorder()
	h.RequireAuth(h.AddSessionLink)(addW, addReq)
	require.Equal(t, http.StatusCreated, addW.Code, addW.Body.String())
	var link models.SessionLink
	require.NoError(t, json.NewDecoder(addW.Body).Decode(&link))

	// Intruder tries to delete.
	delReq := httptest.NewRequest(http.MethodDelete, "/api/sessions/"+sess.ID.String()+"/links/"+link.ID.String(), nil)
	delReq.URL.Path = "/api/sessions/" + sess.ID.String() + "/links/" + link.ID.String()
	addUserSessionCookie(t, h, delReq, "intruder+linksdel@smoke.test")
	delW := httptest.NewRecorder()
	h.RequireAuth(h.DeleteSessionLink)(delW, delReq)

	assert.Equal(t, http.StatusForbidden, delW.Code)
}
