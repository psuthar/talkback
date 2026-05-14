package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/storage"
	"github.com/psuthar/talkback/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SCRUM-444/445 backend pipeline test matrix.
//
// Twelve cases per the SCRUM-445 description. Tests 1-3 cover the writer
// dispatch (flag-aware tryGenerateAndStoreSlides{,Local}); tests 4-12 cover the
// GetMaterialSlides / GetSlidesStatus read-side contract under both manifest
// shapes. Discipline notes from the ticket:
//   - Storage assertions are positive AND negative — every test that asserts a
//     write also asserts at least one key that must NOT be written.
//   - Presign count, not presign string — test 8 verifies PresignGet is called
//     once per handler invocation (no caching) against the fake storage.
//   - Dual dispatch — test 4 hits both /sessions/ and /api/sessions/ paths.

// stubPDFConverter replaces utils.ConvertPPTXToPDFFn with a deterministic
// stub that writes a minimal PDF to a temp file and returns its path. The real
// soffice binary is not invoked; CI does not need LibreOffice installed.
func stubPDFConverter(t *testing.T, pages int) {
	t.Helper()
	orig := utils.ConvertPPTXToPDFFn
	utils.ConvertPPTXToPDFFn = func(_ string) (string, int, func(), error) {
		dir := t.TempDir()
		pdfPath := filepath.Join(dir, "fake-converter-output.pdf")
		require.NoError(t, os.WriteFile(pdfPath, []byte("%PDF-1.7\n%fake pdf body\n"), 0644))
		return pdfPath, pages, func() {}, nil
	}
	t.Cleanup(func() { utils.ConvertPPTXToPDFFn = orig })
}

// stubPNGConverter replaces utils.ConvertSlidesToPNGsFn with a stub that emits
// `count` synthetic PNG slides.
func stubPNGConverter(t *testing.T, count int) {
	t.Helper()
	orig := utils.ConvertSlidesToPNGsFn
	utils.ConvertSlidesToPNGsFn = func(_ string) ([]utils.ConvertedSlide, error) {
		out := make([]utils.ConvertedSlide, 0, count)
		for i := 1; i <= count; i++ {
			out = append(out, utils.ConvertedSlide{
				Index: i,
				Name:  fmt.Sprintf("slide-%03d.png", i),
				Data:  []byte("PNG-FAKE"),
			})
		}
		return out, nil
	}
	t.Cleanup(func() { utils.ConvertSlidesToPNGsFn = orig })
}

func mkR2DeckMaterial(t *testing.T, h *Handlers, filename string) (*models.Session, *models.Material, string) {
	t.Helper()
	ctx := context.Background()
	session := createTestSessionForHandlers(t, h.DB, "deck-test-"+uuid.NewString())
	artifact, err := h.DB.CreateArtifact(ctx, session.ID, "fixture", nil)
	require.NoError(t, err)
	artifactKey := "sessions/" + session.ID.String() + "/data/uploads/" + filename
	m := &models.Material{
		ID:              uuid.New(),
		ArtifactID:      artifact.ID,
		SessionID:       session.ID,
		Kind:            string(models.MaterialKindDocument),
		Filename:        filename,
		ContentType:     "application/octet-stream",
		StorageProvider: "r2",
		StorageKey:      artifactKey,
		StorageURL:      artifactKey,
		TextStatus:      models.MaterialTextStatusReady,
	}
	require.NoError(t, h.DB.CreateMaterial(ctx, m))
	return session, m, artifactKey
}

// ---- Test 1 --------------------------------------------------------------

