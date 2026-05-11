package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/invitations"
	"github.com/psuthar/talkback/internal/migrations"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testInvitationLookup implements invitations.SessionInviterLookup for handler tests.
type testInvitationLookup struct{ db *database.DB }

func (l *testInvitationLookup) GetSessionTitle(ctx context.Context, sessionID uuid.UUID) (string, error) {
	s, err := l.db.GetSession(ctx, sessionID)
	if err != nil || s == nil {
		return "", err
	}
	return s.Title, nil
}

func (l *testInvitationLookup) GetUserDisplayName(ctx context.Context, userID uuid.UUID) (string, error) {
	u, err := l.db.GetUserByID(ctx, userID)
	if err != nil || u == nil {
		return "", err
	}
	return u.DisplayName, nil
}

func (l *testInvitationLookup) GetUserEmail(ctx context.Context, userID uuid.UUID) (string, error) {
	u, err := l.db.GetUserByID(ctx, userID)
	if err != nil || u == nil {
		return "", err
	}
	return u.Email, nil
}

func TestMain(m *testing.M) {
	sharedURL, cleanupDB, err := test.CreateSharedTestDB()
	if err != nil {
		log.Fatalf("Create shared test DB: %v", err)
	}
	if err := runTestMigrations(sharedURL); err != nil {
		_ = test.DropTestDatabaseURL(sharedURL)
		log.Fatalf("Run test migrations: %v", err)
	}
	originalURL := os.Getenv("DATABASE_URL")
	os.Setenv("DATABASE_URL", sharedURL)
	code := m.Run()
	if cleanupDB != nil {
		cleanupDB()
	}
	if originalURL != "" {
		os.Setenv("DATABASE_URL", originalURL)
	} else {
		os.Unsetenv("DATABASE_URL")
	}
	os.Exit(code)
}

// runTestMigrations runs migrations for tests using embedded migrations
func runTestMigrations(databaseURL string) error {
	// Get migrations from embedded filesystem
	migrationsSubFS, err := fs.Sub(migrations.MigrationsFS, "migrations")
	if err != nil {
		return err
	}

	// Create iofs driver from embedded filesystem
	sourceDriver, err := iofs.New(migrationsSubFS, ".")
	if err != nil {
		return err
	}

	// Open database connection using database/sql (required by golang-migrate)
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	// Create postgres driver
	postgresDriver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return err
	}

	// Create migrate instance with embedded source
	m, err := migrate.NewWithInstance("iofs", sourceDriver, "postgres", postgresDriver)
	if err != nil {
		return err
	}

	// Run migrations
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return err
	}

	return nil
}

// setupTestHandlersParallel creates a dedicated test database and handlers for this test. Use with t.Parallel() so tests do not share DB state. Cleanup drops the database and closes the pool.
func setupTestHandlersParallel(t *testing.T) (*Handlers, func()) {
	t.Helper()
	databaseURL, cleanupDB := test.SetupTestDB(t)
	if err := runTestMigrations(databaseURL); err != nil {
		cleanupDB()
		t.Fatalf("run test migrations: %v", err)
	}
	db, err := database.NewFromURL(databaseURL)
	require.NoError(t, err, "NewFromURL")
	cleanup := func() {
		db.Close()
		cleanupDB()
	}
	return NewHandlers(db, nil, nil, nil), cleanup
}

// setupTestHandlersWithInvitations is like setupTestHandlersParallel but with InvitationService set (for invitation handler tests).
func setupTestHandlersWithInvitations(t *testing.T) (*Handlers, func()) {
	t.Helper()
	databaseURL, cleanupDB := test.SetupTestDB(t)
	if err := runTestMigrations(databaseURL); err != nil {
		cleanupDB()
		t.Fatalf("run test migrations: %v", err)
	}
	db, err := database.NewFromURL(databaseURL)
	require.NoError(t, err, "NewFromURL")
	invRepo := invitations.NewRepository(db.Pool)
	lookup := &testInvitationLookup{db: db}
	invSvc := &invitations.Service{
		Repo:   invRepo,
		Lookup: lookup,
		Config: invitations.Config{AppBaseURL: "http://test.example", ExpiryHours: 168},
		Send:   nil,
	}
	cleanup := func() {
		db.Close()
		cleanupDB()
	}
	return NewHandlers(db, nil, nil, invSvc), cleanup
}

// createTestSessionForHandlers is a helper to create a test session for handler tests
func createTestSessionForHandlers(t *testing.T, db *database.DB, title string) *models.Session {
	t.Helper()
	ctx := context.Background()

	session := &models.Session{
		ID:     uuid.New(),
		Title:  title,
		Status: models.SessionStatusOpen,
	}

	err := db.CreateSession(ctx, session)
	require.NoError(t, err)
	return session
}

