package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

type TranscriptJobStatusResponse struct {
	JobID         string  `json:"job_id"`
	Status        string  `json:"status"`
	TranscriptText *string `json:"transcript_text,omitempty"`
	ErrorMessage  *string `json:"error_message,omitempty"`
	VideoSourceID string  `json:"video_source_id,omitempty"`
}

// GetTranscriptJobStatus retrieves transcript job status and transcript text by job ID
func (h *Handlers) GetTranscriptJobStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID and job ID from URL path: /sessions/{id}/transcript-jobs/{job_id}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 4 || pathParts[0] != "sessions" || pathParts[2] != "transcript-jobs" {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}

	sessionID, err := uuid.Parse(pathParts[1])
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	jobID, err := uuid.Parse(pathParts[3])
	if err != nil {
		http.Error(w, "Invalid job ID", http.StatusBadRequest)
		return
	}

	// Get transcript job
	job, err := h.DB.GetTranscriptJob(r.Context(), jobID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Transcript job not found: %v", err), http.StatusNotFound)
		return
	}

	// Verify job belongs to session
	if job.SessionID != sessionID {
		http.Error(w, "Transcript job does not belong to this session", http.StatusForbidden)
		return
	}

	// Get video source to retrieve transcript text
	var transcriptText *string
	if job.Status == "completed" {
		videoSource, err := h.DB.GetVideoSourceByID(r.Context(), job.VideoSourceID)
		if err == nil && videoSource.TranscriptText != nil {
			transcriptText = videoSource.TranscriptText
		}
	}

	response := TranscriptJobStatusResponse{
		JobID:         job.ID.String(),
		Status:        string(job.Status),
		TranscriptText: transcriptText,
		ErrorMessage:  job.ErrorMessage,
		VideoSourceID: job.VideoSourceID.String(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}
