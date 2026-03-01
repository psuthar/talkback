package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/storage"
)

// PresignPutRequest is the body for POST /api/artifacts/presign-put
type PresignPutRequest struct {
	SessionID   *string `json:"session_id,omitempty"`
	OwnerUserID *string `json:"owner_user_id,omitempty"`
	Kind        string  `json:"kind"`
	Filename    string  `json:"filename"`
	ContentType string  `json:"content_type"`
	SizeBytes   *int64  `json:"size_bytes,omitempty"`
	Sha256      string  `json:"sha256,omitempty"`
}

// PresignPutResponse is the response for POST /api/artifacts/presign-put
type PresignPutResponse struct {
	ArtifactID      string            `json:"artifact_id"`
	StorageKey      string            `json:"storage_key,omitempty"`
	PutURL          string            `json:"put_url"`
	RequiredHeaders map[string]string `json:"required_headers"`
}

// CompleteArtifactRequest is the body for POST /api/artifacts/complete
type CompleteArtifactRequest struct {
	ArtifactID string `json:"artifact_id"`
	SizeBytes  int64  `json:"size_bytes"`
	Sha256     string `json:"sha256,omitempty"`
}

// ArtifactAccessResponse is the response for GET /api/artifacts/{id}/access
type ArtifactAccessResponse struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

func maxUploadBytesDefault() int64 {
	v := os.Getenv("MAX_UPLOAD_BYTES_DEFAULT")
	if v == "" {
		return 250 * 1024 * 1024 // 250MB
	}
	n, _ := strconv.ParseInt(v, 10, 64)
	if n <= 0 {
		return 250 * 1024 * 1024
	}
	return n
}

func maxUploadBytesVideo() int64 {
	v := os.Getenv("MAX_UPLOAD_BYTES_VIDEO")
	if v == "" {
		return 1024 * 1024 * 1024 // 1GB
	}
	n, _ := strconv.ParseInt(v, 10, 64)
	if n <= 0 {
		return 1024 * 1024 * 1024
	}
	return n
}

func maxSessionArtifacts() int {
	v := os.Getenv("MAX_SESSION_ARTIFACTS")
	if v == "" {
		return 50
	}
	n, _ := strconv.Atoi(v)
	if n <= 0 {
		return 50
	}
	return n
}

