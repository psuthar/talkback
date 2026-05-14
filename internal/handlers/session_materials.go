package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/auth"
	"github.com/psuthar/talkback/internal/markitdown"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/storage"
	"github.com/psuthar/talkback/internal/utils"
)

// SCRUM-334: spreadsheet uploads have an inner 5 MiB sidecar cap (the
// /extract/file endpoint) but the multipart limit lives at 10 MiB. A 6 MB
// XLSX would slip past the upload step and only fail at extraction,
// leaving the user with a stuck row and a vague failure. We cap
// spreadsheet uploads explicitly at the handler boundary so the failure
// is fast and the UI can show a clear error.
const defaultSpreadsheetMaxBytes int64 = 5 << 20 // 5 MiB

// spreadsheetMaxBytes returns the configured cap. Override at runtime via
// TALKBACK_SPREADSHEET_MAX_BYTES (positive integer, bytes). Operators can
// raise this for specific deployments without a code change. Invalid or
// non-positive values fall back to the default.
func spreadsheetMaxBytes() int64 {
	raw := os.Getenv("TALKBACK_SPREADSHEET_MAX_BYTES")
	if raw == "" {
		return defaultSpreadsheetMaxBytes
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v <= 0 {
		return defaultSpreadsheetMaxBytes
	}
	return v
}

// isSpreadsheetExt reports whether the lower-case file extension belongs
// to the SCRUM-329 spreadsheet allowlist (CSV / XLS / XLSX). Centralised
// so the cap and any future helper agree.
func isSpreadsheetExt(ext string) bool {
	return ext == ".csv" || ext == ".xls" || ext == ".xlsx"
}

// MaterialSlidesResponse is the JSON response for GET .../materials/{material_id}/slides.
//
// SCRUM-444/445: dual-format read path. Legacy materials with a {"slides":[...]}
// manifest return the original shape (Slides populated, Format/SlideCount/PDFURL
// empty). New PDF-pipeline materials return Format="pdf" + SlideCount + PDFURL,
// with Slides serialized as an empty array. Exactly one of PDFURL / Slides is
// populated for any given material.
type MaterialSlidesResponse struct {
	MaterialID string                 `json:"material_id"`
	Format     string                 `json:"format,omitempty"`
	SlideCount int                    `json:"slide_count,omitempty"`
	PDFURL     string                 `json:"pdf_url,omitempty"`
	Slides     []MaterialSlidePayload `json:"slides"`
}

// MaterialSlidePayload is one slide entry in MaterialSlidesResponse.
type MaterialSlidePayload struct {
	Index    int    `json:"index"`
	ImageURL string `json:"image_url"`
}

// sniffImageContentType returns a MIME type when file begins with a common raster image signature.
// Used when browsers send application/octet-stream or omit Content-Type for uploads.
func sniffImageContentType(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	buf := make([]byte, 32)
	n, err := f.Read(buf)
	if err != nil || n < 3 {
		return "", false
	}
	buf = buf[:n]
	switch {
	case n >= 3 && buf[0] == 0xFF && buf[1] == 0xD8 && buf[2] == 0xFF:
		return "image/jpeg", true
	case n >= 8 && buf[0] == 0x89 && buf[1] == 0x50 && buf[2] == 0x4E && buf[3] == 0x47 &&
		buf[4] == 0x0D && buf[5] == 0x0A && buf[6] == 0x1A && buf[7] == 0x0A:
		return "image/png", true
	case n >= 6 && buf[0] == 0x47 && buf[1] == 0x49 && buf[2] == 0x46 && buf[3] == 0x38 &&
		(buf[4] == 0x37 || buf[4] == 0x39) && buf[5] == 0x61:
		return "image/gif", true
	case n >= 2 && buf[0] == 0x42 && buf[1] == 0x4D:
		return "image/bmp", true
	case n >= 12 && buf[0] == 0x52 && buf[1] == 0x49 && buf[2] == 0x46 && buf[3] == 0x46 &&
		buf[8] == 0x57 && buf[9] == 0x45 && buf[10] == 0x42 && buf[11] == 0x50:
		return "image/webp", true
	case n >= 4 && buf[0] == 0x49 && buf[1] == 0x49 && buf[2] == 0x2A && buf[3] == 0x00:
		return "image/tiff", true
	case n >= 4 && buf[0] == 0x4D && buf[1] == 0x4D && buf[2] == 0x00 && buf[3] == 0x2A:
		return "image/tiff", true
	}
	return "", false
}

func refineUploadAsImageIfMagic(filePath string, ext string, contentType *string, isImage *bool, kind *string) {
	ct, magic := sniffImageContentType(filePath)
	if !magic {
		return
	}
	*isImage = true
	ctHdr := strings.ToLower(strings.TrimSpace(*contentType))
	if ctHdr == "application/octet-stream" || ctHdr == "" {
		*contentType = ct
	}
	*kind = deriveMaterialKind(ext, *contentType, true)
}

// ListSessionMaterials handles GET /api/sessions/:id/materials and GET /sessions/:id/materials
func (h *Handlers) ListSessionMaterials(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sessionID, err := sessionIDFromPath(r.URL.Path, 3) // api/sessions/:id/materials or sessions/:id/materials
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_, err = h.DB.GetSession(r.Context(), sessionID)
	if err != nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}
	materials, err := h.DB.GetActiveMaterialsBySessionID(r.Context(), sessionID)
	if err != nil {
		log.Printf("ListSessionMaterials: %v", err)
		http.Error(w, "Failed to list materials", http.StatusInternalServerError)
		return
	}
	if materials == nil {
		materials = []*models.Material{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(materials)
}

// SessionUploadMaterialRequest optional title for multipart (form field "title")
// File is form field "file".

// SessionUploadMaterial handles POST /api/sessions/:id/materials/upload and POST /sessions/:id/materials/upload
func (h *Handlers) SessionUploadMaterial(w http.ResponseWriter, r *http.Request) {
	log.Printf("SessionUploadMaterial called: %s %s", r.Method, r.URL.Path)
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sessionID, err := sessionIDFromPath(r.URL.Path, 4) // .../materials/upload (4 parts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	session, err := h.DB.GetSession(r.Context(), sessionID)
	if err != nil || session == nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}
	artifactID, err := h.ensureSessionArtifactForMaterials(r.Context(), sessionID)
	if err != nil {
		log.Printf("SessionUploadMaterial ensure artifact: %v", err)
		http.Error(w, "Failed to prepare session for upload", http.StatusInternalServerError)
		return
	}
	count, err := h.DB.CountActiveMaterialsBySessionID(r.Context(), sessionID)
	if err != nil {
		log.Printf("SessionUploadMaterial count materials: %v", err)
		http.Error(w, "Failed to check materials limit", http.StatusInternalServerError)
		return
	}
	if auth.Config.MaxMaterialsPerSession > 0 && count >= auth.Config.MaxMaterialsPerSession {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]any{
			"error":             "session materials limit reached",
			"max_materials":     auth.Config.MaxMaterialsPerSession,
		})
		return
	}

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse form: %v", err), http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, fmt.Sprintf("Missing or invalid file: %v", err), http.StatusBadRequest)
		return
	}
	defer file.Close()

	filename := storage.NormalizeFilename(header.Filename)
	exists, err := h.DB.ExistsMaterialWithFilenameInSession(r.Context(), sessionID, filename)
	if err != nil {
		log.Printf("SessionUploadMaterial check duplicate: %v", err)
		http.Error(w, "Failed to check existing files", http.StatusInternalServerError)
		return
	}
	if exists {
		http.Error(w, fmt.Sprintf("A file named %q is already in this session", filename), http.StatusConflict)
		return
	}

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	storageURL := storage.SessionArtifactPath(sessionID, filename)
	ext := strings.ToLower(filepath.Ext(filename))

	// SCRUM-334: explicit spreadsheet upload size cap. Reject early with a
	// structured 413 so the UI can show "this spreadsheet is too large"
	// instead of letting the file flow through the pipeline and fail
	// later at sidecar extraction time. Returns JSON with both the cap
	// and the actual size so the UI can render a precise error.
	if isSpreadsheetExt(ext) {
		cap := spreadsheetMaxBytes()
		if header.Size > cap {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":        "spreadsheet_too_large",
				"max_bytes":    cap,
				"actual_bytes": header.Size,
			})
			return
		}
	}

	isImage := strings.HasPrefix(contentType, "image/") ||
		ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".gif" || ext == ".webp" || ext == ".bmp" || ext == ".svg" ||
		ext == ".heic" || ext == ".heif" || ext == ".avif" || ext == ".jfif" || ext == ".tif" || ext == ".tiff" || ext == ".ico"
	kind := deriveMaterialKind(ext, contentType, isImage)

	var filePath string
	var storageProvider string
	var storageKey string

	var removeTempWhenDone bool
	if h.Storage != nil {
		// R2 path: write to temp file, upload to R2, extract from temp, then remove temp (unless slide gen runs in background)
		// Temp file MUST have the original extension so ExtractTextFromFile can detect PDF/DOCX/etc.
		tmpDir := os.TempDir()
		tmpPattern := "talkback-upload-*" + ext
		tmpFile, err := os.CreateTemp(tmpDir, tmpPattern)
		if err != nil {
			http.Error(w, "Failed to create temp file", http.StatusInternalServerError)
			return
		}
		filePath = tmpFile.Name()
		removeTempWhenDone = true
		defer func() { if removeTempWhenDone { _ = os.Remove(filePath) } }()
		if _, err := io.Copy(tmpFile, file); err != nil {
			_ = tmpFile.Close()
			http.Error(w, "Failed to write temp file", http.StatusInternalServerError)
			return
		}
		if err := tmpFile.Close(); err != nil {
			http.Error(w, "Failed to close temp file", http.StatusInternalServerError)
			return
		}
		refineUploadAsImageIfMagic(filePath, ext, &contentType, &isImage, &kind)
		prefix := strings.TrimSuffix(strings.TrimSpace(os.Getenv("R2_PREFIX")), "/")
		storageKey = storage.BuildArtifactStorageKey(prefix, sessionID, artifactID, filename)
		f, err := os.Open(filePath)
		if err != nil {
			http.Error(w, "Failed to open temp file for upload", http.StatusInternalServerError)
			return
		}
		_, _, err = h.Storage.Put(r.Context(), storageKey, f, contentType, 0)
		_ = f.Close()
		if err != nil {
			log.Printf("SessionUploadMaterial R2 Put: %v", err)
			http.Error(w, "Failed to upload file to storage", http.StatusInternalServerError)
			return
		}
		storageProvider = "r2"
	} else {
		// Local path: write to uploads dir
		uploadsDir := storage.SessionUploadsAbsDir(sessionID)
		log.Printf("SessionUploadMaterial: storing to %s", uploadsDir)
		if err := os.MkdirAll(uploadsDir, 0755); err != nil {
			http.Error(w, "Failed to create uploads directory", http.StatusInternalServerError)
			return
		}
		filePath = filepath.Join(uploadsDir, filename)
		if err := utils.SaveFile(file, filePath); err != nil {
			http.Error(w, "Failed to save file", http.StatusInternalServerError)
			return
		}
		refineUploadAsImageIfMagic(filePath, ext, &contentType, &isImage, &kind)
		storageProvider = "local"
	}

	textStatus := models.MaterialTextStatusPending
	var extractedText *string
	var errMsg *string
	// SCRUM-303: when the operator has opted into image captioning, set
	// text_status=pending so the worker can populate extracted_text via
	// the sidecar. The decision to actually enqueue the extraction job is
	// re-checked below (needsImageExtraction) so a flag flipped on without
	// a configured sidecar client cleanly falls back to the legacy
	// "ready with empty text" path.
	imageExtractionGate := isImage && markitdown.ImageExtractionEnabled() && h.Markitdown != nil && h.Markitdown.Enabled()
	// SCRUM-332: when the operator has opted into spreadsheet ingestion via
	// the markitdown sidecar, route CSV/XLS/XLSX through the async path so
	// extracted_text receives a markdown table (one ## SheetName per sheet
	// for multi-sheet XLSX). Defaults to off; existing pure-Go XLSX excelize
	// path remains the canonical route for unconfigured environments and
	// for .xlsx specifically when sync extraction succeeds.
	isSpreadsheet := ext == ".csv" || ext == ".xls" || ext == ".xlsx"
	fileExtractionGate := isSpreadsheet && markitdown.FileExtractionEnabled() && h.Markitdown != nil && h.Markitdown.Enabled()
	switch {
	case imageExtractionGate:
		textStatus = models.MaterialTextStatusPending
	case isImage:
		textStatus = models.MaterialTextStatusReady
	case (ext == ".csv" || contentType == "text/csv") && filePath != "":
		// SCRUM-371: CSV is parsed synchronously via Go's stdlib so the
		// markitdown sidecar isn't a hard dependency for this format
		// (production was leaving CSVs stuck in `pending` whenever the
		// sidecar wasn't configured — the previous fileExtractionGate
		// path was the only one that handled .csv at all). On success
		// we land at ready+text. On parse failure we fall back to the
		// sidecar if it's enabled (preserves the existing async path
		// semantics) and only land at `failed` when neither route is
		// available — never silent `pending`.
		if data, readErr := os.ReadFile(filePath); readErr == nil {
			if md, csvErr := utils.CSVToMarkdown(data, utils.DefaultCSVMarkdownMaxBytes); csvErr == nil {
				textStatus = models.MaterialTextStatusReady
				extractedText = &md
				log.Printf("SessionUploadMaterial: extracted %s text synchronously via CSVToMarkdown (%d bytes, no async job needed)", filename, len(md))
			} else if fileExtractionGate {
				// Sync failed but sidecar is wired up — defer to the
				// existing async route. The downstream needsExtraction
				// predicate sees fileExtractionGate==true + textStatus
				// pending and enqueues the worker job as it always did.
				textStatus = models.MaterialTextStatusPending
				log.Printf("SessionUploadMaterial: CSV sync extraction failed for %s (%v); deferring to markitdown sidecar", filename, csvErr)
			} else {
				textStatus = models.MaterialTextStatusFailed
				s := fmt.Sprintf("csv extraction failed: %v", csvErr)
				errMsg = &s
			}
		} else {
			textStatus = models.MaterialTextStatusFailed
			s := readErr.Error()
			errMsg = &s
		}
	case ext == ".xls" && filePath != "":
		// SCRUM-395: legacy binary .xls (MIME application/vnd.ms-excel) is a
		// different format from .xlsx — excelize can't read it, isOfficeFile()
		// doesn't recognize it, and it used to fall through every case and sit
		// in text_status=pending forever (no async job was ever enqueued). Parse
		// it synchronously via the pure-Go BIFF reader, mirroring the .csv
		// (SCRUM-371) and .xlsx synchronous paths — no markitdown-sidecar
		// dependency. Placed before fileExtractionGate so .xls always uses the
		// native path. On extraction failure, land at `failed` with a clear
		// error_message — never silent `pending`.
		extracted, exErr := utils.ExtractTextFromFileWithMeta(filePath, filename, contentType)
		if exErr == nil && strings.TrimSpace(extracted) != "" {
			textStatus = models.MaterialTextStatusReady
			extractedText = &extracted
			log.Printf("SessionUploadMaterial: extracted %s text synchronously via pure-Go xls reader (%d bytes, no async job needed)", filename, len(extracted))
		} else {
			textStatus = models.MaterialTextStatusFailed
			s := "xls extraction failed"
			if exErr != nil {
				s = fmt.Sprintf("xls extraction failed: %v", exErr)
			} else {
				s = "xls extraction produced no text"
			}
			errMsg = &s
		}
	case fileExtractionGate:
		// SCRUM-332: defer extraction to the async worker so the markitdown
		// sidecar can produce markdown. Pre-empts the pure-Go excelize path
		// below for .xlsx. SCRUM-371 carved .csv out of this case and SCRUM-395
		// carved .xls out (native pure-Go path above); only .xlsx now lands here.
		textStatus = models.MaterialTextStatusPending
	case contentType == "text/plain" || ext == ".txt" || ext == ".md":
		content, err := os.ReadFile(filePath)
		if err == nil {
			t := string(content)
			extractedText = &t
			textStatus = models.MaterialTextStatusReady
		} else {
			textStatus = models.MaterialTextStatusFailed
			s := err.Error()
			errMsg = &s
		}
	case contentType == "application/pdf" || ext == ".pdf":
		// PDF: extraction runs async via job processor; return 201 immediately with text_status=pending.
		// PDF parsing via pdftotext or ledongthuc/pdf can be slow, so keep async.
		textStatus = models.MaterialTextStatusPending
	case isOfficeFile(ext, contentType) && (ext == ".pptx" || ext == ".ppt" || ext == ".docx" || ext == ".xlsx"):
		// Office (PPTX/DOCX/XLSX): pure-Go extraction is fast (milliseconds). Extract synchronously while
		// the temp/local file is still in hand, avoiding an async round-trip that re-downloads from R2.
		// Fall back to async (pending) only if extraction fails or produces no text.
		if filePath != "" {
			extracted, exErr := utils.ExtractTextFromFileWithMeta(filePath, filename, contentType)
			if exErr == nil && strings.TrimSpace(extracted) != "" {
				textStatus = models.MaterialTextStatusReady
				extractedText = &extracted
				log.Printf("SessionUploadMaterial: extracted %s text synchronously (%d bytes, no async job needed)", ext, len(extracted))
			} else {
				// Extraction failed or empty - fall through to async job
				textStatus = models.MaterialTextStatusPending
				if exErr != nil {
					log.Printf("SessionUploadMaterial: sync extraction failed for %s (%s), will retry async: %v", filename, ext, exErr)
				}
			}
		} else {
			textStatus = models.MaterialTextStatusPending
		}
	case isOfficeFile(ext, contentType):
		// Other OOXML office types matched by content-type (not by the explicit
		// extension list above): extraction runs async via job processor.
		textStatus = models.MaterialTextStatusPending
	case isVideoForTranscription(ext, contentType):
		// Video: transcript is handled by VideoSource + job; material is just the file reference — show ready so it doesn't stay "pending"
		textStatus = models.MaterialTextStatusReady
	default:
		// SCRUM-395 fail-safe: any upload that matched no extraction path above
		// (e.g. legacy .ppt / .doc, which have no pure-Go text extractor, or any
		// unrecognized format) would otherwise be created with the initial
		// text_status=pending and never transition — the UI shows a permanent
		// "Processing…" spinner with no error. Mark it failed with a clear
		// reason instead. (Slide derivation for .ppt still runs below.)
		textStatus = models.MaterialTextStatusFailed
		s := fmt.Sprintf("no text-extraction handler for this file type (ext=%q, content-type=%q)", ext, contentType)
		errMsg = &s
	}

	titleFromForm := r.FormValue("title")
	var title *string
	if titleFromForm != "" {
		title = &titleFromForm
	} else {
		title = &filename
	}

	sizeBytes := header.Size
	material := &models.Material{
		ID:               uuid.New(),
		ArtifactID:       artifactID,
		SessionID:        sessionID,
		Kind:             kind,
		Filename:         filename,
		ContentType:      contentType,
		StorageURL:       storageURL,
		StorageProvider:  storageProvider,
		StorageKey:       storageKey,
		SizeBytes:        &sizeBytes,
		TextStatus:       textStatus,
		ExtractedText:    extractedText,
		Title:            title,
		ErrorMessage:     errMsg,
	}
	if err := h.DB.CreateMaterial(r.Context(), material); err != nil {
		if storageProvider == "r2" && storageKey != "" && h.Storage != nil {
			_ = h.Storage.Delete(r.Context(), storageKey)
		}
		if storageProvider == "local" && filePath != "" {
			_ = os.Remove(filePath)
		}
		log.Printf("SessionUploadMaterial CreateMaterial: %v", err)
		http.Error(w, "Failed to create material record", http.StatusInternalServerError)
		return
	}

	// Enqueue material extraction job for PDF/Office so extraction runs async (worker sets processing → ready/failed, then index + broadcast).
	// Skip the async job when text was already extracted synchronously during upload (textStatus==ready with extractedText set).
	// SCRUM-303: image materials also need the async path when image-extraction
	// is gated on; the worker calls the sidecar.
	// SCRUM-332: spreadsheets (CSV/XLS/XLSX) gated on fileExtractionGate go
	// through the same async path so the worker can call the sidecar's
	// /extract/file endpoint and store markdown.
	needsExtraction := (((contentType == "application/pdf" || ext == ".pdf") || isOfficeFile(ext, contentType)) ||
		imageExtractionGate ||
		fileExtractionGate) &&
		!(textStatus == models.MaterialTextStatusReady && extractedText != nil)
	if needsExtraction && h.JobProcessor != nil {
		jobKey := "material_extract:" + material.ID.String()
		existing, _ := h.DB.GetTranscriptJobByKey(r.Context(), jobKey)
		if existing == nil || existing.Status == models.TranscriptJobStatusFailed {
			var sourceURL string
			if storageProvider == "local" {
				sourceURL = filepath.ToSlash(storageURL)
			} else if storageProvider == "r2" && storageKey != "" && h.Storage != nil {
				presigned, err := h.Storage.PresignGet(r.Context(), storageKey, 24*time.Hour)
				if err != nil {
					log.Printf("SessionUploadMaterial PresignGet for material extraction: %v", err)
				} else {
					sourceURL = presigned
				}
			}
			if sourceURL != "" {
				job := &models.TranscriptJob{
					ID:         uuid.New(),
					MaterialID: &material.ID,
					SessionID:  sessionID,
					Status:     models.TranscriptJobStatusQueued,
					SourceURL:  sourceURL,
					JobKey:     jobKey,
					QueuedAt:   time.Now(),
				}
				if err := h.DB.CreateTranscriptJob(r.Context(), job); err != nil {
					log.Printf("SessionUploadMaterial CreateTranscriptJob: %v", err)
				} else if err := h.JobProcessor.Enqueue(r.Context(), job); err != nil {
					log.Printf("SessionUploadMaterial Enqueue material extraction: %v", err)
				}
			}
		}
	}

	// Best-effort slide derivation for PPT/PPTX (R2 or local): always run in background so code path is the same everywhere.
	// R2: goroutine keeps temp file and removes it when done; request returns before Render's ~30s timeout.
	if (ext == ".ppt" || ext == ".pptx") && filePath != "" {
		if storageProvider == "r2" && h.Storage != nil && storageKey != "" {
			removeTempWhenDone = false
			pathCopy := filePath
			keyCopy := storageKey
			sidCopy := sessionID
			go func() {
				defer func() { _ = os.Remove(pathCopy) }()
				h.tryGenerateAndStoreSlides(context.Background(), pathCopy, keyCopy)
				if h.Hub != nil {
					h.Hub.BroadcastSessionUpdated(sidCopy)
				}
			}()
		} else if storageProvider == "local" {
			pathCopy := filePath
			sidCopy := sessionID
			go func() {
				h.tryGenerateAndStoreSlidesLocal(context.Background(), pathCopy)
				if h.Hub != nil {
					h.Hub.BroadcastSessionUpdated(sidCopy)
				}
			}()
		}
	}
	// For video files: create a VideoSource so the file appears in "Additional Videos" and is playable.
	if isVideoForTranscription(ext, contentType) {
		videoStoredKey := filepath.ToSlash(storageURL)
		if storageProvider == "r2" && storageKey != "" {
			videoStoredKey = storageKey
		}
		videoID := uuid.New()
		originalURL := "file:///" + filename
		// SCRUM-295: also create a file_artifacts row for this upload so the
		// SCRUM-271/272 primary system (which keys off file_artifacts.id)
		// can reference it. Same path as video_upload.go's createVideoFileArtifact.
		fileArtifactID := createVideoFileArtifact(r.Context(), h, sessionID, filename, contentType, 0, storageProvider == "r2", videoStoredKey)
		videoSource := &models.VideoSource{
			ID:                   videoID,
			ArtifactID:           artifactID,
			SessionID:            sessionID,
			Provider:             "other",
			PlaybackMode:         "direct",
			SourceType:           models.VideoSourceTypeUpload,
			StoredVideoObjectKey: &videoStoredKey,
			OriginalURL:          &originalURL,
			TranscriptStatus:     models.VideoTranscriptStatusPending,
			AutoTranscribeEnabled: true,
			FileArtifactID:       fileArtifactID,
		}
		if err := h.DB.CreateVideoSource(r.Context(), videoSource); err != nil {
			log.Printf("SessionUploadMaterial CreateVideoSource: %v", err)
		} else {
			// Enqueue Whisper transcription: local path or R2 presigned URL
			if h.JobProcessor != nil {
				var sourceURL string
				if storageProvider == "local" {
					sourceURL = filepath.ToSlash(storageURL)
				} else if storageProvider == "r2" && storageKey != "" && h.Storage != nil {
					presigned, err := h.Storage.PresignGet(r.Context(), storageKey, time.Hour)
					if err != nil {
						log.Printf("SessionUploadMaterial PresignGet for transcript job: %v", err)
					} else {
						sourceURL = presigned
					}
				}
				if sourceURL != "" {
					keyInput := sourceURL
					if storageProvider == "r2" && storageKey != "" {
						keyInput = storageKey
					}
					jobKey := utils.GenerateJobKey(videoID.String(), keyInput)
					existing, _ := h.DB.GetTranscriptJobByKey(r.Context(), jobKey)
					if existing == nil || existing.Status == models.TranscriptJobStatusFailed {
						job := &models.TranscriptJob{
							ID:            uuid.New(),
							VideoSourceID: videoID,
							SessionID:     sessionID,
							Status:        models.TranscriptJobStatusQueued,
							SourceURL:     sourceURL,
							JobKey:        jobKey,
							QueuedAt:      time.Now(),
						}
						if err := h.DB.CreateTranscriptJob(r.Context(), job); err != nil {
							log.Printf("SessionUploadMaterial CreateTranscriptJob: %v", err)
						} else if err := h.DB.UpdateVideoSourceTranscriptionJob(r.Context(), videoID, &job.ID); err != nil {
							log.Printf("SessionUploadMaterial UpdateVideoSourceTranscriptionJob: %v", err)
						} else if err := h.JobProcessor.Enqueue(r.Context(), job); err != nil {
							log.Printf("SessionUploadMaterial Enqueue transcript job: %v", err)
						}
					}
				}
			}
		}
	}
	if material.ExtractedText != nil && *material.ExtractedText != "" {
		h.triggerIndex(sessionID)
	}
	if h.Hub != nil {
		h.Hub.BroadcastSessionUpdated(sessionID)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(material)
}

