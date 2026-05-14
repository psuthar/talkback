package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetSlidesStatus_LocalPath_SCRUM443 covers the marker-precedence matrix on the
// local-filesystem path: manifest → ready, failed → failed, processing-fresh → processing,
// processing-stale → auto-fails (writes failed.json, deletes processing.json, returns "failed"),
// no-markers → processing (legacy compat).
func TestGetSlidesStatus_LocalPath_SCRUM443(t *testing.T) {
	// Cannot t.Parallel() — uses t.Setenv(TALKBACK_UPLOAD_ROOT, ...) per-subtest.
	ctx := context.Background()

	// Each subtest uses an isolated TALKBACK_UPLOAD_ROOT so writes/reads don't collide.
	mkMaterial := func(storageURL string) *models.Material {
		return &models.Material{
			ID:              uuid.New(),
			Filename:        filepath.Base(storageURL),
			StorageProvider: "local",
			StorageURL:      storageURL,
			Kind:            string(models.MaterialKindDocument),
		}
	}

	t.Run("manifest present -> ready", func(t *testing.T) {
		t.Setenv("TALKBACK_UPLOAD_ROOT", t.TempDir())
		storageURL := "sessions/" + uuid.New().String() + "/data/uploads/deck.pptx"
		mustMkdir(t, filepath.Join(storage.UploadRoot(), storageURL+"_slides"))
		mustWriteFile(t, filepath.Join(storage.UploadRoot(), storageURL+"_slides", "manifest.json"), []byte(`{"slides":[]}`))

		h := &Handlers{}
		got := h.GetSlidesStatus(ctx, mkMaterial(storageURL))
		assert.Equal(t, "ready", got)
	})

	t.Run("failed.json present -> failed", func(t *testing.T) {
		t.Setenv("TALKBACK_UPLOAD_ROOT", t.TempDir())
		storageURL := "sessions/" + uuid.New().String() + "/data/uploads/deck.pptx"
		mustMkdir(t, filepath.Join(storage.UploadRoot(), storageURL+"_slides"))
		mustWriteFile(t, filepath.Join(storage.UploadRoot(), storageURL+"_slides", "failed.json"), []byte(`{"status":"failed","error":"prior failure"}`))

		h := &Handlers{}
		got := h.GetSlidesStatus(ctx, mkMaterial(storageURL))
		assert.Equal(t, "failed", got)
	})

	t.Run("processing.json fresh -> processing", func(t *testing.T) {
		t.Setenv("TALKBACK_UPLOAD_ROOT", t.TempDir())
		storageURL := "sessions/" + uuid.New().String() + "/data/uploads/deck.pptx"
		mustMkdir(t, filepath.Join(storage.UploadRoot(), storageURL+"_slides"))
		fresh := time.Now().UTC().Add(-1 * time.Minute).Format(time.RFC3339)
		payload := mustMarshal(t, slidesProcessingMarker{StartedAt: fresh, MarkerVersion: 1})
		mustWriteFile(t, filepath.Join(storage.UploadRoot(), storageURL+"_slides", "processing.json"), payload)

		h := &Handlers{}
		got := h.GetSlidesStatus(ctx, mkMaterial(storageURL))
		assert.Equal(t, "processing", got)

		// Fresh marker must NOT be auto-failed.
		_, err := os.Stat(slidesFailureMarkerPathFromStorageURL(storageURL))
		assert.True(t, errors.Is(err, os.ErrNotExist), "fresh marker should not produce failed.json")
		_, err = os.Stat(slidesProcessingMarkerPathFromStorageURL(storageURL))
		assert.NoError(t, err, "fresh marker should still exist after read")
	})

	t.Run("processing.json stale -> auto-fails (writes failed.json, deletes processing.json, returns failed)", func(t *testing.T) {
		t.Setenv("TALKBACK_UPLOAD_ROOT", t.TempDir())
		storageURL := "sessions/" + uuid.New().String() + "/data/uploads/deck.pptx"
		mustMkdir(t, filepath.Join(storage.UploadRoot(), storageURL+"_slides"))
		stale := time.Now().UTC().Add(-(SlidesStaleProcessingThreshold + time.Minute)).Format(time.RFC3339)
		payload := mustMarshal(t, slidesProcessingMarker{StartedAt: stale, MarkerVersion: 1})
		mustWriteFile(t, filepath.Join(storage.UploadRoot(), storageURL+"_slides", "processing.json"), payload)

		h := &Handlers{}
		got := h.GetSlidesStatus(ctx, mkMaterial(storageURL))
		assert.Equal(t, "failed", got)

		// Side effects: failed.json written, processing.json removed.
		failedData, err := os.ReadFile(slidesFailureMarkerPathFromStorageURL(storageURL))
		require.NoError(t, err, "stale marker should produce failed.json")
		var failedPayload map[string]string
		require.NoError(t, json.Unmarshal(failedData, &failedPayload))
		assert.Equal(t, "failed", failedPayload["status"])
		assert.Contains(t, failedPayload["error"], "stranded")

		_, err = os.Stat(slidesProcessingMarkerPathFromStorageURL(storageURL))
		assert.True(t, errors.Is(err, os.ErrNotExist), "stale marker should be deleted after auto-fail")
	})

	t.Run("no markers -> processing (legacy compat)", func(t *testing.T) {
		t.Setenv("TALKBACK_UPLOAD_ROOT", t.TempDir())
		storageURL := "sessions/" + uuid.New().String() + "/data/uploads/deck.pptx"
		// No _slides dir at all.

		h := &Handlers{}
		got := h.GetSlidesStatus(ctx, mkMaterial(storageURL))
		assert.Equal(t, "processing", got)
	})

	t.Run("processing.json unparseable -> processing (don't auto-fail on bad JSON)", func(t *testing.T) {
		t.Setenv("TALKBACK_UPLOAD_ROOT", t.TempDir())
		storageURL := "sessions/" + uuid.New().String() + "/data/uploads/deck.pptx"
		mustMkdir(t, filepath.Join(storage.UploadRoot(), storageURL+"_slides"))
		mustWriteFile(t, filepath.Join(storage.UploadRoot(), storageURL+"_slides", "processing.json"), []byte("not-json"))

		h := &Handlers{}
		got := h.GetSlidesStatus(ctx, mkMaterial(storageURL))
		assert.Equal(t, "processing", got, "unparseable marker must not auto-fail (conservative)")
	})
}

