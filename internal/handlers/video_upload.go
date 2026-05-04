package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/storage"
	"github.com/psuthar/talkback/internal/utils"
)

type UploadVideoFileResponse struct {
	VideoSource   *models.VideoSource `json:"video_source"`
	RequiresUpload bool                `json:"requires_upload"`
	Message       string               `json:"message,omitempty"`
}

// UploadVideoFile handles MP4 file uploads for sessions
func (h *Handlers) UploadVideoFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID from URL path: /sessions/{id}/video/upload
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 4 || pathParts[0] != "sessions" || pathParts[2] != "video" || pathParts[3] != "upload" {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}

	sessionID, err := uuid.Parse(pathParts[1])
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	// Verify session exists
	_, err = h.DB.GetSession(r.Context(), sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Session not found: %v", err), http.StatusNotFound)
		return
	}

	// Parse multipart form - allow larger files (500MB default)
	maxSize := int64(500 * 1024 * 1024) // 500MB
	if maxSizeEnv := os.Getenv("MAX_VIDEO_UPLOAD_MB"); maxSizeEnv != "" {
		if parsed, err := parseSizeMB(maxSizeEnv); err == nil {
			maxSize = parsed
		}
	}

	err = r.ParseMultipartForm(maxSize)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse form: %v", err), http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get file: %v", err), http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Validate file is MP4
	contentType := header.Header.Get("Content-Type")
	ext := strings.ToLower(filepath.Ext(header.Filename))
	
	if contentType != "video/mp4" && ext != ".mp4" {
		http.Error(w, "File must be MP4 format (video/mp4 or .mp4 extension)", http.StatusBadRequest)
		return
	}

	// Generate video ID and artifact up front so both paths share them.
	videoID := uuid.New()

	artifact, err := h.DB.CreateArtifact(r.Context(), sessionID, "Video Upload", nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create artifact: %v", err), http.StatusInternalServerError)
		return
	}

	var objectKey string
	var transcriptSourceURL string

	if h.Storage != nil {
		// R2 path: buffer to temp file then stream to R2.
		tmpFile, tmpErr := os.CreateTemp(os.TempDir(), "talkback-video-*.mp4")
		if tmpErr != nil {
			http.Error(w, "Failed to create temp file", http.StatusInternalServerError)
			return
		}
		tmpPath := tmpFile.Name()
		defer os.Remove(tmpPath)
		if _, copyErr := io.Copy(tmpFile, file); copyErr != nil {
			_ = tmpFile.Close()
			http.Error(w, "Failed to buffer upload", http.StatusInternalServerError)
			return
		}
		if closeErr := tmpFile.Close(); closeErr != nil {
			http.Error(w, "Failed to close temp file", http.StatusInternalServerError)
			return
		}
		prefix := strings.TrimSuffix(strings.TrimSpace(os.Getenv("R2_PREFIX")), "/")
		objectKey = storage.BuildArtifactStorageKey(prefix, sessionID, artifact.ID, header.Filename)
		f, openErr := os.Open(tmpPath)
		if openErr != nil {
			http.Error(w, "Failed to open temp file for upload", http.StatusInternalServerError)
			return
		}
		_, _, putErr := h.Storage.Put(r.Context(), objectKey, f, contentType, header.Size)
		_ = f.Close()
		if putErr != nil {
			log.Printf("UploadVideoFile R2 Put: %v", putErr)
			http.Error(w, "Failed to upload file to storage", http.StatusInternalServerError)
			return
		}
		presigned, presignErr := h.Storage.PresignGet(r.Context(), objectKey, 4*time.Hour)
		if presignErr != nil {
			log.Printf("UploadVideoFile R2 PresignGet: %v", presignErr)
			http.Error(w, "Failed to generate presigned URL", http.StatusInternalServerError)
			return
		}
		transcriptSourceURL = presigned
	} else {
		// Local path (dev only): save to disk.
		storageDir := storage.SessionVideosDir(sessionID)
		if mkErr := os.MkdirAll(storageDir, 0755); mkErr != nil {
			http.Error(w, fmt.Sprintf("Failed to create storage directory: %v", mkErr), http.StatusInternalServerError)
			return
		}
		objectKey = storage.SessionVideoPath(sessionID, videoID, ".mp4")
		filePath := filepath.Join(storageDir, videoID.String()+".mp4")
		if saveErr := utils.SaveFile(file, filePath); saveErr != nil {
			http.Error(w, fmt.Sprintf("Failed to save file: %v", saveErr), http.StatusInternalServerError)
			return
		}
		transcriptSourceURL = objectKey
	}

	// SCRUM-295: also create a file_artifact row for this upload so the
	// SCRUM-271/272 primary system (which keys off file_artifacts.id) can
	// reference it. The file is already on disk/R2 by this point so the
	// artifact is born status=ready. Failure here is logged but does not
	// fail the upload — the video_source is still usable; just the
	// Make-primary affordance won't appear for this row.
	fileArtifactID := createVideoFileArtifact(r.Context(), h, sessionID, header.Filename, contentType, header.Size, h.Storage != nil, objectKey)

	// Create video source
	videoSource := &models.VideoSource{
		ID:                    videoID,
		ArtifactID:            artifact.ID,
		SessionID:             sessionID,
		Provider:              "other",
		PlaybackMode:          "direct",
		SourceType:            models.VideoSourceTypeUpload,
		StoredVideoObjectKey:  &objectKey,
		TranscriptStatus:      models.VideoTranscriptStatusPending,
		AutoTranscribeEnabled: true,
		FileArtifactID:        fileArtifactID,
	}

	if err := h.DB.CreateVideoSource(r.Context(), videoSource); err != nil {
		http.Error(w, fmt.Sprintf("Failed to create video source: %v", err), http.StatusInternalServerError)
		return
	}
	h.ensurePrimaryVideoIfNone(r.Context(), sessionID, videoID)

	// Enqueue transcription job.
	jobKey := utils.GenerateJobKey(videoID.String(), objectKey)
	job := &models.TranscriptJob{
		ID:            uuid.New(),
		VideoSourceID: videoID,
		SessionID:     sessionID,
		Status:        models.TranscriptJobStatusQueued,
		SourceURL:     transcriptSourceURL,
		JobKey:        jobKey,
		QueuedAt:      time.Now(),
	}

	if err := h.DB.CreateTranscriptJob(r.Context(), job); err != nil {
		log.Printf("Warning: Failed to create transcript job: %v", err)
	} else {
		// Link job to video source
		if err := h.DB.UpdateVideoSourceTranscriptionJob(r.Context(), videoID, &job.ID); err != nil {
			log.Printf("Warning: Failed to link job to video source: %v", err)
		}

		// Enqueue job for processing. JobProcessor is nil in test harnesses
		// that don't drive transcription end-to-end; skip enqueue cleanly so
		// upload-only tests can exercise the synchronous bookkeeping.
		if h.JobProcessor != nil {
			if err := h.JobProcessor.Enqueue(r.Context(), job); err != nil {
				log.Printf("Warning: Failed to enqueue job: %v", err)
			}
		}
	}

	response := UploadVideoFileResponse{
		VideoSource:   videoSource,
		RequiresUpload: false,
		Message:       "Video uploaded successfully. Transcription will begin shortly.",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}

