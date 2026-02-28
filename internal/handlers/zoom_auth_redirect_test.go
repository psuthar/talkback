package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestZoomAuthStart_RedirectURI(t *testing.T) {
	databaseURL, cleanupDB := test.SetupTestDB(t)
	defer cleanupDB()
	origDB := os.Getenv("DATABASE_URL")
	os.Setenv("DATABASE_URL", databaseURL)
	defer func() {
		if origDB != "" {
			os.Setenv("DATABASE_URL", origDB)
		} else {
			os.Unsetenv("DATABASE_URL")
		}
	}()

	db, err := database.New()
	require.NoError(t, err)
	defer db.Close()
	h := NewHandlers(db, nil, nil)

	saveEnv := func(key string) (restore func()) {
		v := os.Getenv(key)
		return func() {
			if v != "" {
				os.Setenv(key, v)
			} else {
				os.Unsetenv(key)
			}
		}
	}

	t.Run("relative ZOOM_REDIRECT_URL returns 500 and error message", func(t *testing.T) {
		defer saveEnv("ZOOM_REDIRECT_URL")()
		defer saveEnv("ZOOM_CLIENT_ID")()
		os.Setenv("ZOOM_REDIRECT_URL", "/auth/zoom/callback")
		os.Setenv("ZOOM_CLIENT_ID", "test-client-id")

		req := httptest.NewRequest(http.MethodGet, "/auth/zoom/start", nil)
		w := httptest.NewRecorder()
		h.ZoomAuthStart(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		body := w.Body.String()
		assert.Contains(t, body, "absolute URL")
		assert.Contains(t, body, "ZOOM_REDIRECT_URL")
	})

	t.Run("absolute ZOOM_REDIRECT_URL redirects with encoded redirect_uri in Location", func(t *testing.T) {
		defer saveEnv("ZOOM_REDIRECT_URL")()
		defer saveEnv("ZOOM_CLIENT_ID")()
		defer saveEnv("ENV")()
		os.Setenv("ZOOM_REDIRECT_URL", "https://talkback-895n.onrender.com/auth/zoom/callback")
		os.Setenv("ZOOM_CLIENT_ID", "test-client-id")
		os.Unsetenv("ENV")

		req := httptest.NewRequest(http.MethodGet, "/auth/zoom/start", nil)
		w := httptest.NewRecorder()
		h.ZoomAuthStart(w, req)

		assert.Equal(t, http.StatusFound, w.Code)
		loc := w.Header().Get("Location")
		require.NotEmpty(t, loc)
		// redirect_uri must be query-encoded absolute URL
		assert.True(t, strings.Contains(loc, "redirect_uri="), "Location should contain redirect_uri param")
		assert.True(t, strings.Contains(loc, "https%3A%2F%2Ftalkback-895n.onrender.com%2Fauth%2Fzoom%2Fcallback") || strings.Contains(loc, "https://talkback-895n.onrender.com/auth/zoom/callback"), "Location should contain absolute redirect URI")
	})

	t.Run("empty ZOOM_REDIRECT_URL with ENV=production returns 500", func(t *testing.T) {
		defer saveEnv("ZOOM_REDIRECT_URL")()
		defer saveEnv("ZOOM_CLIENT_ID")()
		defer saveEnv("ENV")()
		os.Unsetenv("ZOOM_REDIRECT_URL")
		os.Setenv("ZOOM_CLIENT_ID", "test-client-id")
		os.Setenv("ENV", "production")

		req := httptest.NewRequest(http.MethodGet, "/auth/zoom/start", nil)
		w := httptest.NewRecorder()
		h.ZoomAuthStart(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "ZOOM_REDIRECT_URL must be set")
	})

	t.Run("empty ZOOM_REDIRECT_URL and non-production uses local fallback", func(t *testing.T) {
		defer saveEnv("ZOOM_REDIRECT_URL")()
		defer saveEnv("ZOOM_CLIENT_ID")()
		defer saveEnv("ENV")()
		os.Unsetenv("ZOOM_REDIRECT_URL")
		os.Setenv("ZOOM_CLIENT_ID", "test-client-id")
		os.Unsetenv("ENV")

		req := httptest.NewRequest(http.MethodGet, "/auth/zoom/start", nil)
		w := httptest.NewRecorder()
		h.ZoomAuthStart(w, req)

		assert.Equal(t, http.StatusFound, w.Code)
		loc := w.Header().Get("Location")
		require.NotEmpty(t, loc)
		assert.True(t, strings.Contains(loc, "localhost%3A8081") || strings.Contains(loc, "localhost:8081"), "should use local fallback http://localhost:8081/auth/zoom/callback")
	})
}
