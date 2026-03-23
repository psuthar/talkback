package handlers

import (
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

type UploadTranscriptFileResponse struct {
	JobID    string `json:"job_id"`
	Status   string `json:"status"`
	Message  string `json:"message,omitempty"`
}

// UploadTranscriptFile handles MP4 file uploads for transcript-only transcription
func (h *Handlers) UploadTranscriptFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID from URL path: /sessions/{id}/video/transcript/upload
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 5 || pathParts[0] != "sessions" || pathParts[2] != "video" || pathParts[3] != "transcript" || pathParts[4] != "upload" {
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
		// Parse size string like "500" (MB) to bytes
		var size int64
		if _, err := fmt.Sscanf(strings.TrimSpace(maxSizeEnv), "%d", &size); err == nil {
			maxSize = size * 1024 * 1024
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

	// Generate IDs and artifact up front so both paths share them.
	tempID := uuid.New()
	videoID := uuid.New()

	artifact, err := h.DB.CreateArtifact(r.Context(), sessionID, "Transcript Upload", nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create artifact: %v", err), http.StatusInternalServerError)
		return
	}

	var objectKey string
	var transcriptSourceURL string

	if h.Storage != nil {
		// R2 path: buffer to temp file then stream to R2.
		tmpFile, tmpErr := os.CreateTemp(os.TempDir(), "talkback-transcript-*.mp4")
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
			log.Printf("UploadTranscriptFile R2 Put: %v", putErr)
			http.Error(w, "Failed to upload file to storage", http.StatusInternalServerError)
			return
		}
		presigned, presignErr := h.Storage.PresignGet(r.Context(), objectKey, 4*time.Hour)
		if presignErr != nil {
			log.Printf("UploadTranscriptFile R2 PresignGet: %v", presignErr)
			http.Error(w, "Failed to generate presigned URL", http.StatusInternalServerError)
			return
		}
		transcriptSourceURL = presigned
	} else {
		// Local path (dev only): save to disk.
		storageDir := storage.SessionTranscriptsDir(sessionID)
		if mkErr := os.MkdirAll(storageDir, 0755); mkErr != nil {
			http.Error(w, fmt.Sprintf("Failed to create storage directory: %v", mkErr), http.StatusInternalServerError)
			return
		}
		objectKey = storage.SessionTranscriptPath(sessionID, tempID)
		filePath := filepath.Join(storageDir, tempID.String()+".mp4")
		if saveErr := utils.SaveFile(file, filePath); saveErr != nil {
			http.Error(w, fmt.Sprintf("Failed to save file: %v", saveErr), http.StatusInternalServerError)
			return
		}
		transcriptSourceURL = objectKey
	}

	// Create a minimal video source (just for transcript storage, not for playback).
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
		http.Error(w, fmt.Sprintf("Failed to create transcript job: %v", err), http.StatusInternalServerError)
		return
	}

	// Link job to video source
	if err := h.DB.UpdateVideoSourceTranscriptionJob(r.Context(), videoID, &job.ID); err != nil {
		log.Printf("Warning: Failed to link job to video source: %v", err)
	}

	// Enqueue job for processing
	if err := h.JobProcessor.Enqueue(r.Context(), job); err != nil {
		log.Printf("Warning: Failed to enqueue job: %v", err)
	}

	response := UploadTranscriptFileResponse{
		JobID:   job.ID.String(),
		Status:  string(job.Status),
		Message: "Transcript file uploaded. Transcription will begin shortly.",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}