// SessionPasteMaterialRequest body for POST .../materials/paste
type SessionPasteMaterialRequest struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

// SessionPasteMaterial handles POST /api/sessions/:id/materials/paste and POST /sessions/:id/materials/paste
func (h *Handlers) SessionPasteMaterial(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sessionID, err := sessionIDFromPath(r.URL.Path, 4) // .../materials/paste (4 parts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	session, err := h.DB.GetSession(r.Context(), sessionID)
	if err != nil || session == nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}
	artifactID, err := h.ensureSessionArtifactForMaterials(r.Context(), sessionID)
	if err != nil {
		log.Printf("SessionPasteMaterial ensure artifact: %v", err)
		http.Error(w, "Failed to prepare session", http.StatusInternalServerError)
		return
	}
	count, err := h.DB.CountActiveMaterialsBySessionID(r.Context(), sessionID)
	if err != nil {
		log.Printf("SessionPasteMaterial count materials: %v", err)
		http.Error(w, "Failed to check materials limit", http.StatusInternalServerError)
		return
	}
	if auth.Config.MaxMaterialsPerSession > 0 && count >= auth.Config.MaxMaterialsPerSession {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]any{
			"error":          "session materials limit reached",
			"max_materials":  auth.Config.MaxMaterialsPerSession,
		})
		return
	}

	var req SessionPasteMaterialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		http.Error(w, "text is required and must be non-empty", http.StatusBadRequest)
		return
	}
	title := req.Title
	if title == "" {
		title = "Pasted text"
	}

	material := &models.Material{
		ID:            uuid.New(),
		ArtifactID:    artifactID,
		SessionID:     sessionID,
		Kind:          "document",
		Filename:      "",
		ContentType:   "text/plain",
		StorageURL:    "",
		TextStatus:    models.MaterialTextStatusReady,
		ExtractedText: &text,
		Title:         &title,
	}
	if err := h.DB.CreateMaterial(r.Context(), material); err != nil {
		http.Error(w, "Failed to create material", http.StatusInternalServerError)
		return
	}
	h.triggerIndex(sessionID)
	if h.Hub != nil {
		h.Hub.BroadcastSessionUpdated(sessionID)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(material)
}

