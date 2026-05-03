package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sptr(s string) *string { return &s }

func TestParseSessionPrimaryRequest_NilOrEmptyClears(t *testing.T) {
	got, err := parseSessionPrimaryRequest(nil)
	require.NoError(t, err)
	assert.Equal(t, models.SessionPrimaryContentKind(""), got.Kind)
	assert.Nil(t, got.ID)

	got, err = parseSessionPrimaryRequest(&SessionPrimaryUpdate{})
	require.NoError(t, err)
	assert.Equal(t, models.SessionPrimaryContentKind(""), got.Kind)
	assert.Nil(t, got.ID)

	got, err = parseSessionPrimaryRequest(&SessionPrimaryUpdate{Kind: sptr("")})
	require.NoError(t, err)
	assert.Equal(t, models.SessionPrimaryContentKind(""), got.Kind)
}

func TestParseSessionPrimaryRequest_KindRequiresID(t *testing.T) {
	for _, k := range []string{"video", "document", "link"} {
		_, err := parseSessionPrimaryRequest(&SessionPrimaryUpdate{Kind: sptr(k)})
		require.Error(t, err, "kind=%s without id must error", k)
		var derr *primaryUpdateError
		require.ErrorAs(t, err, &derr)
		assert.Equal(t, http.StatusBadRequest, derr.status)
		assert.Equal(t, "PRIMARY_MISSING_ID", derr.code)
	}
}

func TestParseSessionPrimaryRequest_BadKind(t *testing.T) {
	id := uuid.New()
	_, err := parseSessionPrimaryRequest(&SessionPrimaryUpdate{Kind: sptr("audio"), ID: &id})
	require.Error(t, err)
	var derr *primaryUpdateError
	require.ErrorAs(t, err, &derr)
	assert.Equal(t, http.StatusBadRequest, derr.status)
	assert.Equal(t, "PRIMARY_BAD_KIND", derr.code)
}

func TestParseSessionPrimaryRequest_HappyPaths(t *testing.T) {
	id := uuid.New()
	for _, k := range []string{"video", "document", "link"} {
		got, err := parseSessionPrimaryRequest(&SessionPrimaryUpdate{Kind: sptr(k), ID: &id})
		require.NoError(t, err)
		assert.Equal(t, models.SessionPrimaryContentKind(k), got.Kind)
		require.NotNil(t, got.ID)
		assert.Equal(t, id, *got.ID)
	}
}

