package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SCRUM-344: CopySession reports partial failures and rolls back on
// critical errors. Verifies the additive partial_failures field on
// CopySessionResponse and that the field is omitted on a clean copy.

func TestCopySession_NoFailures_PartialFailuresEmpty(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	srcSessionID, creator := seedSessionForMetadataCopy(t, h)

	copyReq := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/api/sessions/%s/copy", srcSessionID.String()),
		bytes.NewReader([]byte(`{}`)),
	)
	copyReq.Header.Set("Content-Type", "application/json")
	addUserSessionCookieFor(t, h, copyReq, creator)
	w := httptest.NewRecorder()
	h.RequireAuth(h.CopySession)(w, copyReq)
	require.Equal(t, http.StatusCreated, w.Code, "happy path copy: %s", w.Body.String())

	// Decode into a generic map so we can verify partial_failures is omitted
	// (not just empty) when there are no failures — that's the JSON contract
	// existing API consumers depend on.
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	_, present := raw["partial_failures"]
	assert.False(t, present, "partial_failures must be omitted on a clean copy (omitempty)")

	// Also decode into the typed response and verify the slice is nil/empty.
	var resp CopySessionResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp.PartialFailures)
}

func TestCopySession_PartialFailure_FileArtifactReported(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TALKBACK_UPLOAD_ROOT", root)

	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	srcSessionID, creator := seedSessionForMetadataCopy(t, h)

	// Seed a file_artifact row whose storage_key points at a path that does
	// not exist on disk. ImportFileArtifacts will hit a copy error, log it,
	// and record "file_artifacts" in partial_failures. The HTTP response is
	// still 201 because per-row file_artifact failures are non-fatal.
	missing := &models.FileArtifact{
		ID:              uuid.New(),
		SessionID:       &srcSessionID,
		Kind:            "video",
		ContentType:     "video/mp4",
		StorageProvider: "local",
		StorageBucket:   "local",
		StorageKey:      filepath.ToSlash(filepath.Join(storage.SessionStorageRoot, srcSessionID.String(), "videos", "missing.mp4")),
		Status:          models.FileArtifactStatusReady,
	}
	require.NoError(t, h.DB.CreateFileArtifact(ctx, missing))
	// Sanity: confirm the source file really does not exist.
	abs := filepath.Join(root, filepath.FromSlash(missing.StorageKey))
	_, err := os.Stat(abs)
	require.Error(t, err, "test prerequisite: source file must not exist")

	copyReq := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/api/sessions/%s/copy", srcSessionID.String()),
		bytes.NewReader([]byte(`{}`)),
	)
	copyReq.Header.Set("Content-Type", "application/json")
	addUserSessionCookieFor(t, h, copyReq, creator)
	w := httptest.NewRecorder()
	h.RequireAuth(h.CopySession)(w, copyReq)
	require.Equal(t, http.StatusCreated, w.Code, "non-fatal partial failure must still return 201; got %s", w.Body.String())

	var resp CopySessionResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp.PartialFailures, "file_artifacts", "partial_failures must surface the affected category")
}
