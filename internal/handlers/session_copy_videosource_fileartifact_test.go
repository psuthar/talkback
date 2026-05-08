package handlers

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SCRUM-341: regression test for "video_sources.file_artifact_id points to
// source session after copy". Migration 000046 added the column; the
// pre-SCRUM-339 CopySession was never updated to remap it. SCRUM-339
// extracted the body into ImportVideoSources but preserved the (broken)
// behavior. SCRUM-341 wires fileArtifactRemap[*vs.FileArtifactID] into
// copyVS.FileArtifactID.

func TestCopySession_VideoSourceFileArtifactID_NilWhenSourceNil(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	srcSessionID, creator := seedSessionForMetadataCopy(t, h)
	srcArtifacts, err := h.DB.GetArtifactsBySessionID(ctx, srcSessionID)
	require.NoError(t, err)
	require.NotEmpty(t, srcArtifacts)

	// Source video_source with FileArtifactID == nil (e.g. external embed,
	// no uploaded MP4). Clone must also have nil.
	src := newSeedVideoSource(srcArtifacts[0].ID, srcSessionID)
	require.Nil(t, src.FileArtifactID)
	require.NoError(t, h.DB.CreateVideoSource(ctx, src))

	dstID := postCopy(t, h, srcSessionID, creator)
	dstVS, err := h.DB.GetVideoSourcesBySessionID(ctx, dstID)
	require.NoError(t, err)
	require.Len(t, dstVS, 1)
	assert.Nil(t, dstVS[0].FileArtifactID, "clone must have nil file_artifact_id when source had nil")
}

func TestCopySession_VideoSourceFileArtifactID_NilWhenRemapMisses(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	srcSessionID, creator := seedSessionForMetadataCopy(t, h)
	srcArtifacts, err := h.DB.GetArtifactsBySessionID(ctx, srcSessionID)
	require.NoError(t, err)
	require.NotEmpty(t, srcArtifacts)

	// Seed a non-primary file_artifact tied to the source session and
	// reference it from the source video_source. Because SCRUM-343 (which
	// copies all file_artifacts) has not landed yet, only the primary FA is
	// copied today — the FileArtifactRemap will not contain this id, so the
	// clone's FileArtifactID must be nil (NOT the source's pointer).
	srcFA := &models.FileArtifact{
		ID:              uuid.New(),
		SessionID:       &srcSessionID,
		Kind:            "video",
		ContentType:     "video/mp4",
		StorageProvider: "local",
		StorageBucket:   "local",
		StorageKey:      "sessions/" + srcSessionID.String() + "/videos/extra.mp4",
		Status:          models.FileArtifactStatusReady,
	}
	require.NoError(t, h.DB.CreateFileArtifact(ctx, srcFA))

	src := newSeedVideoSource(srcArtifacts[0].ID, srcSessionID)
	src.FileArtifactID = &srcFA.ID
	require.NoError(t, h.DB.CreateVideoSource(ctx, src))

	dstID := postCopy(t, h, srcSessionID, creator)
	dstVS, err := h.DB.GetVideoSourcesBySessionID(ctx, dstID)
	require.NoError(t, err)
	require.Len(t, dstVS, 1)
	// Critical: must NOT carry the source's FileArtifactID forward.
	if dstVS[0].FileArtifactID != nil {
		assert.NotEqual(t, srcFA.ID, *dstVS[0].FileArtifactID, "clone must never reference the source session's file_artifact_id (security/ACL concern)")
	}
}

func newSeedVideoSource(artifactID, sessionID uuid.UUID) *models.VideoSource {
	role := models.VideoRoleSecondary
	return &models.VideoSource{
		ID:               uuid.New(),
		ArtifactID:       artifactID,
		SessionID:        sessionID,
		Provider:         "other",
		PlaybackMode:     "embed",
		SourceType:       "embed_url",
		TranscriptStatus: models.VideoTranscriptStatusMissing,
		VideoRole:        &role,
	}
}
