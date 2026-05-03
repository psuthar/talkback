package handlers

import (
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

// TestRetryMaterialSlides_ACLAndValidation_SCRUM287 covers the four
// no-side-effect rejection paths the retry endpoint enforces before
// kicking off background work: non-creator caller, missing material,
// cross-session material, and a non-pptx kind. The happy path returns
// 202 Accepted and is exercised separately.
func TestRetryMaterialSlides_ACLAndValidation_SCRUM287(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	creatorEmail := "retry-slides-creator@scrum287.test"
	creator := &models.User{
		ID:          uuid.New(),
		Email:       creatorEmail,
		DisplayName: "Retry Creator",
		Status:      "active",
		GlobalRole:  "creator",
	}
	require.NoError(t, h.DB.CreateUser(ctx, creator))

	stranger := &models.User{
		ID:          uuid.New(),
		Email:       "stranger-retry@scrum287.test",
		DisplayName: "Stranger",
		Status:      "active",
		GlobalRole:  "creator",
	}
	require.NoError(t, h.DB.CreateUser(ctx, stranger))

	mkSession := func(title string) *models.Session {
		s := &models.Session{
			ID:        uuid.New(),
			Title:     title,
			CreatedBy: &creatorEmail,
			Status:    models.SessionStatusOpen,
		}
		require.NoError(t, h.DB.CreateSession(ctx, s))
		return s
	}

	// MaterialSupportsDerivedSlideDeck checks the filename for .ppt/.pptx
	// (kind is always "document" today; the suffix is the canonical signal).
	mkMaterial := func(sessionID uuid.UUID, filename string) *models.Material {
		artifact, err := h.DB.CreateArtifact(ctx, sessionID, "fixture", nil)
		require.NoError(t, err)
		m := &models.Material{
			ID:          uuid.New(),
			ArtifactID:  artifact.ID,
			SessionID:   sessionID,
			Kind:        string(models.MaterialKindDocument),
			Filename:    filename,
			ContentType: "application/octet-stream",
			StorageURL:  "local:///tmp/" + filename,
			TextStatus:  models.MaterialTextStatusReady,
		}
		require.NoError(t, h.DB.CreateMaterial(ctx, m))
		return m
	}

	doRetry := func(t *testing.T, user *models.User, sid, mid uuid.UUID) *httptest.ResponseRecorder {
		t.Helper()
		path := "/api/sessions/" + sid.String() + "/materials/" + mid.String() + "/retry-slides"
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(""))
		req = req.WithContext(context.WithValue(req.Context(), userContextKey, user))
		w := httptest.NewRecorder()
		h.RetryMaterialSlides(w, req)
		return w
	}

	t.Run("non-creator -> 403", func(t *testing.T) {
		s := mkSession("retry forbidden")
		m := mkMaterial(s.ID, "deck.pptx")
		w := doRetry(t, stranger, s.ID, m.ID)
		assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	})

	t.Run("missing material -> 404", func(t *testing.T) {
		s := mkSession("retry missing material")
		w := doRetry(t, creator, s.ID, uuid.New())
		assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	})

	t.Run("cross-session material -> 400", func(t *testing.T) {
		s := mkSession("retry cross-session A")
		other := mkSession("retry cross-session B")
		m := mkMaterial(other.ID, "deck.pptx")
		w := doRetry(t, creator, s.ID, m.ID)
		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	})

	t.Run("non-pptx material -> 400", func(t *testing.T) {
		s := mkSession("retry wrong kind")
		m := mkMaterial(s.ID, "notes.pdf")
		w := doRetry(t, creator, s.ID, m.ID)
		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		var resp map[string]string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Contains(t, resp["error"], "slide derivation")
	})

	t.Run("happy path with .pptx returns 202", func(t *testing.T) {
		s := mkSession("retry happy path")
		m := mkMaterial(s.ID, "deck.pptx")
		w := doRetry(t, creator, s.ID, m.ID)
		require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
		var resp map[string]string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "queued", resp["state"])
		// Note: the actual slides goroutine will fail to find the source file
		// (StorageURL points at a non-existent local path); that's expected and
		// logged at WARN+. The handler's contract is "queued" — completion is
		// signalled separately via session_updated.
	})
}
