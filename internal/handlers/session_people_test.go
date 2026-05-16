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

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSessionPeopleAPI exercises the SCRUM-424 endpoints end-to-end:
// GET aggregates labels + aliases; POST upserts (merge / rename); DELETE
// unmaps. Authz fires on POST and DELETE.
func TestSessionPeopleAPI(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	ctx := context.Background()

	editor := &models.User{
		ID:          uuid.New(),
		Email:       "editor-" + uuid.NewString() + "@example.com",
		DisplayName: "Editor",
		GlobalRole:  models.GlobalRoleCreator,
		Status:      models.UserStatusActive,
	}
	require.NoError(t, h.DB.CreateUser(ctx, editor))

	outsider := &models.User{
		ID:          uuid.New(),
		Email:       "outsider-" + uuid.NewString() + "@example.com",
		DisplayName: "Outsider",
		GlobalRole:  models.GlobalRoleCreator,
		Status:      models.UserStatusActive,
	}
	require.NoError(t, h.DB.CreateUser(ctx, outsider))

	session := createTestSessionForHandlers(t, h.DB, "people-api session")
	require.NoError(t, h.DB.CreateSessionMembership(ctx, session.ID, editor.ID, "creator", nil))

	// Seed transcript_segments so ListDistinctSpeakerLabels has rows.
	transcriptID := uuid.New()
	_, err := h.DB.Pool.Exec(ctx, `
		INSERT INTO transcripts (id, session_id, source, status)
		VALUES ($1, $2, 'zoom', 'ready')
	`, transcriptID, session.ID)
	require.NoError(t, err)
	for idx, label := range []string{"Speaker 0", "Speaker 1", "Speaker 0"} {
		_, err := h.DB.Pool.Exec(ctx, `
			INSERT INTO transcript_segments (transcript_id, session_id, idx, start_ms, end_ms, text, speaker_label)
			VALUES ($1, $2, $3, $4, $5, 'hello', $6)
		`, transcriptID, session.ID, idx+1, (idx+1)*1000, (idx+1)*1000+500, label)
		require.NoError(t, err)
	}

	withCtx := func(req *http.Request, user *models.User) *http.Request {
		if user == nil {
			return req
		}
		return req.WithContext(context.WithValue(req.Context(), userContextKey, user))
	}

	t.Run("GET returns labels + (empty) aliases", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+session.ID.String()+"/people", nil)
		req = withCtx(req, editor)
		w := httptest.NewRecorder()
		h.SessionPeopleRouter(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		var resp SessionPeopleResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.Len(t, resp.Labels, 2)
		// Speaker 0 has 2 segments, Speaker 1 has 1.
		got := map[string]int{}
		for _, l := range resp.Labels {
			got[l.SourceLabel] = l.SegmentCount
		}
		assert.Equal(t, 2, got["Speaker 0"])
		assert.Equal(t, 1, got["Speaker 1"])
		assert.Empty(t, resp.Aliases)
	})

	canonical := uuid.New()

	t.Run("POST upsert from editor creates an alias", func(t *testing.T) {
		body, _ := json.Marshal(UpsertAliasRequest{
			SourceLabel:          "Speaker 0",
			CanonicalPersonID:    &canonical,
			CanonicalDisplayName: "Alice",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID.String()+"/people/aliases", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = withCtx(req, editor)
		w := httptest.NewRecorder()
		h.SessionPeopleRouter(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		var resp map[string]string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, canonical.String(), resp["canonical_person_id"])
	})

	t.Run("GET after upsert returns the alias", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+session.ID.String()+"/people", nil)
		req = withCtx(req, editor)
		w := httptest.NewRecorder()
		h.SessionPeopleRouter(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		var resp SessionPeopleResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.Len(t, resp.Aliases, 1)
		assert.Equal(t, "Speaker 0", resp.Aliases[0].SourceLabel)
		assert.Equal(t, canonical, resp.Aliases[0].CanonicalPersonID)
		assert.Equal(t, "Alice", resp.Aliases[0].CanonicalDisplayName)
	})

	t.Run("POST merge: map Speaker 1 to the same canonical person", func(t *testing.T) {
		body, _ := json.Marshal(UpsertAliasRequest{
			SourceLabel:          "Speaker 1",
			CanonicalPersonID:    &canonical,
			CanonicalDisplayName: "Alice",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID.String()+"/people/aliases", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = withCtx(req, editor)
		w := httptest.NewRecorder()
		h.SessionPeopleRouter(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		aliases, err := h.DB.ListAliasesBySession(ctx, session.ID)
		require.NoError(t, err)
		require.Len(t, aliases, 2)
		for _, a := range aliases {
			assert.Equal(t, canonical, a.CanonicalPersonID, "both labels merged to same canonical")
		}
	})

	t.Run("POST rejected for non-editor", func(t *testing.T) {
		body, _ := json.Marshal(UpsertAliasRequest{SourceLabel: "Speaker 2", CanonicalDisplayName: "Bob"})
		req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID.String()+"/people/aliases", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = withCtx(req, outsider)
		w := httptest.NewRecorder()
		h.SessionPeopleRouter(w, req)
		assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	})

	t.Run("POST rejected with 401 when no user in ctx", func(t *testing.T) {
		body, _ := json.Marshal(UpsertAliasRequest{SourceLabel: "Speaker 2", CanonicalDisplayName: "Bob"})
		req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID.String()+"/people/aliases", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.SessionPeopleRouter(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("POST with no canonical_person_id mints a new one", func(t *testing.T) {
		body, _ := json.Marshal(UpsertAliasRequest{SourceLabel: "Brand New", CanonicalDisplayName: "Carol"})
		req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID.String()+"/people/aliases", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = withCtx(req, editor)
		w := httptest.NewRecorder()
		h.SessionPeopleRouter(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var resp map[string]string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		newCanonical, err := uuid.Parse(resp["canonical_person_id"])
		require.NoError(t, err)
		assert.NotEqual(t, canonical, newCanonical, "must be a fresh canonical")
	})

	t.Run("DELETE alias unmaps", func(t *testing.T) {
		aliases, err := h.DB.ListAliasesBySession(ctx, session.ID)
		require.NoError(t, err)
		require.NotEmpty(t, aliases)
		target := aliases[0]
		req := httptest.NewRequest(http.MethodDelete, "/api/sessions/"+session.ID.String()+"/people/aliases/"+target.ID.String(), nil)
		req = withCtx(req, editor)
		w := httptest.NewRecorder()
		h.SessionPeopleRouter(w, req)
		require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

		// And the row is gone.
		aliasesAfter, err := h.DB.ListAliasesBySession(ctx, session.ID)
		require.NoError(t, err)
		for _, a := range aliasesAfter {
			assert.NotEqual(t, target.ID, a.ID)
		}
	})

	t.Run("DELETE rejected for non-editor", func(t *testing.T) {
		aliases, err := h.DB.ListAliasesBySession(ctx, session.ID)
		require.NoError(t, err)
		require.NotEmpty(t, aliases)
		req := httptest.NewRequest(http.MethodDelete, "/api/sessions/"+session.ID.String()+"/people/aliases/"+aliases[0].ID.String(), nil)
		req = withCtx(req, outsider)
		w := httptest.NewRecorder()
		h.SessionPeopleRouter(w, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("DELETE cross-session alias returns 404", func(t *testing.T) {
		other := createTestSessionForHandlers(t, h.DB, "people-api other session")
		require.NoError(t, h.DB.CreateSessionMembership(ctx, other.ID, editor.ID, "creator", nil))
		require.NoError(t, h.DB.UpsertAlias(ctx, other.ID, "X", nil, uuid.New(), "Other-X", nil))
		othersAliases, err := h.DB.ListAliasesBySession(ctx, other.ID)
		require.NoError(t, err)
		require.NotEmpty(t, othersAliases)
		// Try to DELETE that alias against our test session — must 404.
		req := httptest.NewRequest(http.MethodDelete, "/api/sessions/"+session.ID.String()+"/people/aliases/"+othersAliases[0].ID.String(), nil)
		req = withCtx(req, editor)
		w := httptest.NewRecorder()
		h.SessionPeopleRouter(w, req)
		require.Equal(t, http.StatusNotFound, w.Code)
		// And the alias is intact on the other session.
		var n int
		require.NoError(t, h.DB.Pool.QueryRow(ctx, `SELECT count(*) FROM session_speaker_aliases WHERE session_id = $1`, other.ID).Scan(&n))
		assert.Equal(t, 1, n)
	})

	t.Run("GET on missing session returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+uuid.New().String()+"/people", nil)
		req = withCtx(req, editor)
		w := httptest.NewRecorder()
		h.SessionPeopleRouter(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("Body validation: missing source_label rejected", func(t *testing.T) {
		body, _ := json.Marshal(UpsertAliasRequest{CanonicalDisplayName: "no-label"})
		req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID.String()+"/people/aliases", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = withCtx(req, editor)
		w := httptest.NewRecorder()
		h.SessionPeopleRouter(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, strings.ToLower(w.Body.String()), "source_label")
	})

	t.Run("Body validation: missing canonical_display_name rejected", func(t *testing.T) {
		body, _ := json.Marshal(UpsertAliasRequest{SourceLabel: "Speaker 0"})
		req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID.String()+"/people/aliases", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = withCtx(req, editor)
		w := httptest.NewRecorder()
		h.SessionPeopleRouter(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	// Make sure the test name suffix is unique across re-runs.
	_ = fmt.Sprintf
}
