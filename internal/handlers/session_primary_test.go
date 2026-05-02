package handlers

import (
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func uuidPtr(u uuid.UUID) *uuid.UUID                            { return &u }
func kindPtr(k models.SessionPrimaryContentKind) *models.SessionPrimaryContentKind { return &k }

func TestResolveSessionPrimary_LegacyVideoFallback(t *testing.T) {
	videoID := uuid.New()
	session := &models.Session{PrimaryVideoArtifactID: uuidPtr(videoID)}
	got := resolveSessionPrimary(session)
	require.NotNil(t, got, "legacy video session must produce a descriptor")
	assert.Equal(t, "video", got.Kind)
	assert.Equal(t, videoID, got.ID)
}

func TestResolveSessionPrimary_NoPrimary(t *testing.T) {
	assert.Nil(t, resolveSessionPrimary(&models.Session{}), "empty session has no primary")
	assert.Nil(t, resolveSessionPrimary(nil), "nil session is safe")
}

func TestResolveSessionPrimary_ExplicitVideo(t *testing.T) {
	videoID := uuid.New()
	session := &models.Session{
		PrimaryContentKind:     kindPtr(models.SessionPrimaryContentKindVideo),
		PrimaryVideoArtifactID: uuidPtr(videoID),
	}
	got := resolveSessionPrimary(session)
	require.NotNil(t, got)
	assert.Equal(t, "video", got.Kind)
	assert.Equal(t, videoID, got.ID)
}

func TestResolveSessionPrimary_ExplicitDocument(t *testing.T) {
	matID := uuid.New()
	session := &models.Session{
		PrimaryContentKind: kindPtr(models.SessionPrimaryContentKindDocument),
		PrimaryMaterialID:  uuidPtr(matID),
	}
	got := resolveSessionPrimary(session)
	require.NotNil(t, got)
	assert.Equal(t, "document", got.Kind)
	assert.Equal(t, matID, got.ID)
}

func TestResolveSessionPrimary_ExplicitLink(t *testing.T) {
	linkID := uuid.New()
	session := &models.Session{
		PrimaryContentKind:   kindPtr(models.SessionPrimaryContentKindLink),
		PrimarySessionLinkID: uuidPtr(linkID),
	}
	got := resolveSessionPrimary(session)
	require.NotNil(t, got)
	assert.Equal(t, "link", got.Kind)
	assert.Equal(t, linkID, got.ID)
}

// When primary_content_kind is set but the matching pointer is missing
// (e.g. an inconsistent state someone wrote via direct SQL), the resolver
// returns nil — the handler then omits the JSON field. This matches the
// "explicit kind requires a matching pointer" invariant SCRUM-272's PATCH
// endpoint will enforce.
func TestResolveSessionPrimary_ExplicitKindMissingPointer(t *testing.T) {
	// kind=document but primary_material_id is nil: invalid state, no descriptor.
	session := &models.Session{
		PrimaryContentKind: kindPtr(models.SessionPrimaryContentKindDocument),
	}
	assert.Nil(t, resolveSessionPrimary(session))
}

// Explicit kind takes precedence over the legacy column even when both are
// populated, so a session that has been migrated to kind=document still
// resolves to document even if primary_video_artifact_id was never cleared.
func TestResolveSessionPrimary_ExplicitDocumentBeatsLegacyVideo(t *testing.T) {
	matID := uuid.New()
	videoID := uuid.New()
	session := &models.Session{
		PrimaryContentKind:     kindPtr(models.SessionPrimaryContentKindDocument),
		PrimaryMaterialID:      uuidPtr(matID),
		PrimaryVideoArtifactID: uuidPtr(videoID),
	}
	got := resolveSessionPrimary(session)
	require.NotNil(t, got)
	assert.Equal(t, "document", got.Kind)
	assert.Equal(t, matID, got.ID)
}

func TestEnrichSessionPrimaryFromMaterials_PopulatesTitleAndStatus(t *testing.T) {
	matID := uuid.New()
	custom := "custom-doc-title"
	materials := []*models.Material{
		{ID: uuid.New(), Filename: "other.pdf"},
		{
			ID:         matID,
			Filename:   "fallback.pdf",
			Title:      &custom,
			TextStatus: models.MaterialTextStatusReady,
		},
	}
	desc := &SessionPrimaryDescriptor{Kind: "document", ID: matID}
	got := enrichSessionPrimaryFromMaterials(desc, materials)
	require.NotNil(t, got)
	require.NotNil(t, got.Title)
	assert.Equal(t, "custom-doc-title", *got.Title, "Material.Title preferred over Filename")
	require.NotNil(t, got.Status)
	assert.Equal(t, "ready", *got.Status)
}

func TestEnrichSessionPrimaryFromMaterials_FallsBackToFilename(t *testing.T) {
	matID := uuid.New()
	materials := []*models.Material{
		{ID: matID, Filename: "fallback.pdf", TextStatus: models.MaterialTextStatusPending},
	}
	desc := &SessionPrimaryDescriptor{Kind: "document", ID: matID}
	got := enrichSessionPrimaryFromMaterials(desc, materials)
	require.NotNil(t, got)
	require.NotNil(t, got.Title)
	assert.Equal(t, "fallback.pdf", *got.Title)
}

func TestEnrichSessionPrimaryFromMaterials_KindMismatchIsNoop(t *testing.T) {
	matID := uuid.New()
	materials := []*models.Material{{ID: matID, Filename: "x.pdf"}}
	desc := &SessionPrimaryDescriptor{Kind: "video", ID: matID}
	got := enrichSessionPrimaryFromMaterials(desc, materials)
	require.NotNil(t, got)
	assert.Nil(t, got.Title, "non-document kind must not be enriched from materials")
}

func TestEnrichSessionPrimaryFromLinks_TitlePrefersLinkTitleThenURL(t *testing.T) {
	linkID := uuid.New()
	linkTitle := "Important spec"
	links := []*models.SessionLink{
		{ID: uuid.New(), URL: "https://other"},
		{ID: linkID, URL: "https://example", Title: &linkTitle, Status: models.SessionLinkStatusVerified},
	}
	desc := &SessionPrimaryDescriptor{Kind: "link", ID: linkID}
	got := enrichSessionPrimaryFromLinks(desc, links)
	require.NotNil(t, got.Title)
	assert.Equal(t, "Important spec", *got.Title)

	// Drop the title — URL becomes fallback.
	links[1].Title = nil
	desc2 := &SessionPrimaryDescriptor{Kind: "link", ID: linkID}
	got2 := enrichSessionPrimaryFromLinks(desc2, links)
	require.NotNil(t, got2.Title)
	assert.Equal(t, "https://example", *got2.Title)
}

func TestEnrichSessionPrimaryFromFileArtifact_VideoOnly(t *testing.T) {
	faID := uuid.New()
	filename := "intro.mp4"
	fa := &models.FileArtifact{
		ID:       faID,
		Filename: &filename,
		Status:   models.FileArtifactStatusReady,
	}
	desc := &SessionPrimaryDescriptor{Kind: "video", ID: faID}
	got := enrichSessionPrimaryFromFileArtifact(desc, fa)
	require.NotNil(t, got.Title)
	assert.Equal(t, "intro.mp4", *got.Title)
	require.NotNil(t, got.Status)
	assert.Equal(t, "ready", *got.Status)

	// Wrong ID — no enrichment, descriptor returned unchanged.
	desc2 := &SessionPrimaryDescriptor{Kind: "video", ID: uuid.New()}
	got2 := enrichSessionPrimaryFromFileArtifact(desc2, fa)
	assert.Nil(t, got2.Title)

	// Non-video kind — no enrichment.
	desc3 := &SessionPrimaryDescriptor{Kind: "document", ID: faID}
	got3 := enrichSessionPrimaryFromFileArtifact(desc3, fa)
	assert.Nil(t, got3.Title)
}

func TestEnrichSessionPrimaryFromFileArtifact_NilFileArtifactSafe(t *testing.T) {
	desc := &SessionPrimaryDescriptor{Kind: "video", ID: uuid.New()}
	got := enrichSessionPrimaryFromFileArtifact(desc, nil)
	require.NotNil(t, got)
	assert.Nil(t, got.Title)
}
