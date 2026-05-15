package handlers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingStorage is a fake storage.Interface that records every Delete and
// DeletePrefix call so the SCRUM-406 cascade test can assert the canonical
// deletion order (file_artifacts.storage_key per-row + prefix sweep).
type recordingStorage struct {
	mu          sync.Mutex
	objects     map[string][]byte
	deletes     []string
	prefixes    []string
}

func newRecordingStorage() *recordingStorage {
	return &recordingStorage{objects: map[string][]byte{}}
}

func (s *recordingStorage) put(key string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = append([]byte(nil), data...)
}

func (s *recordingStorage) PresignPut(_ context.Context, _ string, _ time.Duration, _ string) (string, map[string]string, error) {
	return "", nil, nil
}
func (s *recordingStorage) PresignGet(_ context.Context, key string, _ time.Duration) (string, error) {
	return "presigned:" + key, nil
}
func (s *recordingStorage) Put(_ context.Context, key string, r io.Reader, _ string, _ int64) (string, int64, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", 0, err
	}
	s.put(key, data)
	return "etag", int64(len(data)), nil
}
func (s *recordingStorage) Get(_ context.Context, key string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.objects[key]
	if !ok {
		return nil, fmt.Errorf("not found: %s", key)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}
func (s *recordingStorage) Head(_ context.Context, key string) (bool, int64, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.objects[key]
	if !ok {
		return false, 0, "", nil
	}
	return true, int64(len(data)), "application/octet-stream", nil
}
func (s *recordingStorage) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deletes = append(s.deletes, key)
	delete(s.objects, key)
	return nil
}
func (s *recordingStorage) CopyObject(_ context.Context, _, _ string) error { return nil }
func (s *recordingStorage) DeletePrefix(_ context.Context, prefix string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prefixes = append(s.prefixes, prefix)
	n := 0
	for k := range s.objects {
		if strings.HasPrefix(k, prefix) {
			delete(s.objects, k)
			n++
		}
	}
	return n, nil
}

func (s *recordingStorage) deleteCalls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]string(nil), s.deletes...)
	sort.Strings(out)
	return out
}

func (s *recordingStorage) prefixCalls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.prefixes...)
}