func TestSlidesConverter_ProducesPDF_R2Path_SCRUM445(t *testing.T) {
	t.Setenv("TALKBACK_SLIDES_PIPELINE", "pdf")
	stubPDFConverter(t, 7)

	ms := newSlidesStatusStorage()
	h := &Handlers{Storage: ms}

	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, "deck.pptx")
	require.NoError(t, os.WriteFile(localPath, []byte("fake pptx"), 0644))
	artifactKey := "sessions/" + uuid.NewString() + "/data/uploads/deck.pptx"

	h.tryGenerateAndStoreSlides(context.Background(), localPath, artifactKey)

	// Positive: deck.pdf + manifest.json written.
	pdfKey := storage.SlidePDFKeyFromArtifactKey(artifactKey)
	manifestKey := storage.SlidesManifestKeyFromArtifactKey(artifactKey)
	require.True(t, ms.has(pdfKey), "deck.pdf must be written under PDF pipeline")
	require.True(t, ms.has(manifestKey), "manifest.json must be written under PDF pipeline")

	// Negative: no slide-NNN.png keys, no failure marker.
	for i := 1; i <= 10; i++ {
		assert.False(t, ms.has(storage.SlideImageKeyFromArtifactKey(artifactKey, i)),
			"slide-%03d.png must NOT be written under PDF pipeline", i)
	}
	assert.False(t, ms.has(slidesFailureMarkerKeyFromArtifactKey(artifactKey)),
		"failure marker must be absent on success")
	// processing.json was written then cleared on terminal success.
	assert.False(t, ms.has(storage.SlidesProcessingKeyFromArtifactKey(artifactKey)),
		"processing marker must be cleared on terminal success")

	// Manifest shape.
	manifestBytes, ok := ms.get(manifestKey)
	require.True(t, ok)
	var manifest utils.SlideManifest
	require.NoError(t, json.Unmarshal(manifestBytes, &manifest))
	assert.Equal(t, "pdf", manifest.Format)
	assert.Equal(t, 7, manifest.SlideCount)
	assert.Equal(t, pdfKey, manifest.PDFStorageKey)
	assert.Empty(t, manifest.Slides, "PDF manifest must not carry legacy per-slide entries")
}

// ---- Test 2 --------------------------------------------------------------

func TestSlidesConverter_ProducesPDF_LocalStoragePath_SCRUM445(t *testing.T) {
	t.Setenv("TALKBACK_SLIDES_PIPELINE", "pdf")
	t.Setenv("TALKBACK_UPLOAD_ROOT", t.TempDir())
	stubPDFConverter(t, 4)

	storageURL := "sessions/" + uuid.NewString() + "/data/uploads/deck.pptx"
	localPath := filepath.Join(storage.UploadRoot(), storageURL)
	require.NoError(t, os.MkdirAll(filepath.Dir(localPath), 0755))
	require.NoError(t, os.WriteFile(localPath, []byte("fake pptx"), 0644))

	h := &Handlers{} // no Storage, no DB — local path needs neither.
	h.tryGenerateAndStoreSlidesLocal(context.Background(), localPath)

	slidesDir := filepath.Join(filepath.Dir(localPath), filepath.Base(localPath)+"_slides")
	deckPath := filepath.Join(slidesDir, "deck.pdf")
	manifestPath := filepath.Join(slidesDir, "manifest.json")

	require.FileExists(t, deckPath, "deck.pdf must be present under local PDF pipeline")
	require.FileExists(t, manifestPath, "manifest.json must be present under local PDF pipeline")

	// Negative: no slide-NNN.png sidecars.
	entries, err := os.ReadDir(slidesDir)
	require.NoError(t, err)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "slide-") && strings.HasSuffix(e.Name(), ".png") {
			t.Fatalf("unexpected PNG sidecar %q in PDF-pipeline local _slides dir", e.Name())
		}
	}
	_, err = os.Stat(slidesFailureMarkerPathFromStorageURL(storageURL))
	assert.True(t, errors.Is(err, os.ErrNotExist), "failure marker must be absent on success")
	_, err = os.Stat(slidesProcessingMarkerPathFromStorageURL(storageURL))
	assert.True(t, errors.Is(err, os.ErrNotExist), "processing marker must be cleared on terminal success")

	mb, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	var manifest utils.SlideManifest
	require.NoError(t, json.Unmarshal(mb, &manifest))
	assert.Equal(t, "pdf", manifest.Format)
	assert.Equal(t, 4, manifest.SlideCount)
	assert.Equal(t, "deck.pdf", manifest.PDFStoragePath)
}

// ---- Test 3 --------------------------------------------------------------