// DeleteSessionMaterial handles DELETE /api/sessions/:id/materials/:material_id and DELETE /sessions/:id/materials/:material_id
func (h *Handlers) DeleteSessionMaterial(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	// api/sessions/:id/materials/:mid (5 parts) or sessions/:id/materials/:mid (4 parts)
	var sessionIDStr, materialIDStr string
	if len(pathParts) >= 5 && pathParts[0] == "api" && pathParts[1] == "sessions" && pathParts[3] == "materials" {
		sessionIDStr = pathParts[2]
		materialIDStr = pathParts[4]
	} else if len(pathParts) >= 4 && pathParts[0] == "sessions" && pathParts[2] == "materials" {
		sessionIDStr = pathParts[1]
		materialIDStr = pathParts[3]
	} else {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(sessionIDStr)
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	materialID, err := uuid.Parse(materialIDStr)
	if err != nil {
		http.Error(w, "Invalid material ID", http.StatusBadRequest)
		return
	}
	mat, err := h.DB.GetMaterialByID(r.Context(), materialID)
	if err != nil || mat == nil {
		http.Error(w, "Material not found", http.StatusNotFound)
		return
	}
	if mat.SessionID != sessionID {
		http.Error(w, "Material not found", http.StatusNotFound)
		return
	}
	// Remove file from storage first (R2 or local), then soft-delete the row (tombstone).
	// Also remove derived PPT/PPTX slide assets (manifest + PNGs under *_slides/). If we only delete the
	// main object, re-uploading the same filename reuses the same StorageKey and stale slides would
	// appear "ready" in ~1s while pointing at the previous deck's images.
	if mat.StorageProvider == "r2" && mat.StorageKey != "" && h.Storage != nil {
		if err := h.Storage.Delete(r.Context(), mat.StorageKey); err != nil {
			log.Printf("DeleteSessionMaterial R2 Delete: %v", err)
		}
		slidePrefix := storage.SlidesPrefixFromArtifactKey(mat.StorageKey)
		if n, err := h.Storage.DeletePrefix(r.Context(), slidePrefix); err != nil {
			log.Printf("DeleteSessionMaterial R2 DeletePrefix slides %s: %v", slidePrefix, err)
		} else if n > 0 {
			log.Printf("DeleteSessionMaterial: removed %d derived slide object(s) under prefix %s", n, slidePrefix)
		}
	} else if mat.StorageURL != "" {
		path := filepath.Join(storage.UploadRoot(), filepath.FromSlash(mat.StorageURL))
		_ = os.Remove(path)
		slidesDir := filepath.Join(storage.UploadRoot(), filepath.FromSlash(mat.StorageURL)+"_slides")
		if err := os.RemoveAll(slidesDir); err != nil && !os.IsNotExist(err) {
			log.Printf("DeleteSessionMaterial local RemoveAll slides dir %s: %v", slidesDir, err)
		}
	}
	// If this material was a video file, remove the linked VideoSource(s) so the Presentation video section stays in sync.
	matKeyNorm := filepath.ToSlash(mat.StorageURL)
	if mat.StorageKey != "" {
		matKeyNorm = mat.StorageKey
	}
	if matKeyNorm != "" {
		sources, _ := h.DB.GetVideoSourcesBySessionID(r.Context(), sessionID)
		for _, vs := range sources {
			if vs.StoredVideoObjectKey != nil {
				key := *vs.StoredVideoObjectKey
				keyNorm := filepath.ToSlash(key)
				if keyNorm == matKeyNorm || key == matKeyNorm || key == mat.StorageURL || key == mat.StorageKey {
					if err := h.DB.DeleteVideoSourceByID(r.Context(), vs.ID); err != nil {
						log.Printf("DeleteSessionMaterial DeleteVideoSourceByID %s: %v", vs.ID, err)
					}
					break
				}
			}
		}
	}
	// Delete chunks for this material (embeddings cascade-delete)
	if err := h.DB.DeleteSessionChunksBySource(r.Context(), sessionID, "material", materialID); err != nil {
		log.Printf("DeleteSessionMaterial delete chunks: %v", err)
	}
	if err := h.DB.SoftDeleteMaterial(r.Context(), materialID); err != nil {
		http.Error(w, "Failed to delete material", http.StatusInternalServerError)
		return
	}
	if h.Hub != nil {
		h.Hub.BroadcastSessionUpdated(sessionID)
	}
	w.WriteHeader(http.StatusNoContent)
}

// ensureSessionArtifactForMaterials returns the first artifact for the session, or creates one.
func (h *Handlers) ensureSessionArtifactForMaterials(ctx context.Context, sessionID uuid.UUID) (uuid.UUID, error) {
	artifacts, err := h.DB.GetArtifactsBySessionID(ctx, sessionID)
	if err != nil {
		return uuid.Nil, err
	}
	if len(artifacts) > 0 {
		return artifacts[0].ID, nil
	}
	artifact, err := h.DB.CreateArtifact(ctx, sessionID, "Session materials", nil)
	if err != nil {
		return uuid.Nil, err
	}
	return artifact.ID, nil
}

// tryGenerateAndStoreSlides performs best-effort slide derivation for PPT/PPTX materials.
// It never returns errors to the caller; failures are logged for debugging.
//
// SCRUM-444/445: dispatches to the PDF or PNG pipeline based on
// TALKBACK_SLIDES_PIPELINE. Default and unset values keep the legacy PNG path
// for full backward compatibility; the SCRUM-447 cutover flips this to "pdf".
func (h *Handlers) tryGenerateAndStoreSlides(ctx context.Context, localPath string, artifactKey string) {
	pipelineStart := time.Now()
	log.Printf("slide generation started for %s (key=%s) pipeline=%s", localPath, artifactKey, utils.SlidesPipeline())
	// SCRUM-443: write processing.json BEFORE clearing failed.json so concurrent reads always see
	// a marker. defer the clear so an OOM-killed goroutine leaves processing.json behind for
	// GetSlidesStatus to detect via the staleness threshold.
	h.writeSlidesProcessingMarkerStorage(ctx, artifactKey)
	defer h.clearSlidesProcessingMarkerStorage(ctx, artifactKey)
	h.clearSlidesFailureMarkerStorage(ctx, artifactKey)
	if utils.SlidesPipeline() == utils.SlidesPipelinePDF {
		h.tryGenerateAndStoreSlidesPDF(ctx, localPath, artifactKey, pipelineStart)
		return
	}
	tConv := time.Now()
	slides, err := utils.ConvertSlidesToPNGsFn(localPath)
	convElapsed := time.Since(tConv)
	if err != nil {
		log.Printf("slides conversion failed for %s: %v", localPath, err)
		h.writeSlidesFailureMarkerStorage(ctx, artifactKey, err.Error())
		return
	}

	// Pre-allocate manifest entries by slide index so order is deterministic regardless
	// of which goroutine finishes first.
	manifestEntries := make([]utils.SlideManifestEntry, len(slides))

	tUpload := time.Now()
	var uploadBytes atomic.Int64
	var uploadErr atomic.Value // stores first error string
	var wg sync.WaitGroup
	for i, slide := range slides {
		wg.Add(1)
		go func(idx int, s utils.ConvertedSlide) {
			defer wg.Done()
			key := storage.SlideImageKeyFromArtifactKey(artifactKey, s.Index)
			_, _, err := h.Storage.Put(ctx, key, bytes.NewReader(s.Data), "image/png", int64(len(s.Data)))
			if err != nil {
				uploadErr.CompareAndSwap(nil, err.Error())
				log.Printf("failed uploading derived slide %d for %s: %v", s.Index, artifactKey, err)
				return
			}
			uploadBytes.Add(int64(len(s.Data)))
			manifestEntries[idx] = utils.SlideManifestEntry{
				Index:      s.Index,
				StorageKey: key,
			}
		}(i, slide)
	}
	wg.Wait()
	uploadElapsed := time.Since(tUpload)

	if errVal := uploadErr.Load(); errVal != nil {
		h.writeSlidesFailureMarkerStorage(ctx, artifactKey, errVal.(string))
		return
	}

	manifest := utils.SlideManifest{Slides: manifestEntries}

	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		log.Printf("failed marshalling slide manifest for %s: %v", artifactKey, err)
		h.writeSlidesFailureMarkerStorage(ctx, artifactKey, err.Error())
		return
	}

	manifestKey := storage.SlidesManifestKeyFromArtifactKey(artifactKey)
	tManifest := time.Now()
	_, _, err = h.Storage.Put(ctx, manifestKey, bytes.NewReader(manifestBytes), "application/json", int64(len(manifestBytes)))
	manifestElapsed := time.Since(tManifest)
	if err != nil {
		log.Printf("failed uploading slide manifest for %s: %v", artifactKey, err)
		h.writeSlidesFailureMarkerStorage(ctx, artifactKey, err.Error())
		return
	}

	h.clearSlidesFailureMarkerStorage(ctx, artifactKey)
	log.Printf("slides pipeline summary: key=%s slides=%d convert=%v upload_pngs=%v (%d bytes) manifest_put=%v total=%v",
		artifactKey, len(manifest.Slides), convElapsed, uploadElapsed, uploadBytes.Load(), manifestElapsed, time.Since(pipelineStart))
	log.Printf("generated %d derived slides for %s", len(manifest.Slides), artifactKey)
}

