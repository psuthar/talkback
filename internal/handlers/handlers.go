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
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/rag"
	"github.com/psuthar/talkback/internal/storage"
	"github.com/psuthar/talkback/internal/utils"
)

type Handlers struct {
	DB           *database.DB
	JobProcessor *utils.JobProcessor
	Hub          *SessionHub
	Storage      storage.Interface // R2 for file_artifacts (presigned PUT/GET)
}

func NewHandlers(db *database.DB, jobProcessor *utils.JobProcessor, store storage.Interface) *Handlers {
	hub := NewSessionHub()
	go hub.Run()
	return &Handlers{
		DB:           db,
		JobProcessor: jobProcessor,
		Hub:          hub,
		Storage:      store,
	}
}

type CreateArtifactRequest struct {
	SessionID   string  `json:"session_id"` // Required: artifacts belong to sessions
	Title       string  `json:"title"`
	Description *string `json:"description,omitempty"`
}

type CreateArtifactResponse struct {
	ID          string  `json:"id"`
	SessionID   string  `json:"session_id"`
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

	if req.SessionID == "" {
		http.Error(w, "session_id is required", http.StatusBadRequest)
		return
	}
	if req.Title == "" {
		http.Error(w, "Title is required", http.StatusBadRequest)
		return
	}

	sessionID, err := uuid.Parse(req.SessionID)
	if err != nil {
		http.Error(w, "Invalid session_id", http.StatusBadRequest)
		return
	}

	// Verify session exists
	_, err = h.DB.GetSession(r.Context(), sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Session not found: %v", err), http.StatusNotFound)
		return
	}

	artifact, err := h.DB.CreateArtifact(r.Context(), sessionID, req.Title, req.Description)
	if err != nil {
		log.Printf("Error creating artifact: %v", err)
		http.Error(w, fmt.Sprintf("Failed to create artifact: %v", err), http.StatusInternalServerError)
		return
	}

	response := CreateArtifactResponse{
		ID:          artifact.ID.String(),
		SessionID:   artifact.SessionID.String(),
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

	// Optionally include questions if ?include_questions=true
	if r.URL.Query().Get("include_questions") == "true" {
		questions, answers, err := h.DB.GetQuestionsByArtifactID(r.Context(), artifactID, 20)
		if err != nil {
			log.Printf("Warning: Failed to get questions: %v", err)
		} else {
			response["questions"] = questions
			response["answers"] = answers
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// isOfficeFile returns true if the file is a supported Office document (docx, xlsx, pptx) by extension or content-type.
func isOfficeFile(ext, contentType string) bool {
	if ext == ".docx" || ext == ".xlsx" || ext == ".pptx" {
		return true
	}
	ct := strings.ToLower(contentType)
	return strings.Contains(ct, "openxmlformats-officedocument") ||
		strings.Contains(ct, "vnd.openxmlformats-officedocument")
}

// deriveMaterialKind returns material kind from file extension and content-type for grouping in UI.
func deriveMaterialKind(ext, contentType string, isImage bool) string {
	if isImage {
		return "slides"
	}
	ct := strings.ToLower(contentType)
	switch ext {
	case ".pdf":
		return "document"
	case ".txt", ".md":
		return "document"
	case ".docx", ".doc":
		return "document"
	case ".pptx", ".ppt":
		return "slides"
	case ".xlsx", ".xls", ".csv":
		return "document"
	}
	if ct == "text/plain" || strings.HasPrefix(ct, "text/") {
		return "document"
	}
	if strings.HasPrefix(ct, "application/pdf") {
		return "document"
	}
	return "other"
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

	// Get artifact to retrieve session_id
	artifact, err := h.DB.GetArtifact(r.Context(), artifactID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Artifact not found: %v", err), http.StatusNotFound)
		return
	}
	if artifact.SessionID == uuid.Nil {
		http.Error(w, "Artifact has no session; uploads require a session-scoped artifact", http.StatusBadRequest)
		return
	}
	sessionID := artifact.SessionID

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

	exists, err := h.DB.ExistsMaterialWithFilenameInSession(r.Context(), sessionID, header.Filename)
	if err != nil {
		log.Printf("UploadMaterial check duplicate: %v", err)
		http.Error(w, "Failed to check existing files", http.StatusInternalServerError)
		return
	}
	if exists {
		http.Error(w, fmt.Sprintf("A file named %q is already in this session", header.Filename), http.StatusConflict)
		return
	}

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	storageURL := storage.SessionArtifactPath(sessionID, header.Filename)
	ext := strings.ToLower(filepath.Ext(header.Filename))
	isImage := strings.HasPrefix(contentType, "image/") ||
		ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".gif" || ext == ".webp" || ext == ".bmp" || ext == ".svg"
	kind := deriveMaterialKind(ext, contentType, isImage)

	var filePath string
	var storageProvider string
	var storageKey string

	if h.Storage != nil {
		tmpDir := os.TempDir()
		tmpFile, err := os.CreateTemp(tmpDir, "talkback-upload-*")
		if err != nil {
			http.Error(w, "Failed to create temp file", http.StatusInternalServerError)
			return
		}
		filePath = tmpFile.Name()
		defer func() { _ = os.Remove(filePath) }()
		if _, err := io.Copy(tmpFile, file); err != nil {
			_ = tmpFile.Close()
			http.Error(w, "Failed to write temp file", http.StatusInternalServerError)
			return
		}
		if err := tmpFile.Close(); err != nil {
			http.Error(w, "Failed to close temp file", http.StatusInternalServerError)
			return
		}
		prefix := strings.TrimSuffix(strings.TrimSpace(os.Getenv("R2_PREFIX")), "/")
		storageKey = storage.BuildArtifactStorageKey(prefix, sessionID, artifactID, header.Filename)
		f, err := os.Open(filePath)
		if err != nil {
			http.Error(w, "Failed to open temp file for upload", http.StatusInternalServerError)
			return
		}
		_, _, err = h.Storage.Put(r.Context(), storageKey, f, contentType)
		_ = f.Close()
		if err != nil {
			log.Printf("UploadMaterial R2 Put: %v", err)
			http.Error(w, "Failed to upload file to storage", http.StatusInternalServerError)
			return
		}
		storageProvider = "r2"
	} else {
		uploadsDir := storage.SessionUploadsAbsDir(sessionID)
		log.Printf("UploadMaterial: storing to %s", uploadsDir)
		if err := os.MkdirAll(uploadsDir, 0755); err != nil {
			http.Error(w, fmt.Sprintf("Failed to create uploads directory: %v", err), http.StatusInternalServerError)
			return
		}
		filePath = filepath.Join(uploadsDir, header.Filename)
		if err := utils.SaveFile(file, filePath); err != nil {
			http.Error(w, fmt.Sprintf("Failed to save file: %v", err), http.StatusInternalServerError)
			return
		}
		storageProvider = "local"
	}

	textStatus := models.MaterialTextStatusPending
	var extractedText *string
	switch {
	case isImage:
		textStatus = models.MaterialTextStatusReady
	case contentType == "text/plain" || ext == ".txt" || ext == ".md":
		content, err := os.ReadFile(filePath)
		if err == nil {
			text := string(content)
			extractedText = &text
			textStatus = models.MaterialTextStatusReady
		} else {
			textStatus = models.MaterialTextStatusFailed
			log.Printf("Failed to read text file %s: %v", header.Filename, err)
		}
	case contentType == "application/pdf" || ext == ".pdf":
		text, err := utils.ExtractTextFromFile(filePath)
		if err != nil {
			textStatus = models.MaterialTextStatusFailed
			log.Printf("PDF text extraction failed for file %s: %v", header.Filename, err)
		} else if strings.TrimSpace(text) == "" {
			textStatus = models.MaterialTextStatusFailed
			log.Printf("PDF text extraction produced empty text for file %s", header.Filename)
		} else {
			extractedText = &text
			textStatus = models.MaterialTextStatusReady
		}
	case isOfficeFile(ext, contentType):
		text, err := utils.ExtractTextFromFile(filePath)
		if err != nil {
			textStatus = models.MaterialTextStatusFailed
			log.Printf("Office extraction failed for file %s: %v", header.Filename, err)
		} else if strings.TrimSpace(text) == "" {
			textStatus = models.MaterialTextStatusFailed
			log.Printf("Office extraction produced empty text for file %s", header.Filename)
		} else {
			extractedText = &text
			textStatus = models.MaterialTextStatusReady
		}
	}

	sizeBytes := header.Size
	material := &models.Material{
		ID:              uuid.New(),
		ArtifactID:      artifactID,
		SessionID:       artifact.SessionID,
		Kind:            kind,
		Filename:        header.Filename,
		ContentType:     contentType,
		StorageURL:      storageURL,
		StorageProvider: storageProvider,
		StorageKey:      storageKey,
		SizeBytes:       &sizeBytes,
		TextStatus:      textStatus,
		ExtractedText:   extractedText,
	}

	if err := h.DB.CreateMaterial(r.Context(), material); err != nil {
		if storageProvider == "r2" && storageKey != "" && h.Storage != nil {
			_ = h.Storage.Delete(r.Context(), storageKey)
		}
		if storageProvider == "local" && filePath != "" {
			_ = os.Remove(filePath)
		}
		http.Error(w, fmt.Sprintf("Failed to create material record: %v", err), http.StatusInternalServerError)
		return
	}

	if material.ExtractedText != nil && *material.ExtractedText != "" {
		rag.IndexSessionAsync(material.SessionID, h.DB, h.Storage)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(material)
}

// ServeMaterialFile serves a material file by ID (GET /artifacts/:artifactId/materials/:materialId/file).
func (h *Handlers) ServeMaterialFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) != 5 || pathParts[0] != "artifacts" || pathParts[2] != "materials" || pathParts[4] != "file" {
		http.Error(w, "Invalid path", http.StatusNotFound)
		return
	}
	artifactID, err := uuid.Parse(pathParts[1])
	if err != nil {
		http.Error(w, "Invalid artifact ID", http.StatusBadRequest)
		return
	}
	materialID, err := uuid.Parse(pathParts[3])
	if err != nil {
		http.Error(w, "Invalid material ID", http.StatusBadRequest)
		return
	}
	mat, err := h.DB.GetMaterialByID(r.Context(), materialID)
	if err != nil || mat == nil {
		http.Error(w, "Material not found", http.StatusNotFound)
		return
	}
	if mat.ArtifactID != artifactID {
		http.Error(w, "Material not found", http.StatusNotFound)
		return
	}
	if mat.DeletedAt != nil {
		http.Error(w, "File has been deleted", http.StatusGone)
		return
	}
	if mat.StorageProvider == "r2" && mat.StorageKey != "" && h.Storage != nil {
		ttl := time.Hour
		presignedURL, err := h.Storage.PresignGet(r.Context(), mat.StorageKey, ttl)
		if err != nil {
			log.Printf("ServeMaterialFile PresignGet: %v", err)
			http.Error(w, "Failed to generate download URL", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, presignedURL, http.StatusFound)
		return
	}
	path := filepath.Join(storage.UploadRoot(), filepath.FromSlash(mat.StorageURL))
	f, err := os.Open(path)
	if err != nil {
		log.Printf("ServeMaterialFile: open %s: %v", path, err)
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	ct := mat.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	http.ServeContent(w, r, mat.Filename, info.ModTime(), f)
}

type AttachVideoURLRequest struct {
	Provider        string  `json:"provider"`
	VideoURL        string  `json:"video_url"`     // Deprecated: backward compatibility
	PlaybackMode    string  `json:"playback_mode"` // 'embed' or 'direct'
	EmbedURL        string  `json:"embed_url"`     // For embed mode
	MediaURL        string  `json:"media_url"`     // For direct mode (MP4/WebM)
	PosterURL       string  `json:"poster_url"`    // Optional poster image
	DurationSeconds *int    `json:"duration_seconds,omitempty"`
	LoomPassword    *string `json:"loom_password,omitempty"` // Optional password for password-protected Loom videos
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

	// Get artifact to retrieve session_id
	artifact, err := h.DB.GetArtifact(r.Context(), artifactID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Artifact not found: %v", err), http.StatusNotFound)
		return
	}

	var req AttachVideoURLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	// Determine playback mode and validate
	playbackMode := req.PlaybackMode
	if playbackMode == "" {
		// Backward compatibility: if video_url is provided, treat as embed
		if req.VideoURL != "" {
			playbackMode = "embed"
			req.EmbedURL = req.VideoURL
		} else {
			playbackMode = "embed" // Default
		}
	}

	// Validate based on playback mode
	switch playbackMode {
	case "embed":
		if req.EmbedURL == "" && req.VideoURL == "" {
			http.Error(w, "embed_url is required for embed playback mode", http.StatusBadRequest)
			return
		}
		if req.EmbedURL == "" {
			req.EmbedURL = req.VideoURL // Backward compatibility
		}
	case "direct":
		if req.MediaURL == "" {
			http.Error(w, "media_url is required for direct playback mode", http.StatusBadRequest)
			return
		}
	default:
		http.Error(w, "playback_mode must be 'embed' or 'direct'", http.StatusBadRequest)
		return
	}

	if req.Provider == "" {
		req.Provider = "other"
	}

	// Set video_url for backward compatibility (use embed_url or media_url)
	videoURL := req.VideoURL
	if videoURL == "" {
		if playbackMode == "embed" {
			videoURL = req.EmbedURL
		} else {
			videoURL = req.MediaURL
		}
	}

	var embedURLPtr *string
	if req.EmbedURL != "" {
		embedURLPtr = &req.EmbedURL
	}

	var mediaURLPtr *string
	if req.MediaURL != "" {
		mediaURLPtr = &req.MediaURL
	}

	var posterURLPtr *string
	if req.PosterURL != "" {
		posterURLPtr = &req.PosterURL
	}

	videoSource := &models.VideoSource{
		ID:               uuid.New(),
		ArtifactID:       artifactID,
		SessionID:        artifact.SessionID,
		Provider:         req.Provider,
		VideoURL:         videoURL, // Keep for backward compatibility
		PlaybackMode:     playbackMode,
		EmbedURL:         embedURLPtr,
		MediaURL:         mediaURLPtr,
		DurationSeconds:  req.DurationSeconds,
		PosterURL:        posterURLPtr,
		TranscriptStatus: models.VideoTranscriptStatusMissing,
	}

	if err := h.DB.CreateVideoSource(r.Context(), videoSource); err != nil {
		log.Printf("Error creating video source: %v", err)
		http.Error(w, fmt.Sprintf("Failed to attach video URL: %v", err), http.StatusInternalServerError)
		return
	}

	// Auto-transcribe Loom videos using Whisper
	// First try Loom API if available (faster), otherwise use Whisper
	if req.Provider == "loom" {
		loomURL := videoURL
		if req.EmbedURL != "" {
			loomURL = req.EmbedURL
		}

		// Try Loom API first if available (faster)
		if utils.CanFetchLoomTranscript() {
			log.Printf("Attempting to fetch transcript from Loom API for video: %s", loomURL)
			transcript, err := utils.FetchLoomTranscript(r.Context(), loomURL)
			if err == nil && transcript != "" {
				// Successfully fetched from Loom API - save directly
				if err := h.DB.UpdateVideoSourceTranscript(r.Context(), videoSource.ID, transcript); err != nil {
					log.Printf("Failed to save transcript from Loom API: %v", err)
				} else {
					log.Printf("Successfully fetched and saved transcript from Loom API")
					h.DB.UpdateVideoSourceTranscriptionSource(r.Context(), videoSource.ID, "loom_api")
					// Fetch updated video source to include transcript in response
					updatedVideoSource, err := h.DB.GetVideoSourceByID(r.Context(), videoSource.ID)
					if err == nil {
						videoSource = updatedVideoSource
					}
				}
			} else {
				log.Printf("Loom API transcript fetch failed or unavailable, will try Whisper auto-transcription")
			}
		}

		// Enqueue Whisper-based auto-transcription job (if not already completed via Loom API)
		if videoSource.TranscriptStatus == models.VideoTranscriptStatusMissing {
			if h.JobProcessor != nil {
				log.Printf("Enqueueing Whisper auto-transcription job for Loom video: %s", loomURL)
				if err := utils.ProcessTranscriptJob(r.Context(), h.DB, videoSource.ID, artifact.SessionID, loomURL, req.LoomPassword, h.JobProcessor); err != nil {
					log.Printf("Failed to enqueue transcript job (non-fatal): %v", err)
					// Don't fail the request - transcript can be added manually later
				} else {
					log.Printf("Transcript job enqueued successfully")
					// Set status to pending to indicate transcription is in progress
					videoSource.TranscriptStatus = models.VideoTranscriptStatusPending
					// Enable auto-transcribe flag
					videoSource.AutoTranscribeEnabled = true
					// Note: We don't update the DB here because the job will update it when it starts
				}
			} else {
				log.Printf("Job processor not available - cannot auto-transcribe")
			}
		}
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

	rag.IndexSessionAsync(videoSource.SessionID, h.DB, h.Storage)

	// Get updated video source
	updatedVideoSource, _ := h.DB.GetVideoSourceByID(r.Context(), videoID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(updatedVideoSource)
}

// Phase 2: Q&A Handlers

type AskQuestionRequest struct {
	QuestionText     string `json:"question_text"`
	VideoTimeSeconds *int   `json:"video_time_seconds,omitempty"`
}

type AskQuestionResponse struct {
	Question *models.Question `json:"question"`
	Answer   *models.Answer   `json:"answer"`
}

func (h *Handlers) AskQuestion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract artifact ID from URL path (e.g., /artifacts/{id}/questions)
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 3 || pathParts[0] != "artifacts" || pathParts[2] != "questions" {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}

	artifactID, err := uuid.Parse(pathParts[1])
	if err != nil {
		http.Error(w, "Invalid artifact ID", http.StatusBadRequest)
		return
	}

	// Verify artifact exists
	artifact, err := h.DB.GetArtifact(r.Context(), artifactID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Artifact not found: %v", err), http.StatusNotFound)
		return
	}

	// Parse request body
	var req AskQuestionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if req.QuestionText == "" {
		http.Error(w, "question_text is required", http.StatusBadRequest)
		return
	}

	// Check for existing question with same text (repeat-question caching)
	existingQuestion, existingAnswer, err := h.DB.FindExistingQuestionByText(r.Context(), artifact.SessionID, req.QuestionText)
	if err != nil {
		log.Printf("Error checking for existing question: %v", err)
		// Continue with new question creation on error
	} else if existingQuestion != nil && existingAnswer != nil {
		// Return cached answer
		log.Printf("Returning cached answer for duplicate question: %s", req.QuestionText)
		response := AskQuestionResponse{
			Question: existingQuestion,
			Answer:   existingAnswer,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK) // 200 OK for cached response
		json.NewEncoder(w).Encode(response)
		return
	}

	// Create new question
	question := &models.Question{
		ID:               uuid.New(),
		ArtifactID:       artifactID,
		SessionID:        artifact.SessionID,
		QuestionText:     req.QuestionText,
		QuestionSource:   models.QuestionSourceText,
		VideoTimeSeconds: req.VideoTimeSeconds, // Include timestamp if provided
	}

	if err := h.DB.CreateQuestion(r.Context(), question); err != nil {
		log.Printf("Error creating question: %v", err)
		http.Error(w, fmt.Sprintf("Failed to create question: %v", err), http.StatusInternalServerError)
		return
	}

	// Retrieve materials and video sources for RAG (from session; active only)
	materials, err := h.DB.GetActiveMaterialsBySessionID(r.Context(), artifact.SessionID)
	if err != nil {
		log.Printf("Warning: Failed to get materials: %v", err)
		materials = []*models.Material{}
	}

	videoSources, err := h.DB.GetVideoSourcesBySessionID(r.Context(), artifact.SessionID)
	if err != nil {
		log.Printf("Warning: Failed to get video sources: %v", err)
		videoSources = []*models.VideoSource{}
	}

	var videoSource *models.VideoSource
	if len(videoSources) > 0 {
		videoSource = videoSources[0]
	}
	if err != nil {
		log.Printf("Warning: Failed to get video source: %v", err)
		videoSource = nil
	}

	// Perform retrieval
	chunks := utils.RetrieveChunks(req.QuestionText, materials, videoSource, 5)

	// Get prior questions and answers from this session for context accumulation
	priorQuestions, priorAnswers, err := h.DB.GetQuestionsBySessionID(r.Context(), artifact.SessionID, 10)
	if err != nil {
		log.Printf("Warning: Failed to get prior questions for context: %v", err)
		priorQuestions = []*models.Question{}
		priorAnswers = []*models.Answer{}
	}

	// Build prior Q&A pairs (excluding the current question we just created)
	priorQA := make([]utils.PriorQAPair, 0, len(priorQuestions))
	answerMap := make(map[uuid.UUID]*models.Answer)
	for _, answer := range priorAnswers {
		answerMap[answer.QuestionID] = answer
	}

	for _, priorQuestion := range priorQuestions {
		// Skip the current question (it won't have an answer yet, but be safe)
		if priorQuestion.ID == question.ID {
			continue
		}
		if priorAnswer, exists := answerMap[priorQuestion.ID]; exists && priorAnswer != nil {
			priorQA = append(priorQA, utils.PriorQAPair{
				Question: priorQuestion.QuestionText,
				Answer:   priorAnswer.AnswerText,
			})
		}
	}

	// Generate answer using LLM with prior Q&A context
	qaResponse, _, err := utils.GenerateAnswer(r.Context(), req.QuestionText, chunks, artifact.Title, priorQA)
	if err != nil {
		log.Printf("Error generating answer: %v", err)
		// Still create an error answer
		qaResponse = &utils.QAResponse{
			AnswerStatus: "error",
			AnswerText:   fmt.Sprintf("Failed to generate answer: %v", err),
			Confidence:   0,
			Citations:    []models.Citation{},
		}
	}

	// Convert to Answer model
	answer, err := utils.ConvertQAResponseToAnswer(question.ID, qaResponse, "gpt-4o-mini")
	if err != nil {
		log.Printf("Error converting answer: %v", err)
		http.Error(w, fmt.Sprintf("Failed to process answer: %v", err), http.StatusInternalServerError)
		return
	}

	// Save answer
	if err := h.DB.CreateAnswer(r.Context(), answer); err != nil {
		log.Printf("Error creating answer: %v", err)
		http.Error(w, fmt.Sprintf("Failed to save answer: %v", err), http.StatusInternalServerError)
		return
	}

	response := AskQuestionResponse{
		Question: question,
		Answer:   answer,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}

type GetQuestionsResponse struct {
	Questions []*models.Question `json:"questions"`
	Answers   []*models.Answer   `json:"answers"`
}

func (h *Handlers) GetQuestions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract artifact ID from URL path (e.g., /artifacts/{id}/questions)
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 3 || pathParts[0] != "artifacts" || pathParts[2] != "questions" {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}

	artifactID, err := uuid.Parse(pathParts[1])
	if err != nil {
		http.Error(w, "Invalid artifact ID", http.StatusBadRequest)
		return
	}

	// Get questions with answers (allow non-existent artifacts to return empty lists)
	questions, answers, err := h.DB.GetQuestionsByArtifactID(r.Context(), artifactID, 20)
	if err != nil {
		// If artifact doesn't exist, return empty lists instead of error
		// This allows the endpoint to return 200 with empty data for non-existent artifacts
		log.Printf("Warning: Failed to get questions for artifact %s: %v", artifactID, err)
		questions = []*models.Question{}
		answers = []*models.Answer{}
	}

	response := GetQuestionsResponse{
		Questions: questions,
		Answers:   answers,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// GetTranscriptJob retrieves transcript job status for a video source
func (h *Handlers) GetTranscriptJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract artifact ID and video ID from URL path (e.g., /artifacts/{id}/video/{video_id}/transcript-job)
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 5 || pathParts[0] != "artifacts" || pathParts[2] != "video" || pathParts[4] != "transcript-job" {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}

	videoID, err := uuid.Parse(pathParts[3])
	if err != nil {
		http.Error(w, "Invalid video ID", http.StatusBadRequest)
		return
	}

	// Get video source to check transcription_job_id
	videoSource, err := h.DB.GetVideoSourceByID(r.Context(), videoID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Video source not found: %v", err), http.StatusNotFound)
		return
	}

	// If no job ID, return status indicating no job
	if videoSource.TranscriptionJobID == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"video_source_id": videoSource.ID.String(),
			"job":             nil,
			"status":          "no_job",
			"message":         "No transcription job found for this video",
		})
		return
	}

	// Get the job
	job, err := h.DB.GetTranscriptJob(r.Context(), *videoSource.TranscriptionJobID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Transcript job not found: %v", err), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"video_source_id": videoSource.ID.String(),
		"job":             job,
		"status":          string(job.Status),
	})
}

// RegenerateTranscript triggers a new transcription job for a video source
func (h *Handlers) RegenerateTranscript(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract artifact ID and video ID from URL path
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 6 || pathParts[0] != "artifacts" || pathParts[2] != "video" || pathParts[4] != "transcript-job" || pathParts[5] != "regenerate" {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}

	videoID, err := uuid.Parse(pathParts[3])
	if err != nil {
		http.Error(w, "Invalid video ID", http.StatusBadRequest)
		return
	}

	// Get video source
	videoSource, err := h.DB.GetVideoSourceByID(r.Context(), videoID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Video source not found: %v", err), http.StatusNotFound)
		return
	}

	// Only allow regeneration for Loom videos
	if videoSource.Provider != "loom" {
		http.Error(w, "Regeneration only supported for Loom videos", http.StatusBadRequest)
		return
	}

	// Get artifact to retrieve session_id
	artifact, err := h.DB.GetArtifact(r.Context(), videoSource.ArtifactID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Artifact not found: %v", err), http.StatusInternalServerError)
		return
	}

	// Get Loom URL (prefer embed_url, fallback to video_url)
	loomURL := videoSource.VideoURL
	if videoSource.EmbedURL != nil && *videoSource.EmbedURL != "" {
		loomURL = *videoSource.EmbedURL
	}

	if loomURL == "" {
		http.Error(w, "No Loom URL found for this video source", http.StatusBadRequest)
		return
	}

	// Enqueue new transcription job
	if h.JobProcessor == nil {
		http.Error(w, "Job processor not available", http.StatusServiceUnavailable)
		return
	}

	// Get password from existing job if available, otherwise use nil
	var password *string
	if videoSource.TranscriptionJobID != nil {
		existingJob, err := h.DB.GetTranscriptJob(r.Context(), *videoSource.TranscriptionJobID)
		if err == nil && existingJob != nil && existingJob.LoomPassword != nil {
			password = existingJob.LoomPassword
		}
	}

	if err := utils.ProcessTranscriptJob(r.Context(), h.DB, videoSource.ID, artifact.SessionID, loomURL, password, h.JobProcessor); err != nil {
		http.Error(w, fmt.Sprintf("Failed to enqueue transcript job: %v", err), http.StatusInternalServerError)
		return
	}

	// Update video source status to pending
	if err := h.DB.UpdateVideoSourceTranscriptStatus(r.Context(), videoSource.ID, models.VideoTranscriptStatusPending); err != nil {
		log.Printf("Warning: Failed to update video source status: %v", err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "accepted",
		"message": "Transcription job queued successfully",
	})
}

// Ingestion functionality removed for Phase 1
// Will be re-implemented in a later phase with embeddings
