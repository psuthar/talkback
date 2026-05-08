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

// TestImportSessionMetadata_StubIsNoOp confirms the SCRUM-340 stub returns
// nil without touching the database.
func TestImportSessionMetadata_StubIsNoOp(t *testing.T) {
	t.Parallel()
	c := NewCtx(Deps{}, newDstSession())
	require.NoError(t, ImportSessionMetadata(context.Background(), c, nil))
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