// tryGenerateAndStoreSlidesLocal performs best-effort slide derivation for PPT/PPTX stored on local disk.
// Writes manifest.json and slide-001.png, slide-002.png, ... into a _slides subdir next to the source file.
//
// SCRUM-444/445: dispatches to the PDF or PNG pipeline based on
// TALKBACK_SLIDES_PIPELINE (same flag, same defaults as the R2 path).
func (h *Handlers) tryGenerateAndStoreSlidesLocal(_ context.Context, localPath string) {
	pipelineStart := time.Now()
	log.Printf("slide generation started (local) for %s pipeline=%s", localPath, utils.SlidesPipeline())
	// SCRUM-443: mirror R2 path — write processing.json before clearing failed.json; defer clear so
	// a killed goroutine leaves the marker for GetSlidesStatus to staleness-check.
	writeSlidesProcessingMarkerLocal(localPath)
	defer clearSlidesProcessingMarkerLocal(localPath)
	clearSlidesFailureMarkerLocal(localPath)
	if utils.SlidesPipeline() == utils.SlidesPipelinePDF {
		tryGenerateAndStoreSlidesLocalPDF(localPath, pipelineStart)
		return
	}
	tConv := time.Now()
	slides, err := utils.ConvertSlidesToPNGsFn(localPath)
	convElapsed := time.Since(tConv)
	if err != nil {
		log.Printf("slides conversion failed for %s: %v", localPath, err)
		writeSlidesFailureMarkerLocal(localPath, err.Error())
		return
	}
	dir := filepath.Join(filepath.Dir(localPath), filepath.Base(localPath)+"_slides")
	tMkdir := time.Now()
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("slides local mkdir failed for %s: %v", localPath, err)
		writeSlidesFailureMarkerLocal(localPath, err.Error())
		return
	}
	mkdirElapsed := time.Since(tMkdir)
	manifest := utils.SlideManifest{Slides: make([]utils.SlideManifestEntry, 0, len(slides))}
	tWrite := time.Now()
	for _, slide := range slides {
		name := fmt.Sprintf("slide-%03d.png", slide.Index)
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, slide.Data, 0644); err != nil {
			log.Printf("failed writing slide %d for %s: %v", slide.Index, localPath, err)
			writeSlidesFailureMarkerLocal(localPath, err.Error())
			return
		}
		manifest.Slides = append(manifest.Slides, utils.SlideManifestEntry{
			Index:      slide.Index,
			StorageKey: name,
		})
	}
	writeElapsed := time.Since(tWrite)
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		log.Printf("failed marshalling slide manifest for %s: %v", localPath, err)
		writeSlidesFailureMarkerLocal(localPath, err.Error())
		return
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), manifestBytes, 0644); err != nil {
		log.Printf("failed writing slide manifest for %s: %v", localPath, err)
		writeSlidesFailureMarkerLocal(localPath, err.Error())
		return
	}
	clearSlidesFailureMarkerLocal(localPath)
	log.Printf("slides pipeline summary (local): path=%s slides=%d convert=%v mkdir=%v write_files=%v total=%v",
		localPath, len(manifest.Slides), convElapsed, mkdirElapsed, writeElapsed, time.Since(pipelineStart))
	log.Printf("generated %d derived slides (local) for %s", len(manifest.Slides), localPath)
}

