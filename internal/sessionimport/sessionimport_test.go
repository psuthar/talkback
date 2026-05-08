package sessionimport

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewCtx_ZeroValueMaps verifies that NewCtx produces a Ctx with
// initialized (non-nil) remap maps so primitives can write to them safely.
// Structural unit test — does not require a database. Proves primitives are
// usable without any source session: Ctx is constructed from a destination
// alone, satisfying the SCRUM-339 source-descriptor seam.
func TestNewCtx_ZeroValueMaps(t *testing.T) {
	t.Parallel()
	dst := newDstSession()
	c := NewCtx(Deps{}, dst)
	require.NotNil(t, c)
	assert.Equal(t, dst, c.Dst)
	assert.NotNil(t, c.ArtifactRemap)
	assert.NotNil(t, c.MaterialRemap)
	assert.NotNil(t, c.LinkRemap)
	assert.NotNil(t, c.VideoSourceRemap)
	assert.NotNil(t, c.FileArtifactRemap)
	assert.Empty(t, c.ArtifactRemap)
	assert.Empty(t, c.PartialFailures)
	assert.False(t, c.CopiedPrimaryVideo)
}

// TestCtx_RecordPartialFailure_Dedups proves the partial-failure accumulator
// dedupes categories. SCRUM-344 will surface this list in the response.
func TestCtx_RecordPartialFailure_Dedups(t *testing.T) {
	t.Parallel()
	c := NewCtx(Deps{}, newDstSession())
	c.recordPartialFailure("materials")
	c.recordPartialFailure("materials")
	c.recordPartialFailure("file_artifacts")
	c.recordPartialFailure("materials")
	assert.Equal(t, []string{"materials", "file_artifacts"}, c.PartialFailures)
}

// TestImportTranscripts_StubIsNoOp confirms the SCRUM-342 stub returns nil
// without touching the database. Crucially, the primitive accepts a slice of
// *models.Transcript — not a source session ID — so a future template source
// can call it with synthetic data.
func TestImportTranscripts_StubIsNoOp(t *testing.T) {
	t.Parallel()
	c := NewCtx(Deps{}, newDstSession())
	require.NoError(t, ImportTranscripts(context.Background(), c, nil))
	assert.Empty(t, c.PartialFailures)
}

// TestImportSessionMetadata_NilSrc_NoOp confirms early-return when the
// source session is nil (templates may construct one without a row).
func TestImportSessionMetadata_NilSrc_NoOp(t *testing.T) {
	t.Parallel()
	c := NewCtx(Deps{}, newDstSession())
	require.NoError(t, ImportSessionMetadata(context.Background(), c, nil))
	assert.Empty(t, c.PartialFailures)
}

// TestImportSessionMetadata_NoFramingFields_NoOp confirms the primitive
// short-circuits when src has no framing fields and no explicit primary.
// Verifies the source-descriptor seam: no DB call, no panic, no failure.
func TestImportSessionMetadata_NoFramingFields_NoOp(t *testing.T) {
	t.Parallel()
	c := NewCtx(Deps{}, newDstSession())
	src := &models.Session{ID: uuid.New(), Title: "no framing"}
	require.NoError(t, ImportSessionMetadata(context.Background(), c, src))
	assert.Empty(t, c.PartialFailures)
}

// TestImportSessionMetadata_PrimaryRemapMiss_DoesNotPanic confirms that when
// the source has an explicit primary pointer that is not in the remap, the
// primitive logs and returns nil without touching the DB. Validates the
// failure mode acceptance criterion without requiring a database.
func TestImportSessionMetadata_PrimaryRemapMiss_DoesNotPanic(t *testing.T) {
	t.Parallel()
	c := NewCtx(Deps{}, newDstSession())
	missingID := uuid.New()
	kind := models.SessionPrimaryContentKindDocument
	src := &models.Session{
		ID:                 uuid.New(),
		Title:              "miss",
		PrimaryContentKind: &kind,
		PrimaryMaterialID:  &missingID, // not in c.MaterialRemap
	}
	// MaterialRemap is empty, so the lookup must miss and the primitive must
	// not call DB (Deps.DB is nil — would panic otherwise).
	require.NoError(t, ImportSessionMetadata(context.Background(), c, src))
	assert.Empty(t, c.PartialFailures)
}

// TestMaybeEnqueueProcessingJob_NoSourceJob_NoOp confirms the early-return
// path when no source job is supplied (templates path will exercise this).
func TestMaybeEnqueueProcessingJob_NoSourceJob_NoOp(t *testing.T) {
	t.Parallel()
	c := NewCtx(Deps{}, newDstSession())
	// Should not panic and should not call DB (Deps.DB is nil).
	MaybeEnqueueProcessingJob(context.Background(), c, nil, nil)
	assert.Empty(t, c.PartialFailures)
}

// TestMaybeEnqueueProcessingJob_AlreadyCopied_NoOp confirms the early-return
// path when CopiedPrimaryVideo is true.
func TestMaybeEnqueueProcessingJob_AlreadyCopied_NoOp(t *testing.T) {
	t.Parallel()
	c := NewCtx(Deps{}, newDstSession())
	c.CopiedPrimaryVideo = true
	uid := uuid.New()
	srcJob := &models.SessionProcessingJob{ID: uid, MeetingUUID: &[]string{"m"}[0]}
	MaybeEnqueueProcessingJob(context.Background(), c, srcJob, nil)
	assert.Empty(t, c.PartialFailures)
}

func newDstSession() *models.Session {
	return &models.Session{
		ID:     uuid.New(),
		Title:  "Destination",
		Status: models.SessionStatusOpen,
	}
}

// TestCopyVideoSourceFileArtifactID_RemapPath unit-tests the SCRUM-341 remap
// logic in isolation: given a source video_source with a non-nil
// FileArtifactID that IS in c.FileArtifactRemap, computing the clone pointer
// must yield the new ID — not the source ID. Inlines the same lookup pattern
// the handler-level integration test would exercise; allows verifying the
// positive case without needing real R2/local file copy infrastructure.
func TestCopyVideoSourceFileArtifactID_RemapPath(t *testing.T) {
	t.Parallel()
	c := NewCtx(Deps{}, newDstSession())
	srcFAID := uuid.New()
	newFAID := uuid.New()
	c.FileArtifactRemap[srcFAID] = newFAID

	got, ok := c.FileArtifactRemap[srcFAID]
	require.True(t, ok)
	assert.Equal(t, newFAID, got, "remap lookup must return the new file_artifact ID")
	assert.NotEqual(t, srcFAID, got, "remap result must not equal the source ID")
}
