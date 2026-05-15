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

// TestLegacyImportSunsetHeader pins env-override semantics.
func TestLegacyImportSunsetHeader(t *testing.T) {
	t.Run("unset returns the compiled default", func(t *testing.T) {
		t.Setenv("LEGACY_IMPORT_SUNSET_DATE", "")
		assert.Equal(t, defaultLegacyImportSunsetDate, legacyImportSunsetHeader())
	})

	t.Run("env override is returned verbatim", func(t *testing.T) {
		t.Setenv("LEGACY_IMPORT_SUNSET_DATE", "Mon, 01 Jun 2026 00:00:00 GMT")
		assert.Equal(t, "Mon, 01 Jun 2026 00:00:00 GMT", legacyImportSunsetHeader())
	})
}

// TestLegacyImportDeprecationHeaders_AllThree verifies that each legacy
// create-new endpoint stamps Deprecation / Sunset / Link headers on every
// response (including error paths via method-not-allowed).
func TestLegacyImportDeprecationHeaders_AllThree(t *testing.T) {
	t.Setenv("ENABLE_GOOGLE_MEET", "true")
	t.Setenv("ENABLE_TEAMS", "true")
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	cases := []struct {
		name      string
		path      string
		fn        func(http.ResponseWriter, *http.Request)
		altPath   string
	}{
		{"ZoomImport", "/api/zoom/import", h.ZoomImport, "/api/sessions/:id/import/zoom"},
		{"GoogleMeetImport", "/api/google-meet/import", h.GoogleMeetImport, "/api/sessions/:id/import/google-meet"},
		{"TeamsImport", "/api/teams/import", h.TeamsImport, "/api/sessions/:id/import/teams"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name+"/POST with bad body still carries deprecation headers", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewReader([]byte(`{`)))
			req.Header.Set("Content-Type", "application/json")
			user := &models.User{
				ID:         uuid.New(),
				Email:      "u-" + uuid.NewString() + "@example.com",
				GlobalRole: models.GlobalRoleCreator,
				Status:     models.UserStatusActive,
			}
			require.NoError(t, h.DB.CreateUser(context.Background(), user))
			req = req.WithContext(context.WithValue(req.Context(), userContextKey, user))
			w := httptest.NewRecorder()
			tc.fn(w, req)

			assert.Equal(t, "true", w.Header().Get("Deprecation"))
			assert.NotEmpty(t, w.Header().Get("Sunset"))
			link := w.Header().Get("Link")
			assert.True(t, strings.Contains(link, tc.altPath), "Link header must reference the attach endpoint, got %q", link)
			assert.Contains(t, link, `rel="alternate"`)
		})

		t.Run(tc.name+"/wrong method (GET) still carries headers", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			w := httptest.NewRecorder()
			tc.fn(w, req)
			assert.Equal(t, "true", w.Header().Get("Deprecation"))
			assert.NotEmpty(t, w.Header().Get("Sunset"))
		})
	}
}
