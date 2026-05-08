package handlers

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SCRUM-342: cloned session inherits canonical transcript and segment anchors.

func TestCopySession_Transcripts_Copied(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	srcSessionID, creator := seedSessionForMetadataCopy(t, h)

	// Seed two transcripts on the source — different sources to satisfy
	// UNIQUE(session_id, source). Whisper + zoom paths cover the multi-row case.
	rawZoom := "Hello from zoom"
	rawWhisper := "Hello from whisper"
	srcZoom := &models.Transcript{
		ID:        uuid.New(),
		SessionID: srcSessionID,
		Source:    "zoom",
		Status:    models.TranscriptStatusReady,
		RawText:   &rawZoom,
	}
	srcWhisper := &models.Transcript{
		ID:        uuid.New(),
		SessionID: srcSessionID,
		Source:    "whisper",
		Status:    models.TranscriptStatusReady,
		RawText:   &rawWhisper,
	}
	require.NoError(t, h.DB.CreateTranscript(ctx, srcZoom))
	require.NoError(t, h.DB.CreateTranscript(ctx, srcWhisper))

	dstID := postCopy(t, h, srcSessionID, creator)

	dstTranscripts, err := h.DB.ListTranscriptsBySessionID(ctx, dstID)
	require.NoError(t, err)
	require.Len(t, dstTranscripts, 2, "clone must inherit both transcripts")

	bySource := map[string]*models.Transcript{}
	for _, t := range dstTranscripts {
		bySource[t.Source] = t
	}

	zoomDst := bySource["zoom"]
	require.NotNil(t, zoomDst, "zoom transcript missing on clone")
	assert.NotEqual(t, srcZoom.ID, zoomDst.ID, "transcript ID must be new on clone")
	assert.Equal(t, dstID, zoomDst.SessionID)
	require.NotNil(t, zoomDst.RawText)
	assert.Equal(t, rawZoom, *zoomDst.RawText)

	whisperDst := bySource["whisper"]
	require.NotNil(t, whisperDst, "whisper transcript missing on clone")
	assert.NotEqual(t, srcWhisper.ID, whisperDst.ID)
	assert.Equal(t, dstID, whisperDst.SessionID)
}

func TestCopySession_TranscriptSegments_Remapped(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	srcSessionID, creator := seedSessionForMetadataCopy(t, h)

	srcTranscript := &models.Transcript{
		ID:        uuid.New(),
		SessionID: srcSessionID,
		Source:    "whisper",
		Status:    models.TranscriptStatusReady,
	}
	require.NoError(t, h.DB.CreateTranscript(ctx, srcTranscript))

	srcSegments := []models.TranscriptSegmentRow{
		{TranscriptID: srcTranscript.ID, SessionID: srcSessionID, Idx: 0, StartMs: 0, EndMs: 1000, Text: "segment one"},
		{TranscriptID: srcTranscript.ID, SessionID: srcSessionID, Idx: 1, StartMs: 1000, EndMs: 2000, Text: "segment two"},
	}
	require.NoError(t, h.DB.InsertTranscriptSegments(ctx, srcTranscript.ID, srcSessionID, srcSegments))

	dstID := postCopy(t, h, srcSessionID, creator)

	dstTranscripts, err := h.DB.ListTranscriptsBySessionID(ctx, dstID)
	require.NoError(t, err)
	require.Len(t, dstTranscripts, 1)
	dstTranscript := dstTranscripts[0]
	assert.NotEqual(t, srcTranscript.ID, dstTranscript.ID, "transcript ID must be new")

	dstSegments, err := h.DB.ListSegmentsByTranscriptID(ctx, dstTranscript.ID)
	require.NoError(t, err)
	require.Len(t, dstSegments, 2, "all segments must be copied")
	for _, s := range dstSegments {
		assert.Equal(t, dstTranscript.ID, s.TranscriptID, "segment.transcript_id must point at the new transcript on the clone")
		assert.Equal(t, dstID, s.SessionID, "segment.session_id must equal the clone session")
	}

	// The source transcript must still own the original segments.
	srcDstSegments, err := h.DB.ListSegmentsByTranscriptID(ctx, srcTranscript.ID)
	require.NoError(t, err)
	require.Len(t, srcDstSegments, 2, "source segments must be unaffected")
}

func TestCopySession_NoTranscripts_NoOp(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	srcSessionID, creator := seedSessionForMetadataCopy(t, h)
	// No transcripts row seeded — Zoom-only path uses video_sources transcript_text.

	dstID := postCopy(t, h, srcSessionID, creator)

	dstTranscripts, err := h.DB.ListTranscriptsBySessionID(ctx, dstID)
	require.NoError(t, err)
	assert.Empty(t, dstTranscripts, "clone has no transcripts when source had none")
}
