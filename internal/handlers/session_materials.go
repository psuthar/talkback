package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/rag"
	"github.com/psuthar/talkback/internal/storage"
	"github.com/psuthar/talkback/internal/utils"
)

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
	materials, err := h.DB.GetMaterialsBySessionID(r.Context(), sessionID)
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

	exists, err := h.DB.ExistsMaterialWithFilenameInSession(r.Context(), sessionID, header.Filename)
	if err != nil {
		log.Printf("SessionUploadMaterial check duplicate: %v", err)
		http.Error(w, "Failed to check existing files", http.StatusInternalServerError)
		return
	}
	if exists {
		http.Error(w, fmt.Sprintf("A file named %q is already in this session", header.Filename), http.StatusConflict)
		return
	}

	// Explicit path: <UploadRoot>/sessions/{session_id}/data/uploads/{filename}
	uploadsDir := storage.SessionUploadsAbsDir(sessionID)
	log.Printf("SessionUploadMaterial: storing to %s", uploadsDir)
	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		http.Error(w, "Failed to create uploads directory", http.StatusInternalServerError)
		return
	}
	filePath := filepath.Join(uploadsDir, header.Filename)
	if err := utils.SaveFile(file, filePath); err != nil {
		http.Error(w, "Failed to save file", http.StatusInternalServerError)
		return
	}
	storageURL := storage.SessionArtifactPath(sessionID, header.Filename)
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	ext := strings.ToLower(filepath.Ext(header.Filename))
	isImage := strings.HasPrefix(contentType, "image/") ||
		ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".gif" || ext == ".webp" || ext == ".bmp" || ext == ".svg"
	kind := deriveMaterialKind(ext, contentType, isImage)

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
		text, err := utils.ExtractTextFromFile(filePath)
		if err != nil {
			textStatus = models.MaterialTextStatusFailed
			s := err.Error()
			errMsg = &s
			log.Printf("PDF extraction failed for %s: %v", header.Filename, err)
		} else if strings.TrimSpace(text) == "" {
			textStatus = models.MaterialTextStatusFailed
			s := "PDF produced empty text"
			errMsg = &s
		} else {
			extractedText = &text
			textStatus = models.MaterialTextStatusReady
		}
	case isOfficeFile(ext, contentType):
		text, err := utils.ExtractTextFromFile(filePath)
		if err != nil {
			textStatus = models.MaterialTextStatusFailed
			s := err.Error()
			errMsg = &s
			log.Printf("Office extraction failed for %s: %v", header.Filename, err)
		} else if strings.TrimSpace(text) == "" {
			textStatus = models.MaterialTextStatusFailed
			s := "Office extraction produced empty text"
			errMsg = &s
		} else {
			extractedText = &text
			textStatus = models.MaterialTextStatusReady
		}
	}

	titleFromForm := r.FormValue("title")
	var title *string
	if titleFromForm != "" {
		title = &titleFromForm
	} else {
		title = &header.Filename
	}

	material := &models.Material{
		ID:            uuid.New(),
		ArtifactID:    artifactID,
		SessionID:     sessionID,
		Kind:          kind,
		Filename:      header.Filename,
		ContentType:   contentType,
		StorageURL:    storageURL,
		TextStatus:    textStatus,
		ExtractedText: extractedText,
		Title:         title,
		ErrorMessage:  errMsg,
	}
	if err := h.DB.CreateMaterial(r.Context(), material); err != nil {
		os.Remove(filePath)
		http.Error(w, "Failed to create material record", http.StatusInternalServerError)
		return
	}
	if material.ExtractedText != nil && *material.ExtractedText != "" {
		rag.IndexSessionAsync(sessionID, h.DB)
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
	rag.IndexSessionAsync(sessionID, h.DB)
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
	// Delete chunks for this material (embeddings cascade-delete)
	if err := h.DB.DeleteSessionChunksBySource(r.Context(), sessionID, "material", materialID); err != nil {
		log.Printf("DeleteSessionMaterial delete chunks: %v", err)
	}
	if err := h.DB.DeleteMaterial(r.Context(), materialID); err != nil {
		http.Error(w, "Failed to delete material", http.StatusInternalServerError)
		return
	}
	// Optionally remove file from disk if StorageURL set
	if mat.StorageURL != "" {
		path := filepath.Join(storage.UploadRoot(), filepath.FromSlash(mat.StorageURL))
		_ = os.Remove(path)
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