// TestGetSlidesStatus_R2Path_SCRUM443 mirrors the local-path matrix against an in-memory mock
// Storage backend that supports Put/Head/Get/Delete.
func TestGetSlidesStatus_R2Path_SCRUM443(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mkMaterial := func(storageKey string) *models.Material {
		return &models.Material{
			ID:              uuid.New(),
			Filename:        filepath.Base(storageKey),
			StorageProvider: "r2",
			StorageKey:      storageKey,
			Kind:            string(models.MaterialKindDocument),
		}
	}

	t.Run("manifest present -> ready", func(t *testing.T) {
		ms := newSlidesStatusStorage()
		artifactKey := "sessions/" + uuid.New().String() + "/data/uploads/deck.pptx"
		ms.put(storage.SlidesManifestKeyFromArtifactKey(artifactKey), []byte(`{"slides":[]}`))

		h := &Handlers{Storage: ms}
		got := h.GetSlidesStatus(ctx, mkMaterial(artifactKey))
		assert.Equal(t, "ready", got)
	})

	t.Run("failed.json present -> failed", func(t *testing.T) {
		ms := newSlidesStatusStorage()
		artifactKey := "sessions/" + uuid.New().String() + "/data/uploads/deck.pptx"
		ms.put(slidesFailureMarkerKeyFromArtifactKey(artifactKey), []byte(`{"status":"failed","error":"prior"}`))

		h := &Handlers{Storage: ms}
		got := h.GetSlidesStatus(ctx, mkMaterial(artifactKey))
		assert.Equal(t, "failed", got)
	})

	t.Run("processing.json fresh -> processing", func(t *testing.T) {
		ms := newSlidesStatusStorage()
		artifactKey := "sessions/" + uuid.New().String() + "/data/uploads/deck.pptx"
		fresh := time.Now().UTC().Add(-1 * time.Minute).Format(time.RFC3339)
		payload := mustMarshal(t, slidesProcessingMarker{StartedAt: fresh, MarkerVersion: 1})
		ms.put(storage.SlidesProcessingKeyFromArtifactKey(artifactKey), payload)

		h := &Handlers{Storage: ms}
		got := h.GetSlidesStatus(ctx, mkMaterial(artifactKey))
		assert.Equal(t, "processing", got)

		assert.False(t, ms.has(slidesFailureMarkerKeyFromArtifactKey(artifactKey)), "fresh marker must not produce failed.json")
		assert.True(t, ms.has(storage.SlidesProcessingKeyFromArtifactKey(artifactKey)), "fresh marker should still exist after read")
	})

	t.Run("processing.json stale -> auto-fails", func(t *testing.T) {
		ms := newSlidesStatusStorage()
		artifactKey := "sessions/" + uuid.New().String() + "/data/uploads/deck.pptx"
		stale := time.Now().UTC().Add(-(SlidesStaleProcessingThreshold + time.Minute)).Format(time.RFC3339)
		payload := mustMarshal(t, slidesProcessingMarker{StartedAt: stale, MarkerVersion: 1})
		ms.put(storage.SlidesProcessingKeyFromArtifactKey(artifactKey), payload)

		h := &Handlers{Storage: ms}
		got := h.GetSlidesStatus(ctx, mkMaterial(artifactKey))
		assert.Equal(t, "failed", got)

		failedKey := slidesFailureMarkerKeyFromArtifactKey(artifactKey)
		failedData, ok := ms.get(failedKey)
		require.True(t, ok, "stale marker must produce failed.json")
		var failedPayload map[string]string
		require.NoError(t, json.Unmarshal(failedData, &failedPayload))
		assert.Equal(t, "failed", failedPayload["status"])
		assert.Contains(t, failedPayload["error"], "stranded")

		assert.False(t, ms.has(storage.SlidesProcessingKeyFromArtifactKey(artifactKey)), "stale marker must be deleted after auto-fail")
	})

	t.Run("no markers -> processing (legacy compat)", func(t *testing.T) {
		ms := newSlidesStatusStorage()
		artifactKey := "sessions/" + uuid.New().String() + "/data/uploads/deck.pptx"
		// Nothing written.

		h := &Handlers{Storage: ms}
		got := h.GetSlidesStatus(ctx, mkMaterial(artifactKey))
		assert.Equal(t, "processing", got)
	})
}