// tryGenerateAndStoreSlidesPDF is the SCRUM-444/445 PDF-pipeline analogue of
// the per-slide PNG path: convert PPTX → single PDF, stream the PDF up to R2 as
// deck.pdf, and write a format-tagged manifest. The SPA (SCRUM-446) reads
// pdf_url + slide_count and renders pages on demand via PDF.js, eliminating the
// pdftoppm memory peak that drove the Render Starter OOMs.
//
// Markers (processing.json / failed.json) are managed by the caller so the
// staleness auto-fail logic in GetSlidesStatus stays unchanged.
func (h *Handlers) tryGenerateAndStoreSlidesPDF(ctx context.Context, localPath, artifactKey string, pipelineStart time.Time) {
	tConv := time.Now()
	pdfPath, slideCount, cleanupConv, err := utils.ConvertPPTXToPDFFn(localPath)
	convElapsed := time.Since(tConv)
	if err != nil {
		log.Printf("slides PDF conversion failed for %s: %v", localPath, err)
		h.writeSlidesFailureMarkerStorage(ctx, artifactKey, err.Error())
		return
	}
	defer cleanupConv()

	f, err := os.Open(pdfPath)
	if err != nil {
		log.Printf("slides PDF open failed for %s: %v", pdfPath, err)
		h.writeSlidesFailureMarkerStorage(ctx, artifactKey, err.Error())
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		log.Printf("slides PDF stat failed for %s: %v", pdfPath, err)
		h.writeSlidesFailureMarkerStorage(ctx, artifactKey, err.Error())
		return
	}

	pdfKey := storage.SlidePDFKeyFromArtifactKey(artifactKey)
	tUpload := time.Now()
	_, _, err = h.Storage.Put(ctx, pdfKey, f, "application/pdf", info.Size())
	uploadElapsed := time.Since(tUpload)
	if err != nil {
		log.Printf("failed uploading derived PDF for %s: %v", artifactKey, err)
		h.writeSlidesFailureMarkerStorage(ctx, artifactKey, err.Error())
		return
	}

	manifest := utils.SlideManifest{
		Format:        utils.SlidesPipelinePDF,
		SlideCount:    slideCount,
		PDFStorageKey: pdfKey,
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		log.Printf("failed marshalling PDF slide manifest for %s: %v", artifactKey, err)
		h.writeSlidesFailureMarkerStorage(ctx, artifactKey, err.Error())
		return
	}
	manifestKey := storage.SlidesManifestKeyFromArtifactKey(artifactKey)
	tManifest := time.Now()
	_, _, err = h.Storage.Put(ctx, manifestKey, bytes.NewReader(manifestBytes), "application/json", int64(len(manifestBytes)))
	manifestElapsed := time.Since(tManifest)
	if err != nil {
		log.Printf("failed uploading PDF slide manifest for %s: %v", artifactKey, err)
		h.writeSlidesFailureMarkerStorage(ctx, artifactKey, err.Error())
		return
	}

	h.clearSlidesFailureMarkerStorage(ctx, artifactKey)
	log.Printf("slides PDF pipeline summary: key=%s pages=%d pdf_bytes=%d convert=%v upload_pdf=%v manifest_put=%v total=%v",
		artifactKey, slideCount, info.Size(), convElapsed, uploadElapsed, manifestElapsed, time.Since(pipelineStart))
	log.Printf("generated derived PDF (%d pages) for %s", slideCount, artifactKey)
}

