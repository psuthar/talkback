package handlers

import (
	"encoding/json"
	"fmt"
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

	storageDir := storage.SessionVideosDir(sessionID)
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		http.Error(w, fmt.Sprintf("Failed to create storage directory: %v", err), http.StatusInternalServerError)
		return
	}

	// Generate video ID
	videoID := uuid.New()
	
	// Save file
	filePath := filepath.Join(storageDir, videoID.String()+".mp4")
	if err := utils.SaveFile(file, filePath); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save file: %v", err), http.StatusInternalServerError)
		return
	}

	objectKey := storage.SessionVideoPath(sessionID, videoID, ".mp4")

	// Create artifact for this video (required by current schema)
	// Use a default artifact or create one
	artifact, err := h.DB.CreateArtifact(r.Context(), sessionID, "Video Upload", nil)
	if err != nil {
		// Clean up file if artifact creation fails
		os.Remove(filePath)
		http.Error(w, fmt.Sprintf("Failed to create artifact: %v", err), http.StatusInternalServerError)
		return
	}

	// Create video source
	videoSource := &models.VideoSource{
		ID:                   videoID,
		ArtifactID:           artifact.ID,
		SessionID:            sessionID,
		Provider:             "other",
		PlaybackMode:         "direct",
		SourceType:           models.VideoSourceTypeUpload,
		StoredVideoObjectKey: &objectKey,
		TranscriptStatus:     models.VideoTranscriptStatusPending,
		AutoTranscribeEnabled: true,
	}

	if err := h.DB.CreateVideoSource(r.Context(), videoSource); err != nil {
		// Clean up file if DB insert fails
		os.Remove(filePath)
		http.Error(w, fmt.Sprintf("Failed to create video source: %v", err), http.StatusInternalServerError)
		return
	}
	h.ensurePrimaryVideoIfNone(r.Context(), sessionID, videoID)

	// Enqueue transcription job (use local file path as source URL)
	// Create a transcript job for the local file
	jobKey := utils.GenerateJobKey(videoID.String(), objectKey)
	job := &models.TranscriptJob{
		ID:            uuid.New(),
		VideoSourceID: videoID,
		SessionID:     sessionID,
		Status:        models.TranscriptJobStatusQueued,
		SourceURL:     objectKey, // Use local file path instead of URL
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

		// Enqueue job for processing
		if err := h.JobProcessor.Enqueue(r.Context(), job); err != nil {
			log.Printf("Warning: Failed to enqueue job: %v", err)
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