func (s *recordingStorage) remainingKeys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.objects))
	for k := range s.objects {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestDeleteSession_CascadeAndR2Cleanup is the SCRUM-406 integration test:
// build a session populated with every reachable kind of child row + R2 blob,
// call DeleteSession through the admin path, and assert no orphans survive.
func TestDeleteSession_CascadeAndR2Cleanup(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	storage := newRecordingStorage()
	h.Storage = storage

	ctx := context.Background()
	session := createTestSessionForHandlers(t, h.DB, "cascade-delete-fixture")

	// --- Seed: artifact + materials + video_sources + transcripts +
	// transcript_segments + session_chunks + session_speaker_aliases +
	// file_artifacts (one inside the standard prefix, one OUTSIDE it).
	artifactID := uuid.New()
	_, err := h.DB.Pool.Exec(ctx, `
		INSERT INTO artifacts (id, session_id, title, status, created_at, updated_at)
		VALUES ($1, $2, 'fixture artifact', 'ready', now(), now())
	`, artifactID, session.ID)
	require.NoError(t, err)

	videoSourceID := uuid.New()
	_, err = h.DB.Pool.Exec(ctx, `
		INSERT INTO video_sources (id, artifact_id, session_id, provider, video_url, source_type)
		VALUES ($1, $2, $3, 'zoom', 'https://example.com/v.mp4', 'upload')
	`, videoSourceID, artifactID, session.ID)
	require.NoError(t, err)

	transcriptID := uuid.New()
	_, err = h.DB.Pool.Exec(ctx, `
		INSERT INTO transcripts (id, session_id, source, status)
		VALUES ($1, $2, 'zoom', 'ready')
	`, transcriptID, session.ID)
	require.NoError(t, err)
	_, err = h.DB.Pool.Exec(ctx, `
		INSERT INTO transcript_segments (transcript_id, session_id, idx, start_ms, end_ms, text, speaker_label)
		VALUES ($1, $2, 1, 0, 1000, 'hello', 'Speaker 0')
	`, transcriptID, session.ID)
	require.NoError(t, err)

	_, err = h.DB.Pool.Exec(ctx, `
		INSERT INTO session_chunks (session_id, source_type, source_id, chunk_idx, text, content_hash)
		VALUES ($1, 'transcript', $2, 0, 'chunk-text', 'fixture-hash-0')
	`, session.ID, transcriptID)
	require.NoError(t, err)

	// SCRUM-404 table — confirm it cascades alongside everything else.
	_, err = h.DB.Pool.Exec(ctx, `
		INSERT INTO session_speaker_aliases (session_id, canonical_person_id, source_label, canonical_display_name)
		VALUES ($1, $2, 'Speaker 0', 'Alice')
	`, session.ID, uuid.New())
	require.NoError(t, err)

	// Two file_artifacts: one keyed inside the standard prefix, one OUTSIDE.
	// Both must be R2-deleted explicitly by the handler.
	inPrefixKey := "sessions/" + session.ID.String() + "/uploads/in-prefix.bin"
	outOfPrefixKey := "legacy/" + session.ID.String() + "/copy/out-of-prefix.bin"
	storage.put(inPrefixKey, []byte("in"))
	storage.put(outOfPrefixKey, []byte("out"))

	// Plus one bare-prefix blob that is NOT referenced by file_artifacts — the
	// prefix sweep must still clean it up.
	rawUploadKey := "sessions/" + session.ID.String() + "/uploads/raw.bin"
	storage.put(rawUploadKey, []byte("raw"))

	for _, fa := range []struct {
		id  uuid.UUID
		key string
	}{
		{uuid.New(), inPrefixKey},
		{uuid.New(), outOfPrefixKey},
	} {
		_, err = h.DB.Pool.Exec(ctx, `
			INSERT INTO file_artifacts (id, session_id, kind, content_type,
			                            storage_provider, storage_bucket, storage_key,
			                            status, created_at, updated_at)
			VALUES ($1, $2, 'video', 'video/mp4', 'r2', 'test-bucket', $3, 'ready', now(), now())
		`, fa.id, session.ID, fa.key)
		require.NoError(t, err)
	}

	// --- Act: call DeleteSession through the handler with an admin user.
	adminUser := &models.User{
		ID:         uuid.New(),
		Email:      "admin@example.com",
		GlobalRole: models.GlobalRoleAdmin,
		Status:     models.UserStatusActive,
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/"+session.ID.String(), nil)
	req = req.WithContext(context.WithValue(req.Context(), userContextKey, adminUser))
	w := httptest.NewRecorder()
	h.DeleteSession(w, req)
	require.Equal(t, http.StatusNoContent, w.Code, "delete must return 204 no content")

	// --- Assert: explicit per-row R2 delete called for every file_artifact key.
	deletes := storage.deleteCalls()
	assert.Contains(t, deletes, inPrefixKey, "file_artifact storage_key inside the prefix must be Delete()'d explicitly")
	assert.Contains(t, deletes, outOfPrefixKey, "file_artifact storage_key OUTSIDE the prefix must be Delete()'d explicitly — the prefix sweep would miss it")

	// --- Assert: prefix sweep ran for the session's standard prefix.
	prefixes := storage.prefixCalls()
	require.Len(t, prefixes, 1, "exactly one DeletePrefix call expected")
	assert.Equal(t, "sessions/"+session.ID.String()+"/", prefixes[0])

	// --- Assert: no R2 objects remain for this session.
	remaining := storage.remainingKeys()
	for _, k := range remaining {
		assert.NotContains(t, k, session.ID.String(), "no R2 keys referencing the session should remain (key=%s)", k)
	}
	// The out-of-prefix key is gone because of the explicit per-row delete.
	for _, k := range remaining {
		assert.NotEqual(t, outOfPrefixKey, k)
	}

	// --- Assert: every DB row that referenced the session is gone.
	type row struct {
		table string
		col   string
	}
	cascadingRows := []row{
		{"artifacts", "session_id"},
		{"video_sources", "session_id"},
		{"transcripts", "session_id"},
		{"transcript_segments", "session_id"},
		{"session_chunks", "session_id"},
		{"session_speaker_aliases", "session_id"},
		{"file_artifacts", "session_id"},
	}
	for _, rr := range cascadingRows {
		var count int
		err := h.DB.Pool.QueryRow(ctx,
			"SELECT count(*) FROM "+rr.table+" WHERE "+rr.col+" = $1",
			session.ID,
		).Scan(&count)
		require.NoError(t, err, "count(*) on %s", rr.table)
		assert.Equal(t, 0, count, "no rows in %s should reference the deleted session", rr.table)
	}

	// And the session itself.
	var sessionCount int
	err = h.DB.Pool.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE id = $1", session.ID).Scan(&sessionCount)
	require.NoError(t, err)
	assert.Equal(t, 0, sessionCount, "session row itself must be gone")
}

// TestDeleteSession_NoStorageStillCascadesDB checks that the DB-side cleanup
// still runs when Storage is nil (single-server / no-R2 deployments).
func TestDeleteSession_NoStorageStillCascadesDB(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	require.Nil(t, h.Storage, "Storage is nil for this case")

	ctx := context.Background()
	session := createTestSessionForHandlers(t, h.DB, "no-storage-cascade-fixture")

	artifactID := uuid.New()
	_, err := h.DB.Pool.Exec(ctx, `
		INSERT INTO artifacts (id, session_id, title, status, created_at, updated_at)
		VALUES ($1, $2, 'a', 'ready', now(), now())
	`, artifactID, session.ID)
	require.NoError(t, err)

	adminUser := &models.User{
		ID:         uuid.New(),
		Email:      "admin@example.com",
		GlobalRole: models.GlobalRoleAdmin,
		Status:     models.UserStatusActive,
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/"+session.ID.String(), nil)
	req = req.WithContext(context.WithValue(req.Context(), userContextKey, adminUser))
	w := httptest.NewRecorder()
	h.DeleteSession(w, req)
	require.Equal(t, http.StatusNoContent, w.Code)

	var sessionCount, artifactCount int
	require.NoError(t, h.DB.Pool.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE id = $1", session.ID).Scan(&sessionCount))
	require.NoError(t, h.DB.Pool.QueryRow(ctx, "SELECT count(*) FROM artifacts WHERE session_id = $1", session.ID).Scan(&artifactCount))
	assert.Equal(t, 0, sessionCount)
	assert.Equal(t, 0, artifactCount)
}