// tryGenerateAndStoreSlidesLocalPDF is the local-disk analogue of the R2 PDF
// pipeline: produces <storageURL>_slides/deck.pdf + a format-tagged
// manifest.json. The GetMaterialSlidePDF handler streams deck.pdf with
// Accept-Ranges support so PDF.js can range-fetch pages.
func tryGenerateAndStoreSlidesLocalPDF(localPath string, pipelineStart time.Time) {
	tConv := time.Now()
	pdfPath, slideCount, cleanupConv, err := utils.ConvertPPTXToPDFFn(localPath)
	convElapsed := time.Since(tConv)
	if err != nil {
		log.Printf("slides PDF conversion failed (local) for %s: %v", localPath, err)
		writeSlidesFailureMarkerLocal(localPath, err.Error())
		return
	}
	defer cleanupConv()

	dir := filepath.Join(filepath.Dir(localPath), filepath.Base(localPath)+"_slides")
	tMkdir := time.Now()
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("slides PDF local mkdir failed for %s: %v", localPath, err)
		writeSlidesFailureMarkerLocal(localPath, err.Error())
		return
	}
	mkdirElapsed := time.Since(tMkdir)

	dstPath := filepath.Join(dir, "deck.pdf")
	tCopy := time.Now()
	if err := copyLocalPDFFile(pdfPath, dstPath); err != nil {
		log.Printf("slides PDF local copy failed for %s: %v", localPath, err)
		writeSlidesFailureMarkerLocal(localPath, err.Error())
		return
	}
	copyElapsed := time.Since(tCopy)

	manifest := utils.SlideManifest{
		Format:         utils.SlidesPipelinePDF,
		SlideCount:     slideCount,
		PDFStoragePath: "deck.pdf",
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		log.Printf("failed marshalling PDF slide manifest (local) for %s: %v", localPath, err)
		writeSlidesFailureMarkerLocal(localPath, err.Error())
		return
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), manifestBytes, 0644); err != nil {
		log.Printf("failed writing PDF slide manifest (local) for %s: %v", localPath, err)
		writeSlidesFailureMarkerLocal(localPath, err.Error())
		return
	}
	clearSlidesFailureMarkerLocal(localPath)
	log.Printf("slides PDF pipeline summary (local): path=%s pages=%d convert=%v mkdir=%v copy=%v total=%v",
		localPath, slideCount, convElapsed, mkdirElapsed, copyElapsed, time.Since(pipelineStart))
	log.Printf("generated derived PDF (%d pages, local) for %s", slideCount, localPath)
}

// copyLocalPDFFile copies the converter's temp PDF into the persistent _slides
// directory. A direct rename would be faster but the temp dir lives on a
// different volume from uploads under TALKBACK_SOFFICE_CMD (Docker) mode, so a
// streaming copy is the portable choice.
func copyLocalPDFFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open src: %w", err)
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create dst: %w", err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy: %w", err)
	}
	return nil
}

// HasSlidesManifest returns true if the material (PPT/PPTX) has a derived slides manifest available (viewable in UI).
func (h *Handlers) HasSlidesManifest(ctx context.Context, mat *models.Material) bool {
	if mat == nil || !models.MaterialSupportsDerivedSlideDeck(mat) {
		return false
	}
	if mat.StorageProvider == "local" && strings.TrimSpace(mat.StorageURL) != "" {
		manifestPath := filepath.Join(storage.UploadRoot(), mat.StorageURL+"_slides", "manifest.json")
		_, err := os.Stat(manifestPath)
		return err == nil
	}
	if h.Storage != nil && strings.TrimSpace(mat.StorageKey) != "" {
		// Use Head (no body download) to check manifest existence.
		manifestKey := storage.SlidesManifestKeyFromArtifactKey(mat.StorageKey)
		exists, _, _, err := h.Storage.Head(ctx, manifestKey)
		return err == nil && exists
	}
	return false
}

func slidesFailureMarkerKeyFromArtifactKey(artifactKey string) string {
	return storage.SlidesPrefixFromArtifactKey(artifactKey) + "failed.json"
}

func slidesFailureMarkerPathFromStorageURL(storageURL string) string {
	return filepath.Join(storage.UploadRoot(), storageURL+"_slides", "failed.json")
}

func slidesManifestPathFromStorageURL(storageURL string) string {
	return filepath.Join(storage.UploadRoot(), storageURL+"_slides", "manifest.json")
}

func writeSlidesFailureMarkerLocal(localPath, errMsg string) {
	dir := filepath.Join(filepath.Dir(localPath), filepath.Base(localPath)+"_slides")
	if mkErr := os.MkdirAll(dir, 0755); mkErr != nil {
		return
	}
	payload, _ := json.Marshal(map[string]string{
		"status": "failed",
		"error":  errMsg,
	})
	_ = os.WriteFile(filepath.Join(dir, "failed.json"), payload, 0644)
}

func clearSlidesFailureMarkerLocal(localPath string) {
	dir := filepath.Join(filepath.Dir(localPath), filepath.Base(localPath)+"_slides")
	_ = os.Remove(filepath.Join(dir, "failed.json"))
}

func (h *Handlers) writeSlidesFailureMarkerStorage(ctx context.Context, artifactKey, errMsg string) {
	if h.Storage == nil || strings.TrimSpace(artifactKey) == "" {
		return
	}
	payload, _ := json.Marshal(map[string]string{
		"status": "failed",
		"error":  errMsg,
	})
	key := slidesFailureMarkerKeyFromArtifactKey(artifactKey)
	_, _, _ = h.Storage.Put(ctx, key, bytes.NewReader(payload), "application/json", int64(len(payload)))
}

func (h *Handlers) clearSlidesFailureMarkerStorage(ctx context.Context, artifactKey string) {
	if h.Storage == nil || strings.TrimSpace(artifactKey) == "" {
		return
	}
	_ = h.Storage.Delete(ctx, slidesFailureMarkerKeyFromArtifactKey(artifactKey))
}

// SCRUM-443: in-flight slide-derivation marker. Written on goroutine entry, deleted (via defer) on
// terminal success/error. If the goroutine is killed mid-flight (OOM, container restart), the marker
// persists and GetSlidesStatus uses its age to detect stranded conversions and auto-fail them.

// SlidesStaleProcessingThreshold is how long a processing.json marker may live before GetSlidesStatus
// treats it as stranded. soffice has a 3-min hard cap + pdftoppm ~1 min + R2 uploads ~1 min, so 6 min
// is safely past any successful completion.
const SlidesStaleProcessingThreshold = 6 * time.Minute

type slidesProcessingMarker struct {
	StartedAt     string `json:"started_at"`
	MarkerVersion int    `json:"marker_version"`
}

func slidesProcessingMarkerPayload() []byte {
	payload, _ := json.Marshal(slidesProcessingMarker{
		StartedAt:     time.Now().UTC().Format(time.RFC3339),
		MarkerVersion: 1,
	})
	return payload
}

func slidesProcessingMarkerPathFromStorageURL(storageURL string) string {
	return filepath.Join(storage.UploadRoot(), storageURL+"_slides", "processing.json")
}

func writeSlidesProcessingMarkerLocal(localPath string) {
	dir := filepath.Join(filepath.Dir(localPath), filepath.Base(localPath)+"_slides")
	if mkErr := os.MkdirAll(dir, 0755); mkErr != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, "processing.json"), slidesProcessingMarkerPayload(), 0644)
}

func clearSlidesProcessingMarkerLocal(localPath string) {
	dir := filepath.Join(filepath.Dir(localPath), filepath.Base(localPath)+"_slides")
	_ = os.Remove(filepath.Join(dir, "processing.json"))
}

func (h *Handlers) writeSlidesProcessingMarkerStorage(ctx context.Context, artifactKey string) {
	if h.Storage == nil || strings.TrimSpace(artifactKey) == "" {
		return
	}
	payload := slidesProcessingMarkerPayload()
	key := storage.SlidesProcessingKeyFromArtifactKey(artifactKey)
	_, _, _ = h.Storage.Put(ctx, key, bytes.NewReader(payload), "application/json", int64(len(payload)))
}

func (h *Handlers) clearSlidesProcessingMarkerStorage(ctx context.Context, artifactKey string) {
	if h.Storage == nil || strings.TrimSpace(artifactKey) == "" {
		return
	}
	_ = h.Storage.Delete(ctx, storage.SlidesProcessingKeyFromArtifactKey(artifactKey))
}

