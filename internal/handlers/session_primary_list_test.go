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

// TestListSessions_PrimaryEnrichment_SCRUM280 asserts that GET /api/sessions
// returns the resolved primary descriptor on each row when a session has a
// primary set. The fixture mixes one document-primary, one link-primary, one
// video-primary (legacy kind=NULL fallback), and one no-primary session, and
// verifies the {kind, id, title?, status?} block is populated for the first
// three and absent for the fourth.
func TestListSessions_PrimaryEnrichment_SCRUM280(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	ctx := context.Background()
	creatorEmail := "primary-list-creator@scrum280.test"
	creator := &models.User{
		ID:          uuid.New(),
		Email:       creatorEmail,
		DisplayName: "List Primary Creator",
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

	mkSession := func(title string) *models.Session {
		email := creatorEmail
		s := &models.Session{
			ID:        uuid.New(),
			Title:     title,
			CreatedBy: &email,
			Status:    models.SessionStatusOpen,
		}
		require.NoError(t, h.DB.CreateSession(ctx, s))
		return s
	}

	// Doc-primary session.
	docSess := mkSession("doc primary list")
	artifact, err := h.DB.CreateArtifact(ctx, docSess.ID, "doc artifact", nil)
	require.NoError(t, err)
	docTitle := "Spec Sheet"
	material := &models.Material{
		ID:          uuid.New(),
		ArtifactID:  artifact.ID,
		SessionID:   docSess.ID,
		Kind:        string(models.MaterialKindDocument),
		Title:       &docTitle,
		Filename:    "spec.pdf",
		ContentType: "application/pdf",
		StorageURL:  "r2://b/spec.pdf",
		TextStatus:  models.MaterialTextStatusReady,
	}
	require.NoError(t, h.DB.CreateMaterial(ctx, material))
	_, err = h.DB.Pool.Exec(ctx, `
		UPDATE sessions SET primary_content_kind='document', primary_material_id=$1
		WHERE id=$2`, material.ID, docSess.ID)
	require.NoError(t, err)

	// Link-primary session.
	linkSess := mkSession("link primary list")
	linkTitle := "Reference link"
	link := &models.SessionLink{
		ID:        uuid.New(),
		SessionID: linkSess.ID,
		URL:       "https://example.com/spec",
		Title:     &linkTitle,
		Status:    "verified",
	}
	require.NoError(t, h.DB.CreateSessionLink(ctx, link))
	_, err = h.DB.Pool.Exec(ctx, `
		UPDATE sessions SET primary_content_kind='link', primary_session_link_id=$1
		WHERE id=$2`, link.ID, linkSess.ID)
	require.NoError(t, err)

	// Legacy video-primary session: kind NULL, primary_video_artifact_id set.
	// Resolver returns kind="video"; enrichment populates Title/Status from the
	// batched file_artifact lookup.
	videoSess := mkSession("video primary list")
	videoFilename := "lecture.mp4"
	fa := &models.FileArtifact{
		ID:              uuid.New(),
		SessionID:       &videoSess.ID,
		OwnerUserID:     &creator.ID,
		Kind:            models.FileArtifactKindVideo,
		Filename:        &videoFilename,
		ContentType:     "video/mp4",
		StorageProvider: "local",
		StorageBucket:   "talkback-test",
		StorageKey:      "videos/lecture.mp4",
		Status:          models.FileArtifactStatusReady,
	}
	require.NoError(t, h.DB.CreateFileArtifact(ctx, fa))
	_, err = h.DB.Pool.Exec(ctx, `
		UPDATE sessions SET primary_video_artifact_id=$1
		WHERE id=$2`, fa.ID, videoSess.ID)
	require.NoError(t, err)

	// No-primary session.
	emptySess := mkSession("no primary list")

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: cookieValue})
	w := httptest.NewRecorder()
	h.RequireAuth(h.ListSessions)(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Decode loosely so we can read the additive `primary` field that the
	// new wrapper appends. Validates wire-shape backward compat (existing
	// session/my_role/member_count fields still present at the top level).
	var rows []map[string]json.RawMessage
	require.NoError(t, json.NewDecoder(w.Body).Decode(&rows))
	require.Len(t, rows, 4)

	primaryByTitle := map[string]*SessionPrimaryDescriptor{}
	for _, row := range rows {
		// Pull the title out of the embedded session block.
		var sess struct {
			Title string `json:"title"`
		}
		require.NoError(t, json.Unmarshal(row["session"], &sess))
		var p *SessionPrimaryDescriptor
		if raw, ok := row["primary"]; ok && string(raw) != "null" {
			require.NoError(t, json.Unmarshal(raw, &p))
		}
		primaryByTitle[sess.Title] = p
	}

	require.NotNil(t, primaryByTitle["doc primary list"])
	assert.Equal(t, "document", primaryByTitle["doc primary list"].Kind)
	assert.Equal(t, material.ID, primaryByTitle["doc primary list"].ID)
	require.NotNil(t, primaryByTitle["doc primary list"].Title)
	assert.Equal(t, "Spec Sheet", *primaryByTitle["doc primary list"].Title)

	require.NotNil(t, primaryByTitle["link primary list"])
	assert.Equal(t, "link", primaryByTitle["link primary list"].Kind)
	assert.Equal(t, link.ID, primaryByTitle["link primary list"].ID)
	require.NotNil(t, primaryByTitle["link primary list"].Title)
	assert.Equal(t, "Reference link", *primaryByTitle["link primary list"].Title)

	require.NotNil(t, primaryByTitle["video primary list"], "legacy video primary must resolve via fallback")
	assert.Equal(t, "video", primaryByTitle["video primary list"].Kind)
	assert.Equal(t, fa.ID, primaryByTitle["video primary list"].ID)
	require.NotNil(t, primaryByTitle["video primary list"].Title)
	assert.Equal(t, "lecture.mp4", *primaryByTitle["video primary list"].Title)

	assert.Nil(t, primaryByTitle["no primary list"], "session without primary must omit the descriptor")
	_ = emptySess
}
