package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SCRUM-340: cloned session inherits framing context and primary content pointer.

func TestCopySession_PremiseDecisionOutcome_Copied(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	srcSessionID, creator := seedSessionForMetadataCopy(t, h)

	// Set premise / primary_decision / decision_outcome on the source.
	premise := "Should we adopt feature X?"
	decision := "Adopt with caveats"
	outcome := "Approved with one dissent"
	require.NoError(t, h.DB.UpdateSessionContext(ctx, srcSessionID, &premise, &decision, &outcome))

	dstID := postCopy(t, h, srcSessionID, creator)

	dst, err := h.DB.GetSession(ctx, dstID)
	require.NoError(t, err)
	require.NotNil(t, dst.Premise)
	assert.Equal(t, premise, *dst.Premise)
	require.NotNil(t, dst.PrimaryDecision)
	assert.Equal(t, decision, *dst.PrimaryDecision)
	require.NotNil(t, dst.DecisionOutcome)
	assert.Equal(t, outcome, *dst.DecisionOutcome)
}

func TestCopySession_SourceReferenceURL_Copied(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	creator := addUserSessionCookie(t, h, &http.Request{Header: http.Header{}}, "src-ref-url@example.com")
	// Build a session directly with source_reference_url set; CreateSession
	// persists it at INSERT time so we don't have a public UPDATE helper.
	srcURL := "https://meet.example.com/abc-defg-hij"
	src := &models.Session{
		ID:                 uuid.New(),
		Title:              "Source w/ URL",
		CreatedBy:          &creator.Email,
		Status:             models.SessionStatusOpen,
		SourceReferenceURL: &srcURL,
	}
	require.NoError(t, h.DB.CreateSession(ctx, src))

	dstID := postCopy(t, h, src.ID, creator)

	dst, err := h.DB.GetSession(ctx, dstID)
	require.NoError(t, err)
	require.NotNil(t, dst.SourceReferenceURL, "clone must inherit source_reference_url")
	assert.Equal(t, srcURL, *dst.SourceReferenceURL)
}

func TestCopySession_PrimaryRemap_Document(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	srcSessionID, creator := seedSessionForMetadataCopy(t, h)

	// Seed a material on the auto-created artifact and set as primary document.
	srcArtifacts, err := h.DB.GetArtifactsBySessionID(ctx, srcSessionID)
	require.NoError(t, err)
	require.NotEmpty(t, srcArtifacts)
	srcMaterial := &models.Material{
		ID:              uuid.New(),
		ArtifactID:      srcArtifacts[0].ID,
		SessionID:       srcSessionID,
		Kind:            "document",
		Filename:        "doc.pdf",
		ContentType:     "application/pdf",
		StorageProvider: "local",
		StorageKey:      "",
		StorageURL:      "",
		TextStatus:      "ready",
	}
	require.NoError(t, h.DB.CreateMaterial(ctx, srcMaterial))
	require.NoError(t, h.DB.UpdateSessionPrimary(ctx, srcSessionID, models.SessionPrimaryContentKindDocument, &srcMaterial.ID))

	dstID := postCopy(t, h, srcSessionID, creator)

	dst, err := h.DB.GetSession(ctx, dstID)
	require.NoError(t, err)
	require.NotNil(t, dst.PrimaryContentKind)
	assert.Equal(t, models.SessionPrimaryContentKindDocument, *dst.PrimaryContentKind)
	require.NotNil(t, dst.PrimaryMaterialID, "clone must have primary_material_id remapped, not nil")
	assert.NotEqual(t, srcMaterial.ID, *dst.PrimaryMaterialID, "primary_material_id must point to the new material on the clone, not the source")

	// Verify the new material's session_id matches the clone.
	dstMaterials, err := h.DB.GetActiveMaterialsBySessionID(ctx, dstID)
	require.NoError(t, err)
	found := false
	for _, m := range dstMaterials {
		if m.ID == *dst.PrimaryMaterialID {
			found = true
			assert.Equal(t, dstID, m.SessionID)
		}
	}
	assert.True(t, found, "primary_material_id must resolve to a material in the clone")
}