func TestSlidesConverter_LegacyPath_StillProducesPNGs_SCRUM445(t *testing.T) {
	// Default flag (unset / != "pdf") must keep the existing PNG-per-slide path
	// fully intact so SCRUM-447 cutover stays a one-line flag flip and a quick
	// rollback flips back without any code change.
	t.Setenv("TALKBACK_SLIDES_PIPELINE", "pngs")
	stubPNGConverter(t, 3)

	ms := newSlidesStatusStorage()
	h := &Handlers{Storage: ms}

	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, "deck.pptx")
	require.NoError(t, os.WriteFile(localPath, []byte("fake pptx"), 0644))
	artifactKey := "sessions/" + uuid.NewString() + "/data/uploads/deck.pptx"

	h.tryGenerateAndStoreSlides(context.Background(), localPath, artifactKey)

	// Positive: PNG keys + manifest written.
	for i := 1; i <= 3; i++ {
		assert.True(t, ms.has(storage.SlideImageKeyFromArtifactKey(artifactKey, i)),
			"slide-%03d.png must be written under legacy pipeline", i)
	}
	manifestKey := storage.SlidesManifestKeyFromArtifactKey(artifactKey)
	require.True(t, ms.has(manifestKey), "manifest.json must be written under legacy pipeline")

	// Negative: no deck.pdf under legacy pipeline.
	pdfKey := storage.SlidePDFKeyFromArtifactKey(artifactKey)
	assert.False(t, ms.has(pdfKey), "deck.pdf must NOT be written under legacy PNG pipeline")

	manifestBytes, ok := ms.get(manifestKey)
	require.True(t, ok)
	var manifest utils.SlideManifest
	require.NoError(t, json.Unmarshal(manifestBytes, &manifest))
	assert.Empty(t, manifest.Format, "legacy manifest must not carry a Format tag")
	assert.Len(t, manifest.Slides, 3)
}

// ---- Tests 4-12: GetMaterialSlides + GetSlidesStatus ---------------------

func writePDFManifestToFake(t *testing.T, ms *slidesStatusStorage, artifactKey string, slideCount int) {
	t.Helper()
	pdfKey := storage.SlidePDFKeyFromArtifactKey(artifactKey)
	ms.put(pdfKey, []byte("%PDF-1.7\n%seeded\n"))
	manifest := utils.SlideManifest{
		Format:        "pdf",
		SlideCount:    slideCount,
		PDFStorageKey: pdfKey,
	}
	body, err := json.Marshal(manifest)
	require.NoError(t, err)
	ms.put(storage.SlidesManifestKeyFromArtifactKey(artifactKey), body)
}

func writeLegacyManifestToFake(t *testing.T, ms *slidesStatusStorage, artifactKey string, slideCount int, seedPNGs bool) {
	t.Helper()
	entries := make([]utils.SlideManifestEntry, 0, slideCount)
	for i := 1; i <= slideCount; i++ {
		key := storage.SlideImageKeyFromArtifactKey(artifactKey, i)
		entries = append(entries, utils.SlideManifestEntry{Index: i, StorageKey: key})
		if seedPNGs {
			ms.put(key, []byte("PNG-FAKE"))
		}
	}
	manifest := utils.SlideManifest{Slides: entries}
	body, err := json.Marshal(manifest)
	require.NoError(t, err)
	ms.put(storage.SlidesManifestKeyFromArtifactKey(artifactKey), body)
}

func TestGetMaterialSlides_PDFShape_WhenPDFArtifactExists_SCRUM445(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ms := newSlidesStatusStorage()
	h.Storage = ms

	session, mat, artifactKey := mkR2DeckMaterial(t, h, "deck.pptx")
	writePDFManifestToFake(t, ms, artifactKey, 8)

	for _, urlPath := range []string{
		"/sessions/" + session.ID.String() + "/materials/" + mat.ID.String() + "/slides",
		"/api/sessions/" + session.ID.String() + "/materials/" + mat.ID.String() + "/slides",
	} {
		t.Run(urlPath, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, urlPath, nil)
			w := httptest.NewRecorder()
			h.GetMaterialSlides(w, req)

			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			var resp MaterialSlidesResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			assert.Equal(t, "pdf", resp.Format)
			assert.Equal(t, 8, resp.SlideCount)
			assert.NotEmpty(t, resp.PDFURL, "pdf_url must be populated under PDF shape")
			assert.Empty(t, resp.Slides, "slides[] must be empty under PDF shape")
		})
	}

	// Negative: no per-PNG presigns happened across either dispatch path.
	for i := 1; i <= 10; i++ {
		assert.Equal(t, 0, ms.presignCountFor(storage.SlideImageKeyFromArtifactKey(artifactKey, i)),
			"no slide-%03d.png presign under PDF shape", i)
	}
}

