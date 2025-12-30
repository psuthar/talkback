package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/utils"
)

type Handlers struct {
	DB *database.DB
}

func NewHandlers(db *database.DB) *Handlers {
	return &Handlers{DB: db}
}

type CreateArtifactRequest struct {
	Title       string  `json:"title"`
	Description *string `json:"description,omitempty"`
}

type CreateArtifactResponse struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Description *string `json:"description,omitempty"`
	Status      string  `json:"status"`
}

func (h *Handlers) CreateArtifact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CreateArtifactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if req.Title == "" {
		http.Error(w, "Title is required", http.StatusBadRequest)
		return
	}

	artifact, err := h.DB.CreateArtifact(r.Context(), req.Title, req.Description)
	if err != nil {
		log.Printf("Error creating artifact: %v", err)
		http.Error(w, fmt.Sprintf("Failed to create artifact: %v", err), http.StatusInternalServerError)
		return
	}

	response := CreateArtifactResponse{
		ID:          artifact.ID.String(),
		Title:       artifact.Title,
		Description: artifact.Description,
		Status:      string(artifact.Status),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}

func (h *Handlers) GetArtifact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract artifact ID from URL path (e.g., /artifacts/{id})
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 2 || pathParts[0] != "artifacts" {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}

	artifactID, err := uuid.Parse(pathParts[1])
	if err != nil {
		http.Error(w, "Invalid artifact ID", http.StatusBadRequest)
		return
	}

	artifact, err := h.DB.GetArtifact(r.Context(), artifactID)
	if err != nil {
		log.Printf("Error getting artifact: %v", err)
		http.Error(w, fmt.Sprintf("Failed to get artifact: %v", err), http.StatusInternalServerError)
		return
	}

	// Get related materials and video sources
	materials, err := h.DB.GetMaterialsByArtifactID(r.Context(), artifactID)
	if err != nil {
		log.Printf("Warning: Failed to get materials for artifact %s: %v", artifactID, err)
		materials = []*models.Material{} // Return empty slice on error
	}

	videoSource, err := h.DB.GetVideoSourceByArtifactID(r.Context(), artifactID)
	if err != nil {
		log.Printf("Warning: Failed to get video source for artifact %s: %v", artifactID, err)
		videoSource = nil
	}

	response := map[string]interface{}{
		"artifact":      artifact,
		"materials":     materials,
		"video_sources": []interface{}{},
	}

	if videoSource != nil {
		response["video_sources"] = []interface{}{videoSource}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func (h *Handlers) UploadMaterial(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract artifact ID from URL path
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 3 || pathParts[0] != "artifacts" || pathParts[2] != "materials" {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}

	artifactID, err := uuid.Parse(pathParts[1])
	if err != nil {
		http.Error(w, "Invalid artifact ID", http.StatusBadRequest)
		return
	}

	// Parse multipart form
	err = r.ParseMultipartForm(10 << 20) // 10 MB max
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

	// Create data directory if it doesn't exist
	dataDir := "./data"
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		http.Error(w, fmt.Sprintf("Failed to create data directory: %v", err), http.StatusInternalServerError)
		return
	}

	// Get MIME type
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// Get kind from form (default to "document")
	kind := r.FormValue("kind")
	if kind == "" {
		kind = "document"
	}

	// Create uploads directory structure
	uploadsDir := filepath.Join("data", "uploads", artifactID.String())
	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		http.Error(w, fmt.Sprintf("Failed to create uploads directory: %v", err), http.StatusInternalServerError)
		return
	}

	// Save file
	filePath := filepath.Join(uploadsDir, header.Filename)
	if err := utils.SaveFile(file, filePath); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save file: %v", err), http.StatusInternalServerError)
		return
	}

	// Store file under ./data/uploads/{artifact_id}/...
	storageURL := filepath.Join("data", "uploads", artifactID.String(), header.Filename)

	// Extract text if content-type is text/plain
	textStatus := models.MaterialTextStatusPending
	var extractedText *string
	if contentType == "text/plain" {
		// Read file content
		content, err := os.ReadFile(filePath)
		if err == nil {
			text := string(content)
			extractedText = &text
			textStatus = models.MaterialTextStatusReady
		}
	} else if contentType == "application/pdf" {
		// PDF extraction is TODO - set status to failed for now
		textStatus = models.MaterialTextStatusFailed
		log.Printf("PDF text extraction not yet implemented for file: %s", header.Filename)
	}

	// Create material record
	material := &models.Material{
		ID:            uuid.New(),
		ArtifactID:    artifactID,
		Kind:          kind,
		Filename:      header.Filename,
		ContentType:   contentType,
		StorageURL:    storageURL,
		TextStatus:    textStatus,
		ExtractedText: extractedText,
	}

	if err := h.DB.CreateMaterial(r.Context(), material); err != nil {
		// Clean up file if DB insert fails
		os.Remove(filePath)
		http.Error(w, fmt.Sprintf("Failed to create material record: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(material)
}

type AttachVideoURLRequest struct {
	Provider string `json:"provider"`
	VideoURL string `json:"video_url"`
}

func (h *Handlers) AttachVideoURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract artifact ID from URL path
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 3 || pathParts[0] != "artifacts" || pathParts[2] != "video" {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}

	artifactID, err := uuid.Parse(pathParts[1])
	if err != nil {
		http.Error(w, "Invalid artifact ID", http.StatusBadRequest)
		return
	}

	var req AttachVideoURLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if req.VideoURL == "" {
		http.Error(w, "video_url is required", http.StatusBadRequest)
		return
	}

	if req.Provider == "" {
		req.Provider = "other"
	}

	videoSource := &models.VideoSource{
		ID:               uuid.New(),
		ArtifactID:       artifactID,
		Provider:         req.Provider,
		VideoURL:         req.VideoURL,
		TranscriptStatus: models.VideoTranscriptStatusMissing,
	}

	if err := h.DB.CreateVideoSource(r.Context(), videoSource); err != nil {
		log.Printf("Error creating video source: %v", err)
		http.Error(w, fmt.Sprintf("Failed to attach video URL: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(videoSource)
}

type UploadTranscriptRequest struct {
	TranscriptText string `json:"transcript_text"`
}

func (h *Handlers) UploadTranscript(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract artifact ID and video ID from URL path: /artifacts/{id}/video/{video_id}/transcript
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 5 || pathParts[0] != "artifacts" || pathParts[2] != "video" || pathParts[4] != "transcript" {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}

	artifactID, err := uuid.Parse(pathParts[1])
	if err != nil {
		http.Error(w, "Invalid artifact ID", http.StatusBadRequest)
		return
	}

	videoID, err := uuid.Parse(pathParts[3])
	if err != nil {
		http.Error(w, "Invalid video ID", http.StatusBadRequest)
		return
	}

	var req UploadTranscriptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if req.TranscriptText == "" {
		http.Error(w, "transcript_text is required", http.StatusBadRequest)
		return
	}

	// Verify video source belongs to artifact
	videoSource, err := h.DB.GetVideoSourceByID(r.Context(), videoID)
	if err != nil {
		http.Error(w, "Video source not found", http.StatusNotFound)
		return
	}

	if videoSource.ArtifactID != artifactID {
		http.Error(w, "Video source does not belong to this artifact", http.StatusBadRequest)
		return
	}

	if err := h.DB.UpdateVideoSourceTranscript(r.Context(), videoID, req.TranscriptText); err != nil {
		log.Printf("Error updating transcript: %v", err)
		http.Error(w, fmt.Sprintf("Failed to upload transcript: %v", err), http.StatusInternalServerError)
		return
	}

	// Get updated video source
	updatedVideoSource, _ := h.DB.GetVideoSourceByID(r.Context(), videoID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(updatedVideoSource)
}

// Ingestion functionality removed for Phase 1
// Will be re-implemented in a later phase with embeddings