// PresignPut handles POST /api/artifacts/presign-put (auth required; creator/admin).
func (h *Handlers) PresignPut(w http.ResponseWriter, r *http.Request) {
	if h.Storage == nil {
		http.Error(w, "Storage not configured", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req PresignPutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	// Validate kind
	kind := models.FileArtifactKind(strings.TrimSpace(strings.ToLower(req.Kind)))
	switch kind {
	case models.FileArtifactKindVideo, models.FileArtifactKindDocument, models.FileArtifactKindImage,
		models.FileArtifactKindAttachment, models.FileArtifactKindExport, models.FileArtifactKindTranscriptRaw:
	default:
		http.Error(w, "Invalid kind", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.ContentType) == "" {
		http.Error(w, "content_type is required", http.StatusBadRequest)
		return
	}
	// Size caps
	maxBytes := maxUploadBytesDefault()
	if kind == models.FileArtifactKindVideo {
		maxBytes = maxUploadBytesVideo()
	}
	if req.SizeBytes != nil && *req.SizeBytes > maxBytes {
		http.Error(w, fmt.Sprintf("size_bytes exceeds maximum %d", maxBytes), http.StatusBadRequest)
		return
	}
	// Session is required (all files are stored under a session ID path).
	var sessionID, ownerUserID *uuid.UUID
	if req.SessionID != nil && *req.SessionID != "" {
		sid, err := uuid.Parse(*req.SessionID)
		if err != nil {
			http.Error(w, "Invalid session_id", http.StatusBadRequest)
			return
		}
		sessionID = &sid
		// AuthZ: user must have access to session (creator or participant or admin)
		// TODO: require auth and check session access
	}
	if req.OwnerUserID != nil && *req.OwnerUserID != "" {
		oid, err := uuid.Parse(*req.OwnerUserID)
		if err != nil {
			http.Error(w, "Invalid owner_user_id", http.StatusBadRequest)
			return
		}
		ownerUserID = &oid
	}
	if sessionID == nil {
		http.Error(w, "session_id is required", http.StatusBadRequest)
		return
	}
	bucket := os.Getenv("R2_BUCKET")
	if bucket == "" {
		bucket = "talkback-r2-bucket"
	}
	prefix := strings.TrimSuffix(strings.TrimSpace(os.Getenv("R2_PREFIX")), "/")
	artifactID := uuid.New()
	storageKey := storage.BuildArtifactStorageKey(prefix, *sessionID, artifactID, req.Filename)
	ttl := 15 * time.Minute
	if s := os.Getenv("R2_PRESIGN_PUT_TTL_SECONDS"); s != "" {
		if sec, _ := strconv.Atoi(s); sec > 0 {
			ttl = time.Duration(sec) * time.Second
		}
	}
	putURL, headers, err := h.Storage.PresignPut(r.Context(), storageKey, ttl, req.ContentType)
	if err != nil {
		log.Printf("PresignPut: %v", err)
		http.Error(w, "Failed to generate presigned URL", http.StatusInternalServerError)
		return
	}
	fa := &models.FileArtifact{
		ID:              artifactID,
		SessionID:       sessionID,
		OwnerUserID:     ownerUserID,
		Kind:            kind,
		Filename:        trimStringPtr(req.Filename),
		ContentType:     strings.TrimSpace(req.ContentType),
		SizeBytes:       req.SizeBytes,
		StorageProvider: "r2",
		StorageBucket:   bucket,
		StorageKey:      storageKey,
		Status:          models.FileArtifactStatusPending,
	}
	fa.Sha256 = trimStringPtr(req.Sha256)
	if err := h.DB.CreateFileArtifact(r.Context(), fa); err != nil {
		log.Printf("CreateFileArtifact: %v", err)
		http.Error(w, "Failed to create artifact record", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(PresignPutResponse{
		ArtifactID:      artifactID.String(),
		StorageKey:      storageKey,
		PutURL:          putURL,
		RequiredHeaders: headers,
	})
}

func trimStringPtr(s string) *string {
	if s == "" {
		return nil
	}
	t := strings.TrimSpace(s)
	if t == "" {
		return nil
	}
	return &t
}

// CompleteArtifact handles POST /api/artifacts/complete
func (h *Handlers) CompleteArtifact(w http.ResponseWriter, r *http.Request) {
	if h.Storage == nil {
		http.Error(w, "Storage not configured", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req CompleteArtifactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	artifactID, err := uuid.Parse(req.ArtifactID)
	if err != nil {
		http.Error(w, "Invalid artifact_id", http.StatusBadRequest)
		return
	}
	if req.SizeBytes < 0 {
		http.Error(w, "size_bytes is required and must be >= 0", http.StatusBadRequest)
		return
	}
	fa, err := h.DB.GetFileArtifactByID(r.Context(), artifactID)
	if err != nil || fa == nil {
		http.Error(w, "Artifact not found", http.StatusNotFound)
		return
	}
	if fa.Status == models.FileArtifactStatusReady {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(fa)
		return
	}
	// Head to confirm object exists and get size/content-type
	exists, size, contentType, err := h.Storage.Head(r.Context(), fa.StorageKey)
	if err != nil {
		log.Printf("CompleteArtifact Head: %v", err)
		http.Error(w, "Failed to verify upload", http.StatusInternalServerError)
		return
	}
	if !exists {
		http.Error(w, "Object not found in storage; upload may have failed", http.StatusBadRequest)
		return
	}
	if size > 0 {
		req.SizeBytes = size
	}
	if contentType == "" {
		contentType = fa.ContentType
	}
	if err := h.DB.UpdateFileArtifactToReady(r.Context(), artifactID, req.SizeBytes, contentType); err != nil {
		log.Printf("UpdateFileArtifactToReady: %v", err)
		http.Error(w, "Failed to complete artifact", http.StatusInternalServerError)
		return
	}
	fa, _ = h.DB.GetFileArtifactByID(r.Context(), artifactID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(fa)
}

// ApiArtifactsRouter routes /api/artifacts/presign-put, /api/artifacts/complete, /api/artifacts/{id}/access
func (h *Handlers) ApiArtifactsRouter(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 3 || parts[0] != "api" || parts[1] != "artifacts" {
		http.NotFound(w, r)
		return
	}
	if len(parts) == 3 && parts[2] == "presign-put" && r.Method == http.MethodPost {
		h.PresignPut(w, r)
		return
	}
	if len(parts) == 3 && parts[2] == "complete" && r.Method == http.MethodPost {
		h.CompleteArtifact(w, r)
		return
	}
	if len(parts) == 4 {
		if parts[3] == "access" && r.Method == http.MethodGet {
			h.ArtifactAccess(w, r)
			return
		}
	}
	http.NotFound(w, r)
}

// ArtifactAccess handles GET /api/artifacts/{id}/access?ttl_seconds=3600
func (h *Handlers) ArtifactAccess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	// /api/artifacts/{id}/access
	if len(pathParts) < 4 || pathParts[0] != "api" || pathParts[1] != "artifacts" || pathParts[3] != "access" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	artifactID, err := uuid.Parse(pathParts[2])
	if err != nil {
		http.Error(w, "Invalid artifact id", http.StatusBadRequest)
		return
	}
	fa, err := h.DB.GetFileArtifactByID(r.Context(), artifactID)
	if err != nil || fa == nil {
		http.Error(w, "Artifact not found", http.StatusNotFound)
		return
	}
	if fa.Status != models.FileArtifactStatusReady {
		http.Error(w, "Artifact not ready", http.StatusBadRequest)
		return
	}
	// TODO: AuthZ - user must have access to session (or be owner)
	ttl := time.Hour
	if s := r.URL.Query().Get("ttl_seconds"); s != "" {
		if sec, _ := strconv.Atoi(s); sec > 0 && sec <= 86400 {
			ttl = time.Duration(sec) * time.Second
		}
	}
	expiresAt := time.Now().Add(ttl)
	var accessURL string
	if fa.StorageProvider == "local" && fa.SessionID != nil {
		base := strings.TrimSuffix(strings.TrimSpace(os.Getenv("API_PUBLIC_ORIGIN")), "/")
		if base == "" {
			scheme := "https"
			if r.TLS == nil {
				scheme = "http"
			}
			if s := r.Header.Get("X-Forwarded-Proto"); s != "" {
				scheme = s
			}
			base = scheme + "://" + r.Host
		}
		accessURL = fmt.Sprintf("%s/api/sessions/%s/primary-video", base, fa.SessionID.String())
	} else if h.Storage != nil {
		var err error
		accessURL, err = h.Storage.PresignGet(r.Context(), fa.StorageKey, ttl)
		if err != nil {
			log.Printf("ArtifactAccess PresignGet: %v", err)
			http.Error(w, "Failed to generate access URL", http.StatusInternalServerError)
			return
		}
	} else {
		http.Error(w, "Storage not configured", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ArtifactAccessResponse{URL: accessURL, ExpiresAt: expiresAt})
}