// parseSizeMB parses a size string like "500" (MB) to bytes
func parseSizeMB(sizeStr string) (int64, error) {
	sizeStr = strings.TrimSpace(sizeStr)
	var size int64
	if _, err := fmt.Sscanf(sizeStr, "%d", &size); err != nil {
		return 0, err
	}
	return size * 1024 * 1024, nil
}

// createVideoFileArtifact (SCRUM-295) creates a file_artifacts row for a
// just-uploaded MP4 so the SCRUM-271/272 primary system can reference it.
// The file is already on disk/R2 by this point so status is born "ready".
// Returns the new artifact's id, or nil if creation failed (logged as a
// warning so the upload still succeeds — just without a Make-primary id).
func createVideoFileArtifact(ctx context.Context, h *Handlers, sessionID uuid.UUID, filename, contentType string, sizeBytes int64, useR2 bool, storageKey string) *uuid.UUID {
	id := uuid.New()
	provider := "local"
	bucket := "local"
	if useR2 {
		provider = "r2"
		if envBucket := strings.TrimSpace(os.Getenv("R2_BUCKET")); envBucket != "" {
			bucket = envBucket
		} else {
			bucket = "talkback-r2-bucket"
		}
	}
	ct := strings.TrimSpace(contentType)
	if ct == "" {
		ct = "video/mp4"
	}
	var size *int64
	if sizeBytes > 0 {
		size = &sizeBytes
	}
	var fn *string
	if filename != "" {
		f := filename
		fn = &f
	}
	fa := &models.FileArtifact{
		ID:              id,
		SessionID:       &sessionID,
		Kind:            models.FileArtifactKindVideo,
		Filename:        fn,
		ContentType:     ct,
		SizeBytes:       size,
		StorageProvider: provider,
		StorageBucket:   bucket,
		StorageKey:      storageKey,
		Status:          models.FileArtifactStatusReady,
	}
	if err := h.DB.CreateFileArtifact(ctx, fa); err != nil {
		log.Printf("UploadVideoFile create file_artifact: %v", err)
		return nil
	}
	return &id
}