// TestUpdateSessionPrimary_HandlerEndToEnd exercises the PATCH path through
// the live handler to cover the ACL guard + session-ownership validation +
// DB write.
func TestUpdateSessionPrimary_HandlerEndToEnd(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	creatorEmail := "creator@example.com"
	creatorUser := &models.User{
		ID:          uuid.New(),
		Email:       creatorEmail,
		DisplayName: "Creator",
		Status:      "active",
		GlobalRole:  "creator",
	}
	require.NoError(t, h.DB.CreateUser(ctx, creatorUser))

	mkSession := func(t *testing.T, title string) *models.Session {
		t.Helper()
		s := &models.Session{
			ID:        uuid.New(),
			Title:     title,
			CreatedBy: &creatorEmail,
			Status:    models.SessionStatusOpen,
		}
		require.NoError(t, h.DB.CreateSession(ctx, s))
		// SCRUM-226 wires CreateSession to auto-add the creator as a
		// creator-role member, so userIsSessionEditor already returns true
		// for creatorUser without an explicit CreateSessionMembership call.
		return s
	}

	doPatch := func(t *testing.T, sessionID uuid.UUID, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPatch, "/api/sessions/"+sessionID.String(),
			bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), userContextKey, creatorUser))
		w := httptest.NewRecorder()
		h.UpdateSessionStatus(w, req)
		return w
	}

	t.Run("set kind=document with valid material -> 200", func(t *testing.T) {
		s := mkSession(t, "doc primary set")
		artifact, err := h.DB.CreateArtifact(ctx, s.ID, "doc artifact", nil)
		require.NoError(t, err)
		material := &models.Material{
			ID:          uuid.New(),
			ArtifactID:  artifact.ID,
			SessionID:   s.ID,
			Kind:        string(models.MaterialKindDocument),
			Filename:    "spec.pdf",
			ContentType: "application/pdf",
			StorageURL:  "r2://bucket/spec.pdf",
			TextStatus:  models.MaterialTextStatusReady,
		}
		require.NoError(t, h.DB.CreateMaterial(ctx, material))

		body := `{"primary":{"kind":"document","id":"` + material.ID.String() + `"}}`
		w := doPatch(t, s.ID, body)
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())

		got, err := h.DB.GetSession(ctx, s.ID)
		require.NoError(t, err)
		require.NotNil(t, got.PrimaryContentKind)
		assert.Equal(t, models.SessionPrimaryContentKindDocument, *got.PrimaryContentKind)
		require.NotNil(t, got.PrimaryMaterialID)
		assert.Equal(t, material.ID, *got.PrimaryMaterialID)
	})

	t.Run("set kind=document but material in different session -> 400", func(t *testing.T) {
		s := mkSession(t, "doc primary cross-session")
		other := mkSession(t, "other session for cross-check")

		artifact, err := h.DB.CreateArtifact(ctx, other.ID, "other artifact", nil)
		require.NoError(t, err)
		material := &models.Material{
			ID:          uuid.New(),
			ArtifactID:  artifact.ID,
			SessionID:   other.ID, // belongs to other, not s
			Kind:        string(models.MaterialKindDocument),
			Filename:    "x.pdf",
			ContentType: "application/pdf",
			StorageURL:  "r2://b/k",
			TextStatus:  models.MaterialTextStatusReady,
		}
		require.NoError(t, h.DB.CreateMaterial(ctx, material))

		body := `{"primary":{"kind":"document","id":"` + material.ID.String() + `"}}`
		w := doPatch(t, s.ID, body)
		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

		var resp map[string]string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "PRIMARY_MATERIAL_WRONG_SESSION", resp["code"])

		got, err := h.DB.GetSession(ctx, s.ID)
		require.NoError(t, err)
		assert.Nil(t, got.PrimaryContentKind, "session must remain unchanged after a 400")
	})

	t.Run("kind=document with bad uuid id -> 400 PRIMARY_MATERIAL_NOT_FOUND", func(t *testing.T) {
		s := mkSession(t, "doc primary bad id")
		body := `{"primary":{"kind":"document","id":"` + uuid.New().String() + `"}}`
		w := doPatch(t, s.ID, body)
		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		var resp map[string]string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "PRIMARY_MATERIAL_NOT_FOUND", resp["code"])
	})

	t.Run("kind=video without id -> 400 PRIMARY_MISSING_ID", func(t *testing.T) {
		s := mkSession(t, "video missing id")
		body := `{"primary":{"kind":"video"}}`
		w := doPatch(t, s.ID, body)
		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		var resp map[string]string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "PRIMARY_MISSING_ID", resp["code"])
	})

	t.Run("kind=audio (unknown) -> 400 PRIMARY_BAD_KIND", func(t *testing.T) {
		s := mkSession(t, "bad kind")
		body := `{"primary":{"kind":"audio","id":"` + uuid.New().String() + `"}}`
		w := doPatch(t, s.ID, body)
		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		var resp map[string]string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "PRIMARY_BAD_KIND", resp["code"])
	})

	t.Run("clear primary (kind='') -> 200 and resets pointers", func(t *testing.T) {
		s := mkSession(t, "clear primary")
		artifact, err := h.DB.CreateArtifact(ctx, s.ID, "doc artifact", nil)
		require.NoError(t, err)
		material := &models.Material{
			ID:          uuid.New(),
			ArtifactID:  artifact.ID,
			SessionID:   s.ID,
			Kind:        string(models.MaterialKindDocument),
			Filename:    "y.pdf",
			ContentType: "application/pdf",
			StorageURL:  "r2://b/k",
			TextStatus:  models.MaterialTextStatusReady,
		}
		require.NoError(t, h.DB.CreateMaterial(ctx, material))
		// Set a primary first.
		_, err = h.DB.Pool.Exec(ctx, `
			UPDATE sessions SET primary_content_kind='document', primary_material_id=$1
			WHERE id=$2`, material.ID, s.ID)
		require.NoError(t, err)

		body := `{"primary":{"kind":""}}`
		w := doPatch(t, s.ID, body)
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())

		got, err := h.DB.GetSession(ctx, s.ID)
		require.NoError(t, err)
		assert.Nil(t, got.PrimaryContentKind, "primary_content_kind cleared")
		assert.Nil(t, got.PrimaryMaterialID, "primary_material_id cleared")
	})

	t.Run("set kind=document returns enriched primary in PATCH response (SCRUM-279)", func(t *testing.T) {
		s := mkSession(t, "patch returns primary doc")
		artifact, err := h.DB.CreateArtifact(ctx, s.ID, "doc artifact for resp", nil)
		require.NoError(t, err)
		title := "Spec Sheet"
		material := &models.Material{
			ID:          uuid.New(),
			ArtifactID:  artifact.ID,
			SessionID:   s.ID,
			Kind:        string(models.MaterialKindDocument),
			Title:       &title,
			Filename:    "spec.pdf",
			ContentType: "application/pdf",
			StorageURL:  "r2://b/spec.pdf",
			TextStatus:  models.MaterialTextStatusReady,
		}
		require.NoError(t, h.DB.CreateMaterial(ctx, material))

		body := `{"primary":{"kind":"document","id":"` + material.ID.String() + `"}}`
		w := doPatch(t, s.ID, body)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		var resp struct {
			Primary *SessionPrimaryDescriptor `json:"primary"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.NotNil(t, resp.Primary, "PATCH response must include resolved primary")
		assert.Equal(t, "document", resp.Primary.Kind)
		assert.Equal(t, material.ID, resp.Primary.ID)
		require.NotNil(t, resp.Primary.Title)
		assert.Equal(t, "Spec Sheet", *resp.Primary.Title)
		require.NotNil(t, resp.Primary.Status)
		assert.Equal(t, string(models.MaterialTextStatusReady), *resp.Primary.Status)
	})

	t.Run("clear primary returns no primary descriptor (SCRUM-279)", func(t *testing.T) {
		s := mkSession(t, "patch returns primary cleared")
		artifact, err := h.DB.CreateArtifact(ctx, s.ID, "doc artifact for clear-resp", nil)
		require.NoError(t, err)
		material := &models.Material{
			ID:          uuid.New(),
			ArtifactID:  artifact.ID,
			SessionID:   s.ID,
			Kind:        string(models.MaterialKindDocument),
			Filename:    "z.pdf",
			ContentType: "application/pdf",
			StorageURL:  "r2://b/z.pdf",
			TextStatus:  models.MaterialTextStatusReady,
		}
		require.NoError(t, h.DB.CreateMaterial(ctx, material))
		_, err = h.DB.Pool.Exec(ctx, `
			UPDATE sessions SET primary_content_kind='document', primary_material_id=$1
			WHERE id=$2`, material.ID, s.ID)
		require.NoError(t, err)

		body := `{"primary":{"kind":""}}`
		w := doPatch(t, s.ID, body)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		// `primary` is omitted when there's nothing to render — the wrapper uses
		// `omitempty` plus a nil descriptor from the resolver.
		var resp map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		_, hasPrimary := resp["primary"]
		assert.False(t, hasPrimary, "cleared primary must not appear in PATCH response")
	})

	t.Run("kind=document with text_status=processing -> 400 PRIMARY_NOT_READY (SCRUM-281)", func(t *testing.T) {
		s := mkSession(t, "doc primary processing")
		artifact, err := h.DB.CreateArtifact(ctx, s.ID, "processing artifact", nil)
		require.NoError(t, err)
		material := &models.Material{
			ID:          uuid.New(),
			ArtifactID:  artifact.ID,
			SessionID:   s.ID,
			Kind:        string(models.MaterialKindDocument),
			Filename:    "wip.pdf",
			ContentType: "application/pdf",
			StorageURL:  "r2://b/wip.pdf",
			TextStatus:  models.MaterialTextStatusProcessing,
		}
		require.NoError(t, h.DB.CreateMaterial(ctx, material))

		body := `{"primary":{"kind":"document","id":"` + material.ID.String() + `"}}`
		w := doPatch(t, s.ID, body)
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		var resp map[string]string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "PRIMARY_NOT_READY", resp["code"])

		got, err := h.DB.GetSession(ctx, s.ID)
		require.NoError(t, err)
		assert.Nil(t, got.PrimaryContentKind, "session must remain unchanged after a 400")
	})

	t.Run("kind=document with text_status=failed -> 400 PRIMARY_NOT_READY (SCRUM-281)", func(t *testing.T) {
		s := mkSession(t, "doc primary failed")
		artifact, err := h.DB.CreateArtifact(ctx, s.ID, "failed artifact", nil)
		require.NoError(t, err)
		material := &models.Material{
			ID:          uuid.New(),
			ArtifactID:  artifact.ID,
			SessionID:   s.ID,
			Kind:        string(models.MaterialKindDocument),
			Filename:    "broken.pdf",
			ContentType: "application/pdf",
			StorageURL:  "r2://b/broken.pdf",
			TextStatus:  models.MaterialTextStatusFailed,
		}
		require.NoError(t, h.DB.CreateMaterial(ctx, material))

		body := `{"primary":{"kind":"document","id":"` + material.ID.String() + `"}}`
		w := doPatch(t, s.ID, body)
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		var resp map[string]string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "PRIMARY_NOT_READY", resp["code"])
	})

	t.Run("kind=link with status=processing -> 400 PRIMARY_NOT_READY (SCRUM-281)", func(t *testing.T) {
		s := mkSession(t, "link primary processing")
		linkTitle := "WIP link"
		link := &models.SessionLink{
			ID:        uuid.New(),
			SessionID: s.ID,
			URL:       "https://example.com/wip",
			Title:     &linkTitle,
			Status:    models.SessionLinkStatusProcessing,
		}
		require.NoError(t, h.DB.CreateSessionLink(ctx, link))

		body := `{"primary":{"kind":"link","id":"` + link.ID.String() + `"}}`
		w := doPatch(t, s.ID, body)
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		var resp map[string]string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "PRIMARY_NOT_READY", resp["code"])
	})

	t.Run("kind=video with file_artifact status=pending -> 400 PRIMARY_NOT_READY (SCRUM-281)", func(t *testing.T) {
		s := mkSession(t, "video primary pending")
		filename := "uploading.mp4"
		fa := &models.FileArtifact{
			ID:              uuid.New(),
			SessionID:       &s.ID,
			OwnerUserID:     &creatorUser.ID,
			Kind:            models.FileArtifactKindVideo,
			Filename:        &filename,
			ContentType:     "video/mp4",
			StorageProvider: "local",
			StorageBucket:   "test",
			StorageKey:      "vid/uploading.mp4",
			Status:          models.FileArtifactStatusPending,
		}
		require.NoError(t, h.DB.CreateFileArtifact(ctx, fa))

		body := `{"primary":{"kind":"video","id":"` + fa.ID.String() + `"}}`
		w := doPatch(t, s.ID, body)
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		var resp map[string]string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "PRIMARY_NOT_READY", resp["code"])
	})

	t.Run("non-creator -> 403", func(t *testing.T) {
		s := mkSession(t, "non-creator forbidden")
		stranger := &models.User{
			ID:          uuid.New(),
			Email:       "stranger@example.com",
			DisplayName: "Stranger",
			Status:      "active",
			GlobalRole:  "creator",
		}
		require.NoError(t, h.DB.CreateUser(ctx, stranger))

		body := `{"primary":{"kind":""}}`
		req := httptest.NewRequest(http.MethodPatch, "/api/sessions/"+s.ID.String(),
			bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), userContextKey, stranger))
		w := httptest.NewRecorder()
		h.UpdateSessionStatus(w, req)
		assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	})
}

// TestUpdateSessionPrimary_AuditHistory_SCRUM283 verifies that every
// successful PATCH primary writes one row to sessions_primary_history with
// the correct before/after kind+id and actor; failed PATCHes write nothing.
func TestUpdateSessionPrimary_AuditHistory_SCRUM283(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	creatorEmail := "history-creator@scrum283.test"
	creatorUser := &models.User{
		ID:          uuid.New(),
		Email:       creatorEmail,
		DisplayName: "History Creator",
		Status:      "active",
		GlobalRole:  "creator",
	}
	require.NoError(t, h.DB.CreateUser(ctx, creatorUser))

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

	doPatch := func(t *testing.T, sessionID uuid.UUID, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPatch, "/api/sessions/"+sessionID.String(),
			bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), userContextKey, creatorUser))
		w := httptest.NewRecorder()
		h.UpdateSessionStatus(w, req)
		return w
	}

	t.Run("set primary writes history row with prev=(none) new=(document)", func(t *testing.T) {
		s := mkSession("history initial set")
		artifact, err := h.DB.CreateArtifact(ctx, s.ID, "doc", nil)
		require.NoError(t, err)
		material := &models.Material{
			ID:          uuid.New(),
			ArtifactID:  artifact.ID,
			SessionID:   s.ID,
			Kind:        string(models.MaterialKindDocument),
			Filename:    "x.pdf",
			ContentType: "application/pdf",
			StorageURL:  "r2://b/x.pdf",
			TextStatus:  models.MaterialTextStatusReady,
		}
		require.NoError(t, h.DB.CreateMaterial(ctx, material))

		body := `{"primary":{"kind":"document","id":"` + material.ID.String() + `"}}`
		w := doPatch(t, s.ID, body)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		hist, err := h.DB.ListSessionPrimaryHistory(ctx, s.ID)
		require.NoError(t, err)
		require.Len(t, hist, 1, "exactly one audit row after a single set")
		row := hist[0]
		assert.Equal(t, s.ID, row.SessionID)
		require.NotNil(t, row.ActorUserID)
		assert.Equal(t, creatorUser.ID, *row.ActorUserID)
		assert.Equal(t, "", row.PrevKind)
		assert.Nil(t, row.PrevID)
		assert.Equal(t, "document", row.NewKind)
		require.NotNil(t, row.NewID)
		assert.Equal(t, material.ID, *row.NewID)
	})

	t.Run("clear primary writes history row with prev=(document) new=(none)", func(t *testing.T) {
		s := mkSession("history clear")
		artifact, err := h.DB.CreateArtifact(ctx, s.ID, "doc", nil)
		require.NoError(t, err)
		material := &models.Material{
			ID:          uuid.New(),
			ArtifactID:  artifact.ID,
			SessionID:   s.ID,
			Kind:        string(models.MaterialKindDocument),
			Filename:    "y.pdf",
			ContentType: "application/pdf",
			StorageURL:  "r2://b/y.pdf",
			TextStatus:  models.MaterialTextStatusReady,
		}
		require.NoError(t, h.DB.CreateMaterial(ctx, material))
		_, err = h.DB.Pool.Exec(ctx, `
			UPDATE sessions SET primary_content_kind='document', primary_material_id=$1
			WHERE id=$2`, material.ID, s.ID)
		require.NoError(t, err)

		body := `{"primary":{"kind":""}}`
		w := doPatch(t, s.ID, body)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		hist, err := h.DB.ListSessionPrimaryHistory(ctx, s.ID)
		require.NoError(t, err)
		require.Len(t, hist, 1)
		row := hist[0]
		assert.Equal(t, "document", row.PrevKind)
		require.NotNil(t, row.PrevID)
		assert.Equal(t, material.ID, *row.PrevID)
		assert.Equal(t, "", row.NewKind)
		assert.Nil(t, row.NewID)
	})

	t.Run("rejected PATCH (PRIMARY_NOT_READY) writes no history row", func(t *testing.T) {
		s := mkSession("history skipped on reject")
		artifact, err := h.DB.CreateArtifact(ctx, s.ID, "doc processing", nil)
		require.NoError(t, err)
		material := &models.Material{
			ID:          uuid.New(),
			ArtifactID:  artifact.ID,
			SessionID:   s.ID,
			Kind:        string(models.MaterialKindDocument),
			Filename:    "z.pdf",
			ContentType: "application/pdf",
			StorageURL:  "r2://b/z.pdf",
			TextStatus:  models.MaterialTextStatusProcessing,
		}
		require.NoError(t, h.DB.CreateMaterial(ctx, material))

		body := `{"primary":{"kind":"document","id":"` + material.ID.String() + `"}}`
		w := doPatch(t, s.ID, body)
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

		hist, err := h.DB.ListSessionPrimaryHistory(ctx, s.ID)
		require.NoError(t, err)
		assert.Empty(t, hist, "no audit row when PATCH rejected")
	})
}

// Sanity: primaryUpdateError satisfies errors.As as expected.
func TestPrimaryUpdateError_ErrorsAs(t *testing.T) {
	src := &primaryUpdateError{status: 400, code: "X", message: "y"}
	var derr *primaryUpdateError
	assert.True(t, errors.As(error(src), &derr))
	assert.Equal(t, "y", derr.Error())
}