// handleStaleSlidesMarkerLocal lazily auto-fails a stale processing.json marker on the local filesystem.
// Returns true if the marker was stale (in which case failed.json has been written and processing.json
// removed). Returns false on parse errors, fresh markers, or read errors — caller falls through to
// "processing" so the UI keeps polling rather than locking a brand-new conversion into a wrong state.
func (h *Handlers) handleStaleSlidesMarkerLocal(storageURL string, data []byte) bool {
	var marker slidesProcessingMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		return false
	}
	startedAt, err := time.Parse(time.RFC3339, marker.StartedAt)
	if err != nil {
		return false
	}
	if time.Since(startedAt) <= SlidesStaleProcessingThreshold {
		return false
	}
	localSourcePath := filepath.Join(storage.UploadRoot(), storageURL)
	writeSlidesFailureMarkerLocal(localSourcePath,
		fmt.Sprintf("stranded by instance restart or kill (no terminal marker after %v)", SlidesStaleProcessingThreshold))
	clearSlidesProcessingMarkerLocal(localSourcePath)
	return true
}

// handleStaleSlidesMarkerStorage is the R2 equivalent of handleStaleSlidesMarkerLocal. It downloads
// the processing.json body via Storage.Get (Head doesn't return content), checks the started_at age,
// and if stale writes failed.json + deletes processing.json. Returns true iff it auto-failed.
func (h *Handlers) handleStaleSlidesMarkerStorage(ctx context.Context, artifactKey, processingKey string) bool {
	reader, err := h.Storage.Get(ctx, processingKey)
	if err != nil || reader == nil {
		return false
	}
	data, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if readErr != nil {
		return false
	}
	var marker slidesProcessingMarker
	if jsonErr := json.Unmarshal(data, &marker); jsonErr != nil {
		return false
	}
	startedAt, parseErr := time.Parse(time.RFC3339, marker.StartedAt)
	if parseErr != nil {
		return false
	}
	if time.Since(startedAt) <= SlidesStaleProcessingThreshold {
		return false
	}
	h.writeSlidesFailureMarkerStorage(ctx, artifactKey,
		fmt.Sprintf("stranded by instance restart or kill (no terminal marker after %v)", SlidesStaleProcessingThreshold))
	h.clearSlidesProcessingMarkerStorage(ctx, artifactKey)
	return true
}

// GetSlidesStatus returns "ready", "processing", or "failed" for PPT/PPTX materials using the slide pipeline.
//
// Precedence (SCRUM-443):
//  1. manifest.json present → "ready"
//  2. failed.json present → "failed"
//  3. processing.json present + started_at older than SlidesStaleProcessingThreshold → lazily write
//     failed.json + delete processing.json, return "failed" (stranded by OOM/restart)
//  4. processing.json present + fresh → "processing"
//  5. no markers at all → "processing" (legacy compat for materials that pre-date processing.json)
func (h *Handlers) GetSlidesStatus(ctx context.Context, mat *models.Material) string {
	if mat == nil || !models.MaterialSupportsDerivedSlideDeck(mat) {
		return ""
	}
	if mat.StorageProvider == "local" && strings.TrimSpace(mat.StorageURL) != "" {
		if _, err := os.Stat(slidesManifestPathFromStorageURL(mat.StorageURL)); err == nil {
			return "ready"
		}
		if _, err := os.Stat(slidesFailureMarkerPathFromStorageURL(mat.StorageURL)); err == nil {
			return "failed"
		}
		if data, err := os.ReadFile(slidesProcessingMarkerPathFromStorageURL(mat.StorageURL)); err == nil {
			if h.handleStaleSlidesMarkerLocal(mat.StorageURL, data) {
				return "failed"
			}
		}
		return "processing"
	}
	if h.Storage != nil && strings.TrimSpace(mat.StorageKey) != "" {
		// Use Head (no body download) to check existence — avoids downloading manifest/marker bytes on every session load.
		manifestKey := storage.SlidesManifestKeyFromArtifactKey(mat.StorageKey)
		if exists, _, _, err := h.Storage.Head(ctx, manifestKey); err == nil && exists {
			return "ready"
		}
		failureKey := slidesFailureMarkerKeyFromArtifactKey(mat.StorageKey)
		if exists, _, _, err := h.Storage.Head(ctx, failureKey); err == nil && exists {
			return "failed"
		}
		processingKey := storage.SlidesProcessingKeyFromArtifactKey(mat.StorageKey)
		if exists, _, _, err := h.Storage.Head(ctx, processingKey); err == nil && exists {
			if h.handleStaleSlidesMarkerStorage(ctx, mat.StorageKey, processingKey) {
				return "failed"
			}
		}
		return "processing"
	}
	return "processing"
}

// sessionIDFromPath parses session ID from path like "api/sessions/:id/..." or "sessions/:id/..."
func sessionIDFromPath(path string, minParts int) (uuid.UUID, error) {
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	var idStr string
	if parts[0] == "api" && len(parts) >= 3 && parts[1] == "sessions" {
		idStr = parts[2]
	} else if parts[0] == "sessions" && len(parts) >= 2 {
		idStr = parts[1]
	} else {
		return uuid.Nil, fmt.Errorf("invalid path")
	}
	if minParts > 0 && len(parts) < minParts {
		return uuid.Nil, fmt.Errorf("invalid path")
	}
	return uuid.Parse(idStr)
}

// isVideoForTranscription returns true for file types we transcribe with Whisper (e.g. MP4)
func isVideoForTranscription(ext, contentType string) bool {
	if strings.HasPrefix(strings.ToLower(contentType), "video/") {
		return true
	}
	switch ext {
	case ".mp4", ".webm", ".mov", ".m4a", ".mp3", ".wav", ".mpeg", ".mpg", ".avi", ".mkv":
		return true
	}
	return false
}