func TestGetMaterialSlides_LegacyPNGShape_WhenOnlyPNGManifestExists_SCRUM445(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ms := newSlidesStatusStorage()
	h.Storage = ms

	session, mat, artifactKey := mkR2DeckMaterial(t, h, "deck.pptx")
	writeLegacyManifestToFake(t, ms, artifactKey, 4, true /* seed PNGs */)

	req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"/materials/"+mat.ID.String()+"/slides", nil)
	w := httptest.NewRecorder()
	h.GetMaterialSlides(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp MaterialSlidesResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp.Format, "Format must be absent for legacy shape")
	assert.Equal(t, 0, resp.SlideCount, "slide_count must be absent for legacy shape")
	assert.Empty(t, resp.PDFURL, "pdf_url must be empty for legacy shape")
	require.Len(t, resp.Slides, 4)
	for _, s := range resp.Slides {
		assert.NotEmpty(t, s.ImageURL)
	}

	// Negative: deck.pdf was never presigned (no PDF artifact exists for this material).
	assert.Equal(t, 0, ms.presignCountFor(storage.SlidePDFKeyFromArtifactKey(artifactKey)),
		"deck.pdf must not be presigned under legacy shape")
}

func TestGetMaterialSlides_404_WhenNeitherArtifactExists_SCRUM445(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ms := newSlidesStatusStorage()
	h.Storage = ms

	session, mat, _ := mkR2DeckMaterial(t, h, "deck.pptx")
	// No manifest, no PDF, no slides. Background goroutine hasn't run.

	req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"/materials/"+mat.ID.String()+"/slides", nil)
	w := httptest.NewRecorder()
	h.GetMaterialSlides(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	assert.NotContains(t, w.Body.String(), "500", "must fail with a clean 404, not a 500")
}

func TestGetMaterialSlides_PDFTakesPrecedence_WhenBothExist_SCRUM445(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ms := newSlidesStatusStorage()
	h.Storage = ms

	session, mat, artifactKey := mkR2DeckMaterial(t, h, "deck.pptx")
	// Seed legacy PNGs first (mid-migration leftovers) ...
	for i := 1; i <= 3; i++ {
		ms.put(storage.SlideImageKeyFromArtifactKey(artifactKey, i), []byte("PNG-FAKE"))
	}
	// ... then overwrite the manifest with the PDF-shape pointing at deck.pdf.
	writePDFManifestToFake(t, ms, artifactKey, 3)

	req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"/materials/"+mat.ID.String()+"/slides", nil)
	w := httptest.NewRecorder()
	h.GetMaterialSlides(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp MaterialSlidesResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "pdf", resp.Format)
	assert.NotEmpty(t, resp.PDFURL)

	// Negative: stale PNGs are never presigned when the manifest declares PDF.
	for i := 1; i <= 3; i++ {
		assert.Equal(t, 0, ms.presignCountFor(storage.SlideImageKeyFromArtifactKey(artifactKey, i)),
			"stale slide-%03d.png must NOT be presigned when manifest says PDF", i)
	}
	assert.Equal(t, 1, ms.presignCountFor(storage.SlidePDFKeyFromArtifactKey(artifactKey)),
		"exactly one PresignGet should target deck.pdf")
}

