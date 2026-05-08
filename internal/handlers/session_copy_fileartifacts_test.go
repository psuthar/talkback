package handlers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SCRUM-343: cloned session inherits all uploaded file_artifacts (not just
// primary). Uses a temporary upload root + real local files so the storage
// copy path runs end-to-end without needing a fake R2 backend.

func TestCopySession_NonPrimaryFileArtifacts_Copied(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	t.Setenv("TALKBACK_UPLOAD_ROOT", root)

	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	srcSessionID, creator := seedSessionForMetadataCopy(t, h)

	primaryFA := seedLocalFileArtifact(t, h, root, srcSessionID, "primary.mp4", []byte("primary-content"))
	extraFA := seedLocalFileArtifact(t, h, root, srcSessionID, "extra.mp4", []byte("extra-content-bytes"))
	require.NoError(t, h.DB.SetSessionPrimaryVideoArtifact(ctx, srcSessionID, &primaryFA.ID))

	dstID := postCopy(t, h, srcSessionID, creator)

	dstFAs, err := h.DB.ListFileArtifactsBySessionID(ctx, dstID)
	require.NoError(t, err)
	require.Len(t, dstFAs, 2, "clone must inherit both primary and non-primary file_artifacts")

	// Storage-rekey assertion: every dst storage_key must reference the clone
	// session, not the source. This is the SCRUM-345 sweep's spirit applied
	// here without a fake storage.Interface.
	srcIDStr := srcSessionID.String()
	dstIDStr := dstID.String()
	for _, fa := range dstFAs {
		assert.NotContains(t, fa.StorageKey, srcIDStr, "dst FA storage_key must not contain source session id")
		assert.Contains(t, fa.StorageKey, dstIDStr, "dst FA storage_key must contain clone session id")
		require.NotNil(t, fa.SessionID)
		assert.Equal(t, dstID, *fa.SessionID)
		// Each FA's source ID must not equal either of the source FA IDs.
		assert.NotEqual(t, primaryFA.ID, fa.ID)
		assert.NotEqual(t, extraFA.ID, fa.ID)
		// File should exist on disk under the clone path.
		abs := filepath.Join(root, filepath.FromSlash(fa.StorageKey))
		_, err := os.Stat(abs)
		assert.NoError(t, err, "clone file must exist at %s", abs)
	}

	// Primary pointer on clone must resolve to a clone-owned FA.
	dstSession, err := h.DB.GetSession(ctx, dstID)
	require.NoError(t, err)
	require.NotNil(t, dstSession.PrimaryVideoArtifactID)
	assert.NotEqual(t, primaryFA.ID, *dstSession.PrimaryVideoArtifactID, "primary_video_artifact_id must remap to clone FA")
}

func TestCopySession_FileArtifact_PerRowFailure_LoggedNotFatal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	t.Setenv("TALKBACK_UPLOAD_ROOT", root)

	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	srcSessionID, creator := seedSessionForMetadataCopy(t, h)

	// One real file, one with a missing file (storage_key points at a path
	// that doesn't exist). The missing one must be skipped without aborting
	// the copy; the valid one must still be copied.
	good := seedLocalFileArtifact(t, h, root, srcSessionID, "good.mp4", []byte("ok"))
	missing := &models.FileArtifact{
		ID:              uuid.New(),
		SessionID:       &srcSessionID,
		Kind:            "video",
		ContentType:     "video/mp4",
		StorageProvider: "local",
		StorageBucket:   "local",
		StorageKey:      "sessions/" + srcSessionID.String() + "/videos/missing.mp4",
		Status:          models.FileArtifactStatusReady,
	}
	require.NoError(t, h.DB.CreateFileArtifact(ctx, missing))

	dstID := postCopy(t, h, srcSessionID, creator)

	dstFAs, err := h.DB.ListFileArtifactsBySessionID(ctx, dstID)
	require.NoError(t, err)
	require.Len(t, dstFAs, 1, "only the valid FA should be copied")
	assert.NotEqual(t, good.ID, dstFAs[0].ID)
	assert.NotEqual(t, missing.ID, dstFAs[0].ID)
}

func seedLocalFileArtifact(t *testing.T, h *Handlers, root string, srcSessionID uuid.UUID, baseName string, body []byte) *models.FileArtifact {
	t.Helper()
	// File written under <root>/sessions/<src>/videos/<base>; storage_key is
	// the relative path used by ImportFileArtifacts to locate the source file.
	relKey := filepath.ToSlash(filepath.Join(storage.SessionStorageRoot, srcSessionID.String(), "videos", baseName))
	abs := filepath.Join(root, filepath.FromSlash(relKey))
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(abs, body, 0o644))
	size := int64(len(body))
	filename := baseName
	fa := &models.FileArtifact{
		ID:              uuid.New(),
		SessionID:       &srcSessionID,
		Kind:            "video",
		Filename:        &filename,
		ContentType:     "video/mp4",
		SizeBytes:       &size,
		StorageProvider: "local",
		StorageBucket:   "local",
		StorageKey:      relKey,
		Status:          models.FileArtifactStatusReady,
	}
	require.NoError(t, h.DB.CreateFileArtifact(context.Background(), fa))
	// Sanity: produce a fmt.Stringer-friendly value to stop unused import warnings if name shrinks.
	_ = fmt.Sprintf("%s", strings.TrimSpace(baseName))
	return fa
}