// TestSlidesProcessingMarkerHelpers_SCRUM443 covers the write/clear helpers used inside the goroutine
// entrypoints (tryGenerateAndStoreSlides + tryGenerateAndStoreSlidesLocal). These are deferred from
// the real entrypoints in production, but verifying the helpers in isolation guards the contract.
func TestSlidesProcessingMarkerHelpers_SCRUM443(t *testing.T) {
	// Cannot t.Parallel() — "local write then clear" uses t.Setenv.

	t.Run("local write then clear", func(t *testing.T) {
		t.Setenv("TALKBACK_UPLOAD_ROOT", t.TempDir())
		storageURL := "sessions/" + uuid.New().String() + "/data/uploads/deck.pptx"
		localPath := filepath.Join(storage.UploadRoot(), storageURL)
		mustMkdir(t, filepath.Dir(localPath))

		writeSlidesProcessingMarkerLocal(localPath)
		path := slidesProcessingMarkerPathFromStorageURL(storageURL)
		data, err := os.ReadFile(path)
		require.NoError(t, err, "processing.json must be written")
		var marker slidesProcessingMarker
		require.NoError(t, json.Unmarshal(data, &marker))
		assert.Equal(t, 1, marker.MarkerVersion)
		startedAt, err := time.Parse(time.RFC3339, marker.StartedAt)
		require.NoError(t, err)
		assert.WithinDuration(t, time.Now().UTC(), startedAt, 2*time.Second)

		clearSlidesProcessingMarkerLocal(localPath)
		_, err = os.Stat(path)
		assert.True(t, errors.Is(err, os.ErrNotExist), "processing.json must be deleted after clear")
	})

	t.Run("R2 write then clear", func(t *testing.T) {
		ms := newSlidesStatusStorage()
		h := &Handlers{Storage: ms}
		artifactKey := "sessions/" + uuid.New().String() + "/data/uploads/deck.pptx"
		key := storage.SlidesProcessingKeyFromArtifactKey(artifactKey)

		h.writeSlidesProcessingMarkerStorage(context.Background(), artifactKey)
		data, ok := ms.get(key)
		require.True(t, ok, "processing.json must be put on R2")
		var marker slidesProcessingMarker
		require.NoError(t, json.Unmarshal(data, &marker))
		assert.Equal(t, 1, marker.MarkerVersion)

		h.clearSlidesProcessingMarkerStorage(context.Background(), artifactKey)
		assert.False(t, ms.has(key), "processing.json must be deleted from R2 after clear")
	})

	t.Run("R2 write is a no-op with nil Storage or empty artifact key", func(t *testing.T) {
		h := &Handlers{Storage: nil}
		require.NotPanics(t, func() {
			h.writeSlidesProcessingMarkerStorage(context.Background(), "key")
			h.clearSlidesProcessingMarkerStorage(context.Background(), "key")
		})

		ms := newSlidesStatusStorage()
		h2 := &Handlers{Storage: ms}
		h2.writeSlidesProcessingMarkerStorage(context.Background(), "   ")
		assert.Equal(t, 0, ms.size(), "empty/whitespace artifact key should be a no-op")
	})
}

