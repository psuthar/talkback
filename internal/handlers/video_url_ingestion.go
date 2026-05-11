package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/utils"
)

type IngestVideoFromURLRequest struct {
	URL string `json:"url"`
}

type IngestVideoFromURLResponse struct {
	VideoSource   *models.VideoSource `json:"video_source"`
	RequiresUpload bool               `json:"requires_upload"`
	Message       string              `json:"message,omitempty"`
}

// IngestVideoFromURL handles smart URL ingestion for video sources
func (h *Handlers) IngestVideoFromURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID from URL path: /sessions/{id}/video/from-url
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 4 || pathParts[0] != "sessions" || pathParts[2] != "video" || pathParts[3] != "from-url" {
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

	// Parse request body
	var req IngestVideoFromURLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if req.URL == "" {
		http.Error(w, "url is required", http.StatusBadRequest)
		return
	}

	// Probe URL to check if it's a direct media file
	isMedia, contentType, err := utils.ProbeMediaURL(r.Context(), req.URL)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to probe URL: %v", err), http.StatusBadRequest)
		return
	}

	if !isMedia {
		http.Error(w, fmt.Sprintf("URL does not appear to be a direct media file (Content-Type: %s). Please provide a direct MP4 URL or upload the file.", contentType), http.StatusBadRequest)
		return
	}

	// Create artifact for this video
	artifact, err := h.DB.CreateArtifact(r.Context(), sessionID, "Video from URL", nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create artifact: %v", err), http.StatusInternalServerError)
		return
	}

	// Generate video ID
	videoID := uuid.New()

	// Download and store video
	objectKey, err := utils.DownloadVideoToStorage(r.Context(), req.URL, sessionID, videoID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to download video: %v", err), http.StatusInternalServerError)
		return
	}

	// Create video source
	videoSource := &models.VideoSource{
		ID:                   videoID,
		ArtifactID:           artifact.ID,
		SessionID:            sessionID,
		Provider:             "other",
		PlaybackMode:         "direct",
		MediaURL:             &req.URL,
		SourceType:           models.VideoSourceTypeDirectURL,
		StoredVideoObjectKey: &objectKey,
		OriginalURL:          &req.URL,
		TranscriptStatus:     models.VideoTranscriptStatusPending,
		AutoTranscribeEnabled: true,
	}

	if err := h.DB.CreateVideoSource(r.Context(), videoSource); err != nil {
		// Clean up downloaded file if DB insert fails
		// Note: We could add cleanup here, but for now just log
		log.Printf("Warning: Failed to create video source after download, file may be orphaned: %v", err)
		http.Error(w, fmt.Sprintf("Failed to create video source: %v", err), http.StatusInternalServerError)
		return
	}

	// Enqueue transcription job (use local file path as source URL)
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

	response := IngestVideoFromURLResponse{
		VideoSource:   videoSource,
		RequiresUpload: false,
		Message:       "Video downloaded successfully. Transcription will begin shortly.",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}