func TestCopySession_PrimaryRemap_Link(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	srcSessionID, creator := seedSessionForMetadataCopy(t, h)

	srcLink := &models.SessionLink{
		ID:        uuid.New(),
		SessionID: srcSessionID,
		URL:       "https://example.com/agenda",
		Status:    models.SessionLinkStatusVerified,
	}
	require.NoError(t, h.DB.CreateSessionLink(ctx, srcLink))
	require.NoError(t, h.DB.UpdateSessionPrimary(ctx, srcSessionID, models.SessionPrimaryContentKindLink, &srcLink.ID))

	dstID := postCopy(t, h, srcSessionID, creator)

	dst, err := h.DB.GetSession(ctx, dstID)
	require.NoError(t, err)
	require.NotNil(t, dst.PrimaryContentKind)
	assert.Equal(t, models.SessionPrimaryContentKindLink, *dst.PrimaryContentKind)
	require.NotNil(t, dst.PrimarySessionLinkID, "clone must have primary_session_link_id remapped")
	assert.NotEqual(t, srcLink.ID, *dst.PrimarySessionLinkID, "primary_session_link_id must point to the new link on the clone")
}

// TestCopySession_PrimaryRemap_NilWhenChildMissing verifies the failure mode:
// when the source's explicit primary pointer references a child that the copy
// could not reproduce, the clone's primary_content_kind / primary_*_id fields
// are left null (no dangling reference to the source). Achieved by seeding a
// primary pointer to a material that is then soft-deleted before copy — the
// copy step will skip it and the metadata remap will miss.
func TestCopySession_PrimaryRemap_NilWhenChildMissing(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	srcSessionID, creator := seedSessionForMetadataCopy(t, h)
	srcArtifacts, err := h.DB.GetArtifactsBySessionID(ctx, srcSessionID)
	require.NoError(t, err)
	require.NotEmpty(t, srcArtifacts)

	// Seed a soft-deleted material as primary. ImportMaterials only iterates
	// active materials, so the remap will not contain this id and the
	// metadata step must skip the primary set.
	srcMaterial := &models.Material{
		ID:              uuid.New(),
		ArtifactID:      srcArtifacts[0].ID,
		SessionID:       srcSessionID,
		Kind:            "document",
		Filename:        "deleted.pdf",
		ContentType:     "application/pdf",
		StorageProvider: "local",
	}
	require.NoError(t, h.DB.CreateMaterial(ctx, srcMaterial))
	require.NoError(t, h.DB.UpdateSessionPrimary(ctx, srcSessionID, models.SessionPrimaryContentKindDocument, &srcMaterial.ID))
	require.NoError(t, h.DB.SoftDeleteMaterial(ctx, srcMaterial.ID))

	dstID := postCopy(t, h, srcSessionID, creator)

	dst, err := h.DB.GetSession(ctx, dstID)
	require.NoError(t, err)
	// The metadata remap should miss → clone has no explicit primary set.
	assert.Nil(t, dst.PrimaryMaterialID, "primary_material_id must be nil when source primary references a child not copied")
}

// --- helpers ---

func seedSessionForMetadataCopy(t *testing.T, h *Handlers) (uuid.UUID, *models.User) {
	t.Helper()
	createReqBody, _ := json.Marshal(map[string]string{"title": "Source Session"})
	createReq := httptest.NewRequest(http.MethodPost, "/api/sessions", bytes.NewReader(createReqBody))
	createReq.Header.Set("Content-Type", "application/json")
	creator := addUserSessionCookie(t, h, createReq, fmt.Sprintf("metadata-copy-%s@example.com", uuid.New().String()[:8]))
	createW := httptest.NewRecorder()
	h.RequireAuth(h.CreateSession)(createW, createReq)
	require.Equal(t, http.StatusCreated, createW.Code, "create source: %s", createW.Body.String())
	var resp CreateSessionResponse
	require.NoError(t, json.Unmarshal(createW.Body.Bytes(), &resp))
	id, err := uuid.Parse(resp.ID)
	require.NoError(t, err)
	return id, creator
}

func postCopy(t *testing.T, h *Handlers, srcID uuid.UUID, creator *models.User) uuid.UUID {
	t.Helper()
	copyReq := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/api/sessions/%s/copy", srcID.String()),
		bytes.NewReader([]byte(`{}`)),
	)
	copyReq.Header.Set("Content-Type", "application/json")
	addUserSessionCookieFor(t, h, copyReq, creator)
	w := httptest.NewRecorder()
	h.RequireAuth(h.CopySession)(w, copyReq)
	require.Equal(t, http.StatusCreated, w.Code, "copy: %s", w.Body.String())
	var resp CopySessionResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	id, err := uuid.Parse(resp.ID)
	require.NoError(t, err)
	return id
}