func TestGetMaterialSlides_PresignedEachCall_SCRUM445(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ms := newSlidesStatusStorage()
	h.Storage = ms

	session, mat, artifactKey := mkR2DeckMaterial(t, h, "deck.pptx")
	writePDFManifestToFake(t, ms, artifactKey, 2)

	urlPath := "/sessions/" + session.ID.String() + "/materials/" + mat.ID.String() + "/slides"
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, urlPath, nil)
		w := httptest.NewRecorder()
		h.GetMaterialSlides(w, req)
		require.Equal(t, http.StatusOK, w.Code, "call %d: %s", i, w.Body.String())
	}

	// Asserting count, not URL string — SCRUM-445 ticket discipline: presign
	// equality is a weaker assertion (can pass accidentally when caches collide).
	assert.Equal(t, 2, ms.presignCount(),
		"two GetMaterialSlides calls must yield two PresignGet invocations (no caching)")
	assert.Equal(t, 2, ms.presignCountFor(storage.SlidePDFKeyFromArtifactKey(artifactKey)),
		"both presigns must target deck.pdf")
}

func TestGetSlidesStatus_ReadyWhenOnlyPDFExists_SCRUM445(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ms := newSlidesStatusStorage()
	artifactKey := "sessions/" + uuid.NewString() + "/data/uploads/deck.pptx"
	writePDFManifestToFake(t, ms, artifactKey, 5)

	h := &Handlers{Storage: ms}
	mat := &models.Material{
		ID:              uuid.New(),
		Filename:        "deck.pptx",
		StorageProvider: "r2",
		StorageKey:      artifactKey,
		Kind:            string(models.MaterialKindDocument),
	}
	assert.Equal(t, "ready", h.GetSlidesStatus(ctx, mat))
}

func TestGetSlidesStatus_ReadyWhenOnlyLegacyManifestExists_SCRUM445(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ms := newSlidesStatusStorage()
	artifactKey := "sessions/" + uuid.NewString() + "/data/uploads/deck.pptx"
	writeLegacyManifestToFake(t, ms, artifactKey, 3, false /* skip PNG bodies — Status checks manifest only */)

	h := &Handlers{Storage: ms}
	mat := &models.Material{
		ID:              uuid.New(),
		Filename:        "deck.pptx",
		StorageProvider: "r2",
		StorageKey:      artifactKey,
		Kind:            string(models.MaterialKindDocument),
	}
	assert.Equal(t, "ready", h.GetSlidesStatus(ctx, mat))
}

func TestGetMaterialSlides_CorruptPDFArtifact_NoManifest_SCRUM445(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ms := newSlidesStatusStorage()
	h.Storage = ms

	session, mat, artifactKey := mkR2DeckMaterial(t, h, "deck.pptx")
	// Manifest declares PDF, but deck.pdf object was lost (eviction, rollback).
	manifest := utils.SlideManifest{
		Format:        "pdf",
		SlideCount:    5,
		PDFStorageKey: storage.SlidePDFKeyFromArtifactKey(artifactKey),
	}
	body, err := json.Marshal(manifest)
	require.NoError(t, err)
	ms.put(storage.SlidesManifestKeyFromArtifactKey(artifactKey), body)
	// Deliberately do NOT put deck.pdf.

	req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"/materials/"+mat.ID.String()+"/slides", nil)
	w := httptest.NewRecorder()
	h.GetMaterialSlides(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	// Negative: no PresignGet must be attempted when the artifact head fails.
	assert.Equal(t, 0, ms.presignCount(),
		"missing deck.pdf must short-circuit before any PresignGet")
}

func TestGetMaterialSlides_CorruptManifest_NoSlidePNGs_SCRUM445(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ms := newSlidesStatusStorage()
	h.Storage = ms

	session, mat, artifactKey := mkR2DeckMaterial(t, h, "deck.pptx")
	// Legacy manifest declares 3 slides but slide-001.png was never written.
	writeLegacyManifestToFake(t, ms, artifactKey, 3, false /* no PNG bodies */)

	req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"/materials/"+mat.ID.String()+"/slides", nil)
	w := httptest.NewRecorder()
	h.GetMaterialSlides(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	assert.Equal(t, 0, ms.presignCount(),
		"missing slide-001.png must short-circuit before any PresignGet")
}