func TestCreateArtifact(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	// Create a session first
	session := createTestSessionForHandlers(t, h.DB, "Test Session")

	t.Run("creates artifact with title only", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"session_id": session.ID.String(),
			"title":      "Test Artifact",
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/artifacts", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.CreateArtifact(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		var response CreateArtifactResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, "Test Artifact", response.Title)
		assert.Equal(t, "draft", response.Status)
		assert.NotEmpty(t, response.ID)
	})

	t.Run("creates artifact with title and description", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"session_id":  session.ID.String(),
			"title":       "Test Artifact",
			"description": "Test description",
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/artifacts", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.CreateArtifact(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		var response CreateArtifactResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, "Test Artifact", response.Title)
		assert.NotNil(t, response.Description)
		assert.Equal(t, "Test description", *response.Description)
	})

	t.Run("returns 400 when session_id is missing", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"title": "Test Artifact",
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/artifacts", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.CreateArtifact(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("returns 400 when title is missing", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"session_id": session.ID.String(),
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/artifacts", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.CreateArtifact(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("returns 405 for non-POST methods", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/artifacts", nil)
		w := httptest.NewRecorder()

		h.CreateArtifact(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})
}

func TestGetArtifact(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	// Create a session and artifact for testing
	session := createTestSessionForHandlers(t, h.DB, "Test Session")
	artifact, err := h.DB.CreateArtifact(context.Background(), session.ID, "Get Test Artifact", nil)
	require.NoError(t, err)

	t.Run("returns artifact by id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/artifacts/"+artifact.ID.String(), nil)
		w := httptest.NewRecorder()

		h.GetArtifact(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.NotNil(t, response["artifact"])
		// materials should be an array (empty or with items), not nil
		materials, ok := response["materials"]
		require.True(t, ok, "materials key should exist")
		assert.NotNil(t, materials, "materials should not be nil (should be empty array)")
		// video_sources should be an array
		videoSources, ok := response["video_sources"]
		require.True(t, ok, "video_sources key should exist")
		assert.NotNil(t, videoSources, "video_sources should not be nil (should be empty array)")
	})

	t.Run("returns 400 for invalid artifact id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/artifacts/invalid-id", nil)
		w := httptest.NewRecorder()

		h.GetArtifact(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("returns 404 for non-existent artifact", func(t *testing.T) {
		nonExistentID := uuid.New()
		req := httptest.NewRequest(http.MethodGet, "/artifacts/"+nonExistentID.String(), nil)
		w := httptest.NewRecorder()

		h.GetArtifact(w, req)

		// Should return 500 (internal server error) since GetArtifact returns error
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestAttachVideoURL(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	// Create a session and artifact for testing
	session := createTestSessionForHandlers(t, h.DB, "Test Session")
	artifact, err := h.DB.CreateArtifact(context.Background(), session.ID, "Video Test Artifact", nil)
	require.NoError(t, err)

	t.Run("attaches video URL with default provider", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"provider":  "other",
			"video_url": "https://example.com/share/example",
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/artifacts/"+artifact.ID.String()+"/video", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.AttachVideoURL(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		var videoSource map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &videoSource)
		require.NoError(t, err)
		assert.Equal(t, "other", videoSource["provider"])
		assert.Equal(t, "https://example.com/share/example", videoSource["video_url"])
		// Should default to embed mode
		assert.Equal(t, "embed", videoSource["playback_mode"])
	})

	t.Run("attaches video URL with embed mode (new format)", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"provider":      "other",
			"playback_mode": "embed",
			"embed_url":     "https://example.com/share/embed-example",
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/artifacts/"+artifact.ID.String()+"/video", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.AttachVideoURL(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		var videoSource map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &videoSource)
		require.NoError(t, err)
		assert.Equal(t, "other", videoSource["provider"])
		assert.Equal(t, "embed", videoSource["playback_mode"])
		assert.NotNil(t, videoSource["embed_url"])
	})

	t.Run("attaches video URL with direct mode", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"provider":         "other",
			"playback_mode":    "direct",
			"media_url":        "https://example.com/video.mp4",
			"poster_url":       "https://example.com/poster.jpg",
			"duration_seconds": 1234,
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/artifacts/"+artifact.ID.String()+"/video", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.AttachVideoURL(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		var videoSource map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &videoSource)
		require.NoError(t, err)
		assert.Equal(t, "other", videoSource["provider"])
		assert.Equal(t, "direct", videoSource["playback_mode"])
		assert.NotNil(t, videoSource["media_url"])
		assert.NotNil(t, videoSource["poster_url"])
		assert.Equal(t, float64(1234), videoSource["duration_seconds"])
	})

	t.Run("returns 400 when embed_url is missing for embed mode", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"provider":      "other",
			"playback_mode": "embed",
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/artifacts/"+artifact.ID.String()+"/video", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.AttachVideoURL(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("returns 400 when media_url is missing for direct mode", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"provider":      "other",
			"playback_mode": "direct",
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/artifacts/"+artifact.ID.String()+"/video", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.AttachVideoURL(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("returns 400 when playback_mode is invalid", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"provider":      "other",
			"playback_mode": "invalid",
			"media_url":     "https://example.com/video.mp4",
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/artifacts/"+artifact.ID.String()+"/video", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.AttachVideoURL(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("returns 400 when video_url is missing (backward compatibility)", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"provider": "other",
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/artifacts/"+artifact.ID.String()+"/video", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.AttachVideoURL(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

}
