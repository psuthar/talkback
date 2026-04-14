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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/auth"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/storage"
	"github.com/psuthar/talkback/internal/utils"
)

// MaterialSlidesResponse is the JSON response for GET .../materials/{material_id}/slides.
type MaterialSlidesResponse struct {
	MaterialID string                 `json:"material_id"`
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
	switch {
	case isImage:
		textStatus = models.MaterialTextStatusReady
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
		// Other office types (e.g. .ppt, .xls): extraction runs async via job processor.
		textStatus = models.MaterialTextStatusPending
	case isVideoForTranscription(ext, contentType):
		// Video: transcript is handled by VideoSource + job; material is just the file reference — show ready so it doesn't stay "pending"
		textStatus = models.MaterialTextStatusReady
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
	needsExtraction := ((contentType == "application/pdf" || ext == ".pdf") || isOfficeFile(ext, contentType)) &&
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
func (h *Handlers) tryGenerateAndStoreSlides(ctx context.Context, localPath string, artifactKey string) {
	pipelineStart := time.Now()
	log.Printf("slide generation started for %s (key=%s)", localPath, artifactKey)
	h.clearSlidesFailureMarkerStorage(ctx, artifactKey)
	tConv := time.Now()
	slides, err := utils.ConvertSlidesToPNGsWithLibreOffice(localPath)
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
func (h *Handlers) tryGenerateAndStoreSlidesLocal(_ context.Context, localPath string) {
	pipelineStart := time.Now()
	log.Printf("slide generation started (local) for %s", localPath)
	clearSlidesFailureMarkerLocal(localPath)
	tConv := time.Now()
	slides, err := utils.ConvertSlidesToPNGsWithLibreOffice(localPath)
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

// GetSlidesStatus returns "ready", "processing", or "failed" for PPT/PPTX materials using the slide pipeline.
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

	// Local storage: read manifest from disk and return slide-image API URLs
	if mat.StorageProvider == "local" && strings.TrimSpace(mat.StorageURL) != "" {
		manifestPath := filepath.Join(storage.UploadRoot(), mat.StorageURL+"_slides", "manifest.json")
		manifestBytes, err := os.ReadFile(manifestPath)
		if err != nil {
			log.Printf("slides manifest missing or unreadable for material %s path %s: %v", materialID, manifestPath, err)
			writeJSON(w, http.StatusOK, resp)
			return
		}
		var manifest utils.SlideManifest
		if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
			log.Printf("failed to decode slides manifest for material %s: %v", materialID, err)
			writeJSON(w, http.StatusOK, resp)
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

	// R2 storage: read manifest from storage and presign each slide
	if h.Storage == nil || strings.TrimSpace(mat.StorageKey) == "" {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	manifestKey := storage.SlidesManifestKeyFromArtifactKey(mat.StorageKey)
	rc, err := h.Storage.Get(ctx, manifestKey)
	if err != nil {
		log.Printf("slides manifest missing or unreadable for material %s key %s: %v (slide generation may still be running or have failed — check logs for 'slide generation started' / 'slides conversion failed' / 'generated N derived slides')", materialID, manifestKey, err)
		writeJSON(w, http.StatusOK, resp)
		return
	}
	defer rc.Close()
	var manifest utils.SlideManifest
	if err := json.NewDecoder(rc).Decode(&manifest); err != nil {
		log.Printf("failed to decode slides manifest for material %s key %s: %v", materialID, manifestKey, err)
		writeJSON(w, http.StatusOK, resp)
		return
	}
	presignTTL := time.Hour
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