// GetMaterialSlides handles GET /sessions/{session_id}/materials/{material_id}/slides.
// Returns ordered slide image URLs (presigned) from the derived slides manifest, or 200 with empty slides if manifest is missing.
func (h *Handlers) GetMaterialSlides(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	var sessionIDStr, materialIDStr string
	switch {
	case len(pathParts) >= 6 && pathParts[0] == "api" && pathParts[1] == "sessions" && pathParts[3] == "materials" && pathParts[5] == "slides":
		sessionIDStr = pathParts[2]
		materialIDStr = pathParts[4]
	case len(pathParts) >= 5 && pathParts[0] == "sessions" && pathParts[2] == "materials" && pathParts[4] == "slides":
		sessionIDStr = pathParts[1]
		materialIDStr = pathParts[3]
	default:
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(sessionIDStr)
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	materialID, err := uuid.Parse(materialIDStr)
	if err != nil {
		http.Error(w, "Invalid material ID", http.StatusBadRequest)
		return
	}
	mat, err := h.DB.GetMaterialByID(ctx, materialID)
	if err != nil || mat == nil {
		http.Error(w, "Material not found", http.StatusNotFound)
		return
	}
	if mat.SessionID != sessionID {
		http.Error(w, "Material not found", http.StatusNotFound)
		return
	}
	if !models.MaterialSupportsDerivedSlideDeck(mat) {
		http.Error(w, "Material not found", http.StatusNotFound)
		return
	}
	resp := MaterialSlidesResponse{
		MaterialID: materialID.String(),
		Slides:     []MaterialSlidePayload{},
	}

	// Local storage: read manifest from disk and dispatch on Format.
	if mat.StorageProvider == "local" && strings.TrimSpace(mat.StorageURL) != "" {
		manifestPath := filepath.Join(storage.UploadRoot(), mat.StorageURL+"_slides", "manifest.json")
		manifestBytes, err := os.ReadFile(manifestPath)
		if err != nil {
			log.Printf("slides manifest missing or unreadable for material %s path %s: %v", materialID, manifestPath, err)
			http.Error(w, "Slides not available", http.StatusNotFound)
			return
		}
		var manifest utils.SlideManifest
		if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
			log.Printf("failed to decode slides manifest for material %s: %v", materialID, err)
			http.Error(w, "Slides not available", http.StatusNotFound)
			return
		}
		scheme := "https"
		if r.TLS == nil {
			scheme = "http"
		}
		if s := r.Header.Get("X-Forwarded-Proto"); s != "" {
			scheme = s
		}
		baseURL := scheme + "://" + r.Host
		// SCRUM-444/445: PDF-format manifests serve deck.pdf via the slide-pdf
		// handler so PDF.js can range-fetch pages.
		if strings.EqualFold(manifest.Format, utils.SlidesPipelinePDF) {
			pdfPath := filepath.Join(storage.UploadRoot(), mat.StorageURL+"_slides", "deck.pdf")
			if _, err := os.Stat(pdfPath); err != nil {
				log.Printf("slides PDF artifact missing for material %s path %s: %v", materialID, pdfPath, err)
				http.Error(w, "Slides not available", http.StatusNotFound)
				return
			}
			resp.Format = utils.SlidesPipelinePDF
			resp.SlideCount = manifest.SlideCount
			resp.PDFURL = fmt.Sprintf("%s/sessions/%s/materials/%s/slide-pdf", baseURL, sessionID.String(), materialID.String())
			writeJSON(w, http.StatusOK, resp)
			return
		}
		// Legacy PNG path. Treat an empty manifest (no Format, no entries) as a
		// corrupt write and fail closed so the SPA shows a real error instead of
		// a silently-empty deck.
		if len(manifest.Slides) == 0 {
			log.Printf("slides manifest has no entries for material %s path %s (corrupt or mid-write)", materialID, manifestPath)
			http.Error(w, "Slides not available", http.StatusNotFound)
			return
		}
		for _, entry := range manifest.Slides {
			slideURL := fmt.Sprintf("%s/sessions/%s/materials/%s/slide-image?index=%d", baseURL, sessionID.String(), materialID.String(), entry.Index)
			resp.Slides = append(resp.Slides, MaterialSlidePayload{
				Index:    entry.Index,
				ImageURL: slideURL,
			})
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	// R2 storage: read manifest from storage and dispatch on Format.
	if h.Storage == nil || strings.TrimSpace(mat.StorageKey) == "" {
		http.Error(w, "Slides not available", http.StatusNotFound)
		return
	}
	manifestKey := storage.SlidesManifestKeyFromArtifactKey(mat.StorageKey)
	rc, err := h.Storage.Get(ctx, manifestKey)
	if err != nil {
		log.Printf("slides manifest missing or unreadable for material %s key %s: %v (slide generation may still be running or have failed — check logs for 'slide generation started' / 'slides conversion failed' / 'generated N derived slides')", materialID, manifestKey, err)
		http.Error(w, "Slides not available", http.StatusNotFound)
		return
	}
	defer rc.Close()
	var manifest utils.SlideManifest
	if err := json.NewDecoder(rc).Decode(&manifest); err != nil {
		log.Printf("failed to decode slides manifest for material %s key %s: %v", materialID, manifestKey, err)
		http.Error(w, "Slides not available", http.StatusNotFound)
		return
	}
	presignTTL := time.Hour
	// SCRUM-444/445: PDF-format manifests presign deck.pdf instead of per-image
	// URLs. Head() first so a corrupt manifest (manifest says PDF but deck.pdf
	// missing) fails with 404 instead of returning a presigned URL that 403s on
	// fetch. Test #11 guards this.
	if strings.EqualFold(manifest.Format, utils.SlidesPipelinePDF) {
		pdfKey := manifest.PDFStorageKey
		if strings.TrimSpace(pdfKey) == "" {
			pdfKey = storage.SlidePDFKeyFromArtifactKey(mat.StorageKey)
		}
		exists, _, _, hErr := h.Storage.Head(ctx, pdfKey)
		if hErr != nil || !exists {
			log.Printf("slides PDF artifact missing for material %s key %s: exists=%v err=%v", materialID, pdfKey, exists, hErr)
			http.Error(w, "Slides not available", http.StatusNotFound)
			return
		}
		url, err := h.Storage.PresignGet(ctx, pdfKey, presignTTL)
		if err != nil {
			log.Printf("failed to presign PDF for material %s key %s: %v", materialID, pdfKey, err)
			http.Error(w, "Slides not available", http.StatusNotFound)
			return
		}
		resp.Format = utils.SlidesPipelinePDF
		resp.SlideCount = manifest.SlideCount
		resp.PDFURL = url
		writeJSON(w, http.StatusOK, resp)
		return
	}

	// Legacy PNG path. Head the first slide as a sanity check so a corrupt
	// manifest (slide-001.png removed/never-written) fails fast with 404 rather
	// than returning broken URLs (test #12).
	if len(manifest.Slides) == 0 {
		log.Printf("slides manifest has no entries for material %s key %s (corrupt or mid-write)", materialID, manifestKey)
		http.Error(w, "Slides not available", http.StatusNotFound)
		return
	}
	firstKey := manifest.Slides[0].StorageKey
	if exists, _, _, hErr := h.Storage.Head(ctx, firstKey); hErr != nil || !exists {
		log.Printf("first slide missing for material %s manifest_key=%s slide_key=%s exists=%v err=%v", materialID, manifestKey, firstKey, exists, hErr)
		http.Error(w, "Slides not available", http.StatusNotFound)
		return
	}
	resp.Slides = make([]MaterialSlidePayload, 0, len(manifest.Slides))
	for _, entry := range manifest.Slides {
		url, err := h.Storage.PresignGet(ctx, entry.StorageKey, presignTTL)
		if err != nil {
			log.Printf("failed to presign slide %d for material %s key %s: %v", entry.Index, materialID, entry.StorageKey, err)
			continue
		}
		resp.Slides = append(resp.Slides, MaterialSlidePayload{
			Index:    entry.Index,
			ImageURL: url,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// GetMaterialSlideImage serves a single slide PNG for local-storage materials (GET .../materials/:id/slide-image?index=N).
func (h *Handlers) GetMaterialSlideImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	var sessionIDStr, materialIDStr string
	switch {
	case len(pathParts) >= 6 && pathParts[0] == "api" && pathParts[1] == "sessions" && pathParts[3] == "materials" && pathParts[5] == "slide-image":
		sessionIDStr = pathParts[2]
		materialIDStr = pathParts[4]
	case len(pathParts) >= 5 && pathParts[0] == "sessions" && pathParts[2] == "materials" && pathParts[4] == "slide-image":
		sessionIDStr = pathParts[1]
		materialIDStr = pathParts[3]
	default:
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	indexStr := r.URL.Query().Get("index")
	if indexStr == "" {
		http.Error(w, "index query required", http.StatusBadRequest)
		return
	}
	var index int
	if _, err := fmt.Sscanf(indexStr, "%d", &index); err != nil || index < 1 {
		http.Error(w, "index must be a positive integer", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(sessionIDStr)
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	materialID, err := uuid.Parse(materialIDStr)
	if err != nil {
		http.Error(w, "Invalid material ID", http.StatusBadRequest)
		return
	}
	mat, err := h.DB.GetMaterialByID(ctx, materialID)
	if err != nil || mat == nil {
		http.Error(w, "Material not found", http.StatusNotFound)
		return
	}
	if mat.SessionID != sessionID {
		http.Error(w, "Material not found", http.StatusNotFound)
		return
	}
	if !models.MaterialSupportsDerivedSlideDeck(mat) {
		http.Error(w, "Material not found", http.StatusNotFound)
		return
	}
	if mat.StorageProvider != "local" || strings.TrimSpace(mat.StorageURL) == "" {
		http.Error(w, "Slide images only available for local-storage materials", http.StatusNotFound)
		return
	}
	slideName := fmt.Sprintf("slide-%03d.png", index)
	slidePath := filepath.Join(storage.UploadRoot(), mat.StorageURL+"_slides", slideName)
	data, err := os.ReadFile(slidePath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "Slide not found", http.StatusNotFound)
			return
		}
		log.Printf("GetMaterialSlideImage read %s: %v", slidePath, err)
		http.Error(w, "Failed to read slide", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// GetMaterialSlidePDF serves the consolidated deck.pdf for local-storage
// materials produced by the SCRUM-444/445 PDF pipeline. Uses http.ServeContent
// so Range requests work — PDF.js (SCRUM-446) range-fetches pages on demand
// rather than downloading the whole deck up front.
func (h *Handlers) GetMaterialSlidePDF(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	var sessionIDStr, materialIDStr string
	switch {
	case len(pathParts) >= 6 && pathParts[0] == "api" && pathParts[1] == "sessions" && pathParts[3] == "materials" && pathParts[5] == "slide-pdf":
		sessionIDStr = pathParts[2]
		materialIDStr = pathParts[4]
	case len(pathParts) >= 5 && pathParts[0] == "sessions" && pathParts[2] == "materials" && pathParts[4] == "slide-pdf":
		sessionIDStr = pathParts[1]
		materialIDStr = pathParts[3]
	default:
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(sessionIDStr)
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	materialID, err := uuid.Parse(materialIDStr)
	if err != nil {
		http.Error(w, "Invalid material ID", http.StatusBadRequest)
		return
	}
	mat, err := h.DB.GetMaterialByID(ctx, materialID)
	if err != nil || mat == nil {
		http.Error(w, "Material not found", http.StatusNotFound)
		return
	}
	if mat.SessionID != sessionID {
		http.Error(w, "Material not found", http.StatusNotFound)
		return
	}
	if !models.MaterialSupportsDerivedSlideDeck(mat) {
		http.Error(w, "Material not found", http.StatusNotFound)
		return
	}
	if mat.StorageProvider != "local" || strings.TrimSpace(mat.StorageURL) == "" {
		http.Error(w, "Slide PDF only available for local-storage materials", http.StatusNotFound)
		return
	}
	pdfPath := filepath.Join(storage.UploadRoot(), mat.StorageURL+"_slides", "deck.pdf")
	f, err := os.Open(pdfPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "Slide PDF not found", http.StatusNotFound)
			return
		}
		log.Printf("GetMaterialSlidePDF open %s: %v", pdfPath, err)
		http.Error(w, "Failed to read slide PDF", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		log.Printf("GetMaterialSlidePDF stat %s: %v", pdfPath, err)
		http.Error(w, "Failed to read slide PDF", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, "deck.pdf", info.ModTime(), f)
}