// slidesStatusStorage is an in-memory storage.Interface implementation for SCRUM-443 status tests.
// Supports Put/Head/Get/Delete with real semantics so the staleness auto-fail path is exercised
// end-to-end (Head sees the marker, Get returns its body, Delete removes it, Put writes failed.json).
type slidesStatusStorage struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func newSlidesStatusStorage() *slidesStatusStorage {
	return &slidesStatusStorage{objects: make(map[string][]byte)}
}

func (s *slidesStatusStorage) put(key string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = append([]byte(nil), data...)
}

func (s *slidesStatusStorage) get(key string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.objects[key]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), data...), true
}

func (s *slidesStatusStorage) has(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.objects[key]
	return ok
}

func (s *slidesStatusStorage) size() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.objects)
}

func (s *slidesStatusStorage) Put(_ context.Context, key string, r io.Reader, _ string, _ int64) (string, int64, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", 0, err
	}
	s.put(key, data)
	return "etag", int64(len(data)), nil
}

func (s *slidesStatusStorage) Get(_ context.Context, key string) (io.ReadCloser, error) {
	data, ok := s.get(key)
	if !ok {
		return nil, fmt.Errorf("not found: %s", key)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *slidesStatusStorage) Head(_ context.Context, key string) (bool, int64, string, error) {
	data, ok := s.get(key)
	if !ok {
		return false, 0, "", nil
	}
	return true, int64(len(data)), "application/json", nil
}

func (s *slidesStatusStorage) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, key)
	return nil
}

func (s *slidesStatusStorage) PresignPut(_ context.Context, _ string, _ time.Duration, _ string) (string, map[string]string, error) {
	return "", nil, nil
}
func (s *slidesStatusStorage) PresignGet(_ context.Context, _ string, _ time.Duration) (string, error) {
	return "", nil
}
func (s *slidesStatusStorage) CopyObject(_ context.Context, _, _ string) error { return nil }
func (s *slidesStatusStorage) DeletePrefix(_ context.Context, prefix string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for k := range s.objects {
		if strings.HasPrefix(k, prefix) {
			delete(s.objects, k)
			n++
		}
	}
	return n, nil
}

// --- test helpers ---

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0755))
}

func mustWriteFile(t *testing.T, path string, data []byte) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, data, 0644))
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return data
}
