package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/auth"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/storage"
	"github.com/psuthar/talkback/internal/utils"
)

// Phase 3: Session Handlers

type CreateSessionRequest struct {
	Title     string  `json:"title"`
	CreatedBy *string `json:"created_by,omitempty"`
}

type CreateSessionResponse struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	CreatedBy *string `json:"created_by,omitempty"`
	Status    string  `json:"status"`
	CreatedAt string  `json:"created_at"`
}

type GetSessionsResponse struct {
	Sessions []*models.Session `json:"sessions"`
}

type GetSessionResponse struct {
	Session             *models.Session       `json:"session"`
	Artifacts           []*models.Artifact    `json:"artifacts"`
	Materials           []*models.Material     `json:"materials"`
	VideoSources        []*models.VideoSource `json:"video_sources"`
	PrimaryVideo        *models.VideoSource   `json:"primary_video,omitempty"`   // effective primary (explicit primary, else first ready, else first)
	AdditionalVideos    []*models.VideoSource `json:"additional_videos,omitempty"` // all other session videos
	RecentQuestions     []*models.Question    `json:"recent_questions"`
	RecentAnswers       []*models.Answer      `json:"recent_answers"`
	Mode                string                `json:"mode"` // "creator" or "participant"
	UnreadMaterialIDs   []string              `json:"unread_material_ids,omitempty"`   // only when participant_ref provided
	VideoAccessURL      string                `json:"video_access_url,omitempty"`       // presigned R2 URL only when primary artifact is ready, r2, video
	PlaybackReasonCode  string                `json:"playback_reason_code,omitempty"`   // VIDEO_NOT_INGESTED, VIDEO_INGEST_PENDING, VIDEO_INGEST_FAILED
	PlaybackMessage     string                `json:"playback_message,omitempty"`      // safe message when video not playable
}

// resolveEffectivePrimaryAndAdditional returns the session's effective primary video (explicit primary, else first ready, else first) and all other videos as additional. Backward compatible when video_role is nil.
func resolveEffectivePrimaryAndAdditional(videoSources []*models.VideoSource) (primary *models.VideoSource, additional []*models.VideoSource) {
	if len(videoSources) == 0 {
		return nil, nil
	}
	// 1) Explicit primary
	for _, vs := range videoSources {
		if vs != nil && vs.VideoRole != nil && *vs.VideoRole == models.VideoRolePrimary {
			primary = vs
			break
		}
	}
	// 2) First ready
	if primary == nil {
		for _, vs := range videoSources {
			if vs != nil && vs.TranscriptStatus == models.VideoTranscriptStatusReady {
				primary = vs
				break
			}
		}
	}
	// 3) First video
	if primary == nil {
		primary = videoSources[0]
	}
	// Additional = all except primary (by ID)
	for _, vs := range videoSources {
		if vs != nil && (primary == nil || vs.ID != primary.ID) {
			additional = append(additional, vs)
		}
	}
	return primary, additional
}

// ensurePrimaryVideoIfNone sets the given video as primary for the session when no video in the session is yet marked primary (e.g. first video uploaded or Zoom import).
func (h *Handlers) ensurePrimaryVideoIfNone(ctx context.Context, sessionID, videoSourceID uuid.UUID) {
	sources, err := h.DB.GetVideoSourcesBySessionID(ctx, sessionID)
	if err != nil || len(sources) == 0 {
		return
	}
	for _, vs := range sources {
		if vs != nil && vs.VideoRole != nil && *vs.VideoRole == models.VideoRolePrimary {
			return // already have a primary
		}
	}
	if err := h.DB.SetVideoSourceVideoRole(ctx, sessionID, videoSourceID, models.VideoRolePrimary); err != nil {
		log.Printf("Warning: set first video as primary: %v", err)
	}
}

type JoinParticipantRequest struct {
	ParticipantRef string `json:"participant_ref"`
}

type CreateEventRequest struct {
	ParticipantRef   *string                `json:"participant_ref,omitempty"`
	EventType        string                 `json:"event_type"`
	VideoTimeSeconds *int                   `json:"video_time_seconds,omitempty"`
	Payload          map[string]interface{} `json:"payload,omitempty"`
}

type transcribeVoiceResponse struct {
	TranscribedText string   `json:"transcribed_text"`
	Confidence      *float32 `json:"confidence,omitempty"`
}

// SessionWithRole is one session plus the current user's role for it (for GET /api/sessions).
type SessionWithRole struct {
	Session *models.Session `json:"session"`
	MyRole  string         `json:"my_role"` // "creator" | "participant" | "admin"
}

// ListSessions returns all sessions the current user may access, with my_role per session (RequireAuth).
// Admins see all sessions with my_role "admin"; others see sessions they created (my_role "creator") or are invited to (my_role "participant").
func (h *Handlers) ListSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	ctx := r.Context()
	var out []SessionWithRole

	switch user.GlobalRole {
	case models.GlobalRoleAdmin:
		all, err := h.DB.ListAllSessions(ctx)
		if err != nil {
			log.Printf("ListSessions (admin): %v", err)
			http.Error(w, "Failed to list sessions", http.StatusInternalServerError)
			return
		}
		for _, s := range all {
			out = append(out, SessionWithRole{Session: s, MyRole: "admin"})
		}
	case models.GlobalRoleParticipant:
		// Participant role: only sessions they are invited to (no created sessions).
		invited, err := h.DB.ListSessionsForInvitedUser(ctx, user.ID)
		if err != nil {
			log.Printf("ListSessions (invited): %v", err)
			http.Error(w, "Failed to list sessions", http.StatusInternalServerError)
			return
		}
		for _, s := range invited {
			out = append(out, SessionWithRole{Session: s, MyRole: "participant"})
		}
		sort.Slice(out, func(i, j int) bool {
			return out[i].Session.UpdatedAt.After(out[j].Session.UpdatedAt)
		})
	default:
		// Creator (or legacy user): created + invited
		created, err := h.DB.ListSessionsByCreatedBy(ctx, user.Email)
		if err != nil {
			log.Printf("ListSessions (created): %v", err)
			http.Error(w, "Failed to list sessions", http.StatusInternalServerError)
			return
		}
		invited, err := h.DB.ListSessionsForInvitedUser(ctx, user.ID)
		if err != nil {
			log.Printf("ListSessions (invited): %v", err)
			http.Error(w, "Failed to list sessions", http.StatusInternalServerError)
			return
		}
		roleByID := make(map[uuid.UUID]string)
		sessionByID := make(map[uuid.UUID]*models.Session)
		for _, s := range created {
			roleByID[s.ID] = "creator"
			sessionByID[s.ID] = s
		}
		for _, s := range invited {
			if _, exists := roleByID[s.ID]; !exists {
				roleByID[s.ID] = "participant"
				sessionByID[s.ID] = s
			}
		}
		// Single list ordered by updated_at desc (collect then sort)
		for id, s := range sessionByID {
			out = append(out, SessionWithRole{Session: s, MyRole: roleByID[id]})
		}
		sort.Slice(out, func(i, j int) bool {
			return out[i].Session.UpdatedAt.After(out[j].Session.UpdatedAt)
		})
	}

	if out == nil {
		out = []SessionWithRole{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(out)
}

// CreateSession creates a new session (RequireAuth; requires admin or creator role).
func (h *Handlers) CreateSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if !user.GlobalRole.CanCreateSessions() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "your role does not allow creating sessions"})
		return
	}

	// Parse request body
	var req CreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if req.Title == "" {
		http.Error(w, "title is required", http.StatusBadRequest)
		return
	}

	createdBy := user.Email
	// Create session (creator is the authenticated user)
	session := &models.Session{
		ID:        uuid.New(),
		Title:     req.Title,
		CreatedBy: &createdBy,
		Status:    models.SessionStatusOpen,
	}

	if err := h.DB.CreateSession(r.Context(), session); err != nil {
		log.Printf("Error creating session: %v", err)
		http.Error(w, fmt.Sprintf("Failed to create session: %v", err), http.StatusInternalServerError)
		return
	}

	// Create a first artifact so the session is immediately usable (edit view, Q&A, and "session has no artifacts" is avoided)
	title := session.Title
	if title == "" {
		title = "Session content"
	}
	if _, err := h.DB.CreateArtifact(r.Context(), session.ID, title, nil); err != nil {
		log.Printf("Error creating default artifact for session %s: %v", session.ID, err)
		// Non-fatal: session is created; user can create an artifact from the UI
	}

	response := CreateSessionResponse{
		ID:        session.ID.String(),
		Title:     session.Title,
		CreatedBy: session.CreatedBy,
		Status:    string(session.Status),
		CreatedAt: session.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}

// GetArtifactsBySession retrieves all artifacts for a session
func (h *Handlers) GetArtifactsBySession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID from URL path (e.g., /sessions/{id}/artifacts)
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 3 || pathParts[0] != "sessions" || pathParts[2] != "artifacts" {
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

	// Get artifacts
	artifacts, err := h.DB.GetArtifactsBySessionID(r.Context(), sessionID)
	if err != nil {
		log.Printf("Error getting artifacts: %v", err)
		http.Error(w, fmt.Sprintf("Failed to get artifacts: %v", err), http.StatusInternalServerError)
		return
	}

	type GetArtifactsResponse struct {
		Artifacts []*models.Artifact `json:"artifacts"`
	}

	response := GetArtifactsResponse{
		Artifacts: artifacts,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// GetSession retrieves a session with its artifact context
func (h *Handlers) GetSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID from URL path (e.g., /sessions/{id})
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 2 || pathParts[0] != "sessions" {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}

	sessionID, err := uuid.Parse(pathParts[1])
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	// Get session
	session, err := h.DB.GetSession(r.Context(), sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Session not found: %v", err), http.StatusNotFound)
		return
	}

	// Get artifacts for this session
	artifacts, err := h.DB.GetArtifactsBySessionID(r.Context(), sessionID)
	if err != nil {
		log.Printf("Warning: Failed to get artifacts for session %s: %v", sessionID, err)
		artifacts = []*models.Artifact{}
	}

	// Get materials and video sources from session (active only; excludes soft-deleted)
	allMaterials, err := h.DB.GetActiveMaterialsBySessionID(r.Context(), sessionID)
	if err != nil {
		log.Printf("Warning: Failed to get materials: %v", err)
		allMaterials = []*models.Material{}
	}

	allVideoSources, err := h.DB.GetVideoSourcesBySessionID(r.Context(), sessionID)
	if err != nil {
		log.Printf("Warning: Failed to get video sources: %v", err)
		allVideoSources = []*models.VideoSource{}
	}

	// Get recent questions (limit 20)
	questions, answers, err := h.DB.GetQuestionsBySessionID(r.Context(), sessionID, 20)
	if err != nil {
		log.Printf("Warning: Failed to get questions: %v", err)
		questions = []*models.Question{}
		answers = []*models.Answer{}
	}
	// Enrich answer citations with source_id from chunks so doc citations open correctly in participant view
	chunks, _ := h.DB.ListSessionChunksBySessionID(r.Context(), sessionID)
	chunkSourceByID := make(map[string]struct{ SourceID, SourceType string })
	for _, c := range chunks {
		if c.SourceID != nil {
			chunkSourceByID[c.ID.String()] = struct{ SourceID, SourceType string }{
				SourceID:   c.SourceID.String(),
				SourceType: c.SourceType,
			}
		}
	}
	for _, a := range answers {
		if a == nil {
			continue
		}
		for i := range a.Citations {
			c := &a.Citations[i]
			if c.SourceID != "" {
				continue
			}
			if info, ok := chunkSourceByID[c.ChunkID]; ok {
				c.SourceID = info.SourceID
				if c.SourceType == "" {
					c.SourceType = info.SourceType
				}
			}
		}
	}

	// Determine mode: creator if current_user matches session.created_by, otherwise participant
	mode := "participant"
	currentUser := r.Header.Get("X-Current-User")
	if currentUser == "" {
		// Fallback to query parameter
		currentUser = r.URL.Query().Get("user")
	}
	if currentUser != "" && session.CreatedBy != nil && *session.CreatedBy == currentUser {
		mode = "creator"
	}

	// When participant_ref is provided, return unread material IDs for the "new document" marker
	var unreadMaterialIDs []string
	participantRef := r.Header.Get("X-Participant-Ref")
	if participantRef == "" {
		participantRef = r.URL.Query().Get("participant_ref")
	}
	if participantRef != "" {
		unread, err := h.DB.GetUnreadMaterialIDsForParticipant(r.Context(), sessionID, participantRef)
		if err != nil {
			log.Printf("Warning: Failed to get unread materials for participant: %v", err)
		} else {
			for _, id := range unread {
				unreadMaterialIDs = append(unreadMaterialIDs, id.String())
			}
		}
	}

	// R2-only playback: set video_access_url only when primary artifact exists, ready, r2, and content_type is video.
	var videoAccessURL, playbackReasonCode, playbackMessage string
	if session.PrimaryVideoArtifactID != nil {
		fa, err := h.DB.GetFileArtifactByID(r.Context(), *session.PrimaryVideoArtifactID)
		if err != nil || fa == nil {
			playbackReasonCode = "VIDEO_NOT_INGESTED"
			playbackMessage = "Video not available for this session."
		} else if fa.Status == models.FileArtifactStatusPending {
			playbackReasonCode = "VIDEO_INGEST_PENDING"
			playbackMessage = "Video is still being prepared. Refresh in a moment."
		} else if fa.Status == models.FileArtifactStatusFailed {
			playbackReasonCode = "VIDEO_INGEST_FAILED"
			playbackMessage = "Video ingest failed. Creator can retry import."
		} else if fa.Status == models.FileArtifactStatusReady {
			// Always use same-origin primary-video URL so the browser never hits R2 directly (avoids CORS). API streams from R2 or local.
			ct := strings.TrimSpace(strings.ToLower(fa.ContentType))
			isVideo := ct == "video/mp4" || strings.HasPrefix(ct, "video/")
			if !isVideo {
				playbackReasonCode = "VIDEO_NOT_INGESTED"
				playbackMessage = "Video not available for this session."
			} else if fa.StorageProvider == "r2" && h.Storage != nil || fa.StorageProvider == "local" {
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
				videoAccessURL = fmt.Sprintf("%s/api/sessions/%s/primary-video", base, sessionID.String())
			} else {
				playbackReasonCode = "VIDEO_NOT_INGESTED"
				playbackMessage = "Video not available for this session."
			}
		}
	}

	primaryVideo, additionalVideos := resolveEffectivePrimaryAndAdditional(allVideoSources)
	response := GetSessionResponse{
		Session:            session,
		Artifacts:          artifacts,
		Materials:          allMaterials,
		VideoSources:       allVideoSources,
		PrimaryVideo:       primaryVideo,
		AdditionalVideos:   additionalVideos,
		RecentQuestions:    questions,
		RecentAnswers:      answers,
		Mode:               mode,
		UnreadMaterialIDs:  unreadMaterialIDs,
		VideoAccessURL:     videoAccessURL,
		PlaybackReasonCode: playbackReasonCode,
		PlaybackMessage:    playbackMessage,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store") // video_access_url is presigned and time-limited; do not cache session response
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// SessionPlaybackResponse is the success response for GET .../playback.
type SessionPlaybackResponse struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// SessionPlaybackErrorResponse is the error body for playback (409/404/422).
type SessionPlaybackErrorResponse struct {
	Message    string `json:"message"`
	ReasonCode string `json:"reason_code"`
}

// SessionPlayback returns R2 presigned URL only when primary artifact is ready, r2, and video. No Zoom fallback.
// GET /sessions/:id/playback or GET /api/sessions/:id/playback
func (h *Handlers) SessionPlayback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	var sessionID uuid.UUID
	var err error
	if len(pathParts) >= 4 && pathParts[0] == "api" && pathParts[1] == "sessions" && pathParts[3] == "playback" {
		sessionID, err = uuid.Parse(pathParts[2])
	} else if len(pathParts) >= 3 && pathParts[0] == "sessions" && pathParts[2] == "playback" {
		sessionID, err = uuid.Parse(pathParts[1])
	} else {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	session, err := h.DB.GetSession(r.Context(), sessionID)
	if err != nil || session == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(SessionPlaybackErrorResponse{Message: "Session not found", ReasonCode: "VIDEO_NOT_INGESTED"})
		return
	}
	if session.PrimaryVideoArtifactID == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(SessionPlaybackErrorResponse{Message: "Video not available for this session.", ReasonCode: "VIDEO_NOT_INGESTED"})
		return
	}
	fa, err := h.DB.GetFileArtifactByID(r.Context(), *session.PrimaryVideoArtifactID)
	if err != nil || fa == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(SessionPlaybackErrorResponse{Message: "Video not available for this session.", ReasonCode: "VIDEO_NOT_INGESTED"})
		return
	}
	switch fa.Status {
	case models.FileArtifactStatusPending:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(SessionPlaybackErrorResponse{Message: "Video is still being prepared. Refresh in a moment.", ReasonCode: "VIDEO_INGEST_PENDING"})
		return
	case models.FileArtifactStatusFailed:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(SessionPlaybackErrorResponse{Message: "Video ingest failed. Creator can retry import.", ReasonCode: "VIDEO_INGEST_FAILED"})
		return
	}
	if fa.Status != models.FileArtifactStatusReady {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(SessionPlaybackErrorResponse{Message: "Video not available for this session.", ReasonCode: "VIDEO_NOT_INGESTED"})
		return
	}
	ct := strings.TrimSpace(strings.ToLower(fa.ContentType))
	isVideo := ct == "video/mp4" || strings.HasPrefix(ct, "video/")
	if fa.StorageProvider != "r2" || !isVideo || h.Storage == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(SessionPlaybackErrorResponse{Message: "Video not available for this session.", ReasonCode: "VIDEO_NOT_INGESTED"})
		return
	}
	ttl := time.Hour
	expiresAt := time.Now().Add(ttl)
	url, err := h.Storage.PresignGet(r.Context(), fa.StorageKey, ttl)
	if err != nil {
		log.Printf("SessionPlayback PresignGet: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(SessionPlaybackErrorResponse{Message: "Failed to generate playback URL.", ReasonCode: "VIDEO_NOT_INGESTED"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(SessionPlaybackResponse{URL: url, ExpiresAt: expiresAt})
}

// SetPrimaryVideoArtifactRequest body for POST /api/sessions/:id/primary-video-artifact
type SetPrimaryVideoArtifactRequest struct {
	ArtifactID string `json:"artifact_id"`
}

// SessionPrimaryVideoStream serves the primary video file for a session (from local disk or by streaming from R2).
// GET /api/sessions/:id/primary-video — same-origin so the browser avoids CORS with R2.
func (h *Handlers) SessionPrimaryVideoStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 4 || pathParts[0] != "api" || pathParts[1] != "sessions" || pathParts[3] != "primary-video" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(pathParts[2])
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	session, err := h.DB.GetSession(r.Context(), sessionID)
	if err != nil || session == nil || session.PrimaryVideoArtifactID == nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}
	fa, err := h.DB.GetFileArtifactByID(r.Context(), *session.PrimaryVideoArtifactID)
	if err != nil || fa == nil || fa.Status != models.FileArtifactStatusReady {
		http.Error(w, "Video not found", http.StatusNotFound)
		return
	}
	ct := "video/mp4"
	if fa.ContentType != "" {
		ct = strings.TrimSpace(fa.ContentType)
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Accept-Ranges", "bytes")

	switch fa.StorageProvider {
	case "r2":
		if h.Storage == nil {
			http.Error(w, "R2 storage not configured", http.StatusServiceUnavailable)
			return
		}
		exists, size, contentType, headErr := h.Storage.Head(r.Context(), fa.StorageKey)
		if headErr != nil || !exists {
			log.Printf("SessionPrimaryVideoStream R2 Head %s: %v", fa.StorageKey, headErr)
			http.Error(w, "Video not found", http.StatusNotFound)
			return
		}
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		if r.Method == http.MethodHead {
			if size > 0 {
				w.Header().Set("Content-Length", fmt.Sprintf("%d", size))
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		if size > 0 {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", size))
		}
		w.WriteHeader(http.StatusOK)
		rc, err := h.Storage.Get(r.Context(), fa.StorageKey)
		if err != nil {
			log.Printf("SessionPrimaryVideoStream R2 Get %s: %v", fa.StorageKey, err)
			return
		}
		defer rc.Close()
		_, _ = io.Copy(w, rc)
		return
	case "local":
		absPath := filepath.Join(storage.UploadRoot(), fa.StorageKey)
		f, err := os.Open(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				http.Error(w, "Video file not found", http.StatusNotFound)
				return
			}
			log.Printf("SessionPrimaryVideoStream open %s: %v", absPath, err)
			http.Error(w, "Failed to open video", http.StatusInternalServerError)
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			http.Error(w, "Failed to stat video", http.StatusInternalServerError)
			return
		}
		if r.Method == http.MethodHead {
			if info.Size() > 0 {
				w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		http.ServeContent(w, r, "zoom.mp4", info.ModTime(), f)
		return
	default:
		http.Error(w, "Primary video storage not supported", http.StatusBadRequest)
		return
	}
}

// SetSessionPrimaryVideoArtifact sets the session's primary_video_artifact_id (for R2 playback). Called after client completes presign-put flow.
func (h *Handlers) SetSessionPrimaryVideoArtifact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	// /api/sessions/:id/primary-video-artifact -> parts ["api","sessions", id, "primary-video-artifact"]
	if len(pathParts) < 4 || pathParts[0] != "api" || pathParts[1] != "sessions" || pathParts[3] != "primary-video-artifact" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(pathParts[2])
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	var req SetPrimaryVideoArtifactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	artifactID, err := uuid.Parse(strings.TrimSpace(req.ArtifactID))
	if err != nil {
		http.Error(w, "Invalid artifact_id", http.StatusBadRequest)
		return
	}
	if _, err := h.DB.GetSession(r.Context(), sessionID); err != nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}
	fa, err := h.DB.GetFileArtifactByID(r.Context(), artifactID)
	if err != nil || fa == nil {
		http.Error(w, "Artifact not found", http.StatusNotFound)
		return
	}
	if fa.SessionID == nil || *fa.SessionID != sessionID {
		http.Error(w, "Artifact does not belong to this session", http.StatusBadRequest)
		return
	}
	if fa.Kind != models.FileArtifactKindVideo {
		http.Error(w, "Artifact is not a video", http.StatusBadRequest)
		return
	}
	if fa.Status != models.FileArtifactStatusReady {
		http.Error(w, "Artifact is not ready", http.StatusBadRequest)
		return
	}
	if err := h.DB.SetSessionPrimaryVideoArtifact(r.Context(), sessionID, &artifactID); err != nil {
		log.Printf("SetSessionPrimaryVideoArtifact: %v", err)
		http.Error(w, "Failed to set primary video artifact", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "Primary video artifact set"})
}

// SetPrimaryVideoSourceRequest body for POST /api/sessions/:id/set-primary-video (creator: mark a video source as the session's primary presentation).
type SetPrimaryVideoSourceRequest struct {
	VideoSourceID string `json:"video_source_id"`
}

// SetSessionPrimaryVideoSource sets the given video source as primary (video_role=primary) and demotes any existing primary. Creator or admin only.
func (h *Handlers) SetSessionPrimaryVideoSource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 4 || pathParts[0] != "api" || pathParts[1] != "sessions" || pathParts[3] != "set-primary-video" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(pathParts[2])
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	var req SetPrimaryVideoSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	videoSourceID, err := uuid.Parse(strings.TrimSpace(req.VideoSourceID))
	if err != nil {
		http.Error(w, "Invalid video_source_id", http.StatusBadRequest)
		return
	}
	session, err := h.DB.GetSession(r.Context(), sessionID)
	if err != nil || session == nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}
	vs, err := h.DB.GetVideoSourceByID(r.Context(), videoSourceID)
	if err != nil || vs == nil || vs.SessionID != sessionID {
		http.Error(w, "Video source not found", http.StatusNotFound)
		return
	}
	// Creator or admin only (same as other session mutations)
	currentUser := r.Header.Get("X-Current-User")
	if currentUser == "" {
		currentUser = r.URL.Query().Get("user")
	}
	if session.CreatedBy != nil && *session.CreatedBy != currentUser {
		// Allow admin
		if u := UserFromContext(r.Context()); u == nil || u.GlobalRole != models.GlobalRoleAdmin {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
	}
	if err := h.DB.SetVideoSourceVideoRole(r.Context(), sessionID, videoSourceID, models.VideoRolePrimary); err != nil {
		log.Printf("SetSessionPrimaryVideoSource: %v", err)
		http.Error(w, "Failed to set primary video", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "Primary video set"})
}

// MarkSessionMaterialsSeenRequest body for POST /sessions/:id/materials/seen
type MarkSessionMaterialsSeenRequest struct {
	ParticipantRef string   `json:"participant_ref"`
	MaterialIDs    []string `json:"material_ids"`
}

// MarkSessionMaterialsSeen records that a participant has viewed the given materials (clears "new" marker).
func (h *Handlers) MarkSessionMaterialsSeen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 4 || pathParts[0] != "sessions" || pathParts[2] != "materials" || pathParts[3] != "seen" {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(pathParts[1])
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	if _, err := h.DB.GetSession(r.Context(), sessionID); err != nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}
	var req MarkSessionMaterialsSeenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.ParticipantRef == "" {
		http.Error(w, "participant_ref is required", http.StatusBadRequest)
		return
	}
	var materialUUIDs []uuid.UUID
	for _, s := range req.MaterialIDs {
		if s == "" {
			continue
		}
		id, err := uuid.Parse(s)
		if err != nil {
			continue
		}
		materialUUIDs = append(materialUUIDs, id)
	}
	if err := h.DB.MarkMaterialsSeenByParticipant(r.Context(), sessionID, req.ParticipantRef, materialUUIDs); err != nil {
		log.Printf("MarkSessionMaterialsSeen: %v", err)
		http.Error(w, "Failed to mark materials as seen", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// JoinSessionParticipant creates or updates a session participant
func (h *Handlers) JoinSessionParticipant(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID from URL path
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 3 || pathParts[0] != "sessions" || pathParts[2] != "participants" {
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
	var req JoinParticipantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if req.ParticipantRef == "" {
		http.Error(w, "participant_ref is required", http.StatusBadRequest)
		return
	}

	// Upsert participant
	participant, err := h.DB.UpsertSessionParticipant(r.Context(), sessionID, req.ParticipantRef)
	if err != nil {
		log.Printf("Error upserting participant: %v", err)
		http.Error(w, fmt.Sprintf("Failed to join session: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(participant)
}

// CreateSessionEvent creates a new session event
func (h *Handlers) CreateSessionEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID from URL path
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 3 || pathParts[0] != "sessions" || pathParts[2] != "events" {
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
	var req CreateEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	// Validate event type
	eventType := models.SessionEventType(req.EventType)
	validTypes := []models.SessionEventType{
		models.SessionEventTypeJoin,
		models.SessionEventTypeLeave,
		models.SessionEventTypePlay,
		models.SessionEventTypePause,
		models.SessionEventTypeSeek,
		models.SessionEventTypeQuestion,
	}
	valid := false
	for _, vt := range validTypes {
		if eventType == vt {
			valid = true
			break
		}
	}
	if !valid {
		http.Error(w, fmt.Sprintf("Invalid event_type: %s", req.EventType), http.StatusBadRequest)
		return
	}

	// Create event
	event := &models.SessionEvent{
		ID:               uuid.New(),
		SessionID:        sessionID,
		ParticipantRef:   req.ParticipantRef,
		EventType:        eventType,
		VideoTimeSeconds: req.VideoTimeSeconds,
		Payload:          req.Payload,
	}
	if event.Payload == nil {
		event.Payload = make(map[string]interface{})
	}

	if err := h.DB.CreateSessionEvent(r.Context(), event); err != nil {
		log.Printf("Error creating session event: %v", err)
		http.Error(w, fmt.Sprintf("Failed to create event: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(event)
}

// AskSessionQuestion asks a question in a session context
func (h *Handlers) AskSessionQuestion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID from URL path
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 3 || pathParts[0] != "sessions" || pathParts[2] != "questions" {
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

	// Get artifacts for this session
	artifacts, err := h.DB.GetArtifactsBySessionID(r.Context(), sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get artifacts: %v", err), http.StatusInternalServerError)
		return
	}

	if len(artifacts) == 0 {
		http.Error(w, "No artifacts found for this session", http.StatusNotFound)
		return
	}

	// Use first artifact for question (or aggregate across all artifacts)
	artifact := artifacts[0]

	// Parse request body
	type AskSessionQuestionRequest struct {
		QuestionText     string `json:"question_text"`
		VideoTimeSeconds *int   `json:"video_time_seconds,omitempty"`
	}

	var req AskSessionQuestionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if req.QuestionText == "" {
		http.Error(w, "question_text is required", http.StatusBadRequest)
		return
	}

	// Check for existing question with same text in this session (repeat-question caching)
	existingQuestion, existingAnswer, err := h.DB.FindExistingQuestionByText(r.Context(), sessionID, req.QuestionText, nil)
	if err != nil {
		log.Printf("Error checking for existing session question: %v", err)
		// Continue with new question creation on error
	} else if existingQuestion != nil && existingAnswer != nil {
		// Return cached answer
		log.Printf("Returning cached answer for duplicate session question: %s", req.QuestionText)
		response := AskQuestionResponse{
			Question: existingQuestion,
			Answer:   existingAnswer,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK) // 200 OK for cached response
		json.NewEncoder(w).Encode(response)
		return
	}

	// Enforce per-session question limit
	count, err := h.DB.CountQuestionsBySessionID(r.Context(), sessionID)
	if err != nil {
		log.Printf("Error counting session questions: %v", err)
		http.Error(w, "Failed to check question limit", http.StatusInternalServerError)
		return
	}
	if count >= auth.Config.MaxQuestionsPerSession {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":          "session question limit reached",
			"max_questions":  auth.Config.MaxQuestionsPerSession,
		})
		return
	}

	// Create new question with session_id
	question := &models.Question{
		ID:               uuid.New(),
		ArtifactID:       artifact.ID,
		SessionID:        sessionID, // Questions belong to sessions (required)
		QuestionText:     req.QuestionText,
		QuestionSource:   models.QuestionSourceText,
		VideoTimeSeconds: req.VideoTimeSeconds, // Include timestamp if provided
	}

	if err := h.DB.CreateQuestion(r.Context(), question); err != nil {
		log.Printf("Error creating question: %v", err)
		http.Error(w, fmt.Sprintf("Failed to create question: %v", err), http.StatusInternalServerError)
		return
	}

	// Retrieve materials and video sources from session (active only)
	allMaterials, err := h.DB.GetActiveMaterialsBySessionID(r.Context(), sessionID)
	if err != nil {
		log.Printf("Warning: Failed to get materials: %v", err)
		allMaterials = []*models.Material{}
	}

	allVideoSources, err := h.DB.GetVideoSourcesBySessionID(r.Context(), sessionID)
	if err != nil {
		log.Printf("Warning: Failed to get video sources: %v", err)
		allVideoSources = []*models.VideoSource{}
	}

	// Use effective primary video for RAG so retrieval prefers primary transcript
	primaryVideo, _ := resolveEffectivePrimaryAndAdditional(allVideoSources)
	videoSource := primaryVideo

	// Perform retrieval
	chunks := utils.RetrieveChunks(req.QuestionText, allMaterials, videoSource, 5)

	// Get prior questions and answers from this session for context accumulation
	priorQuestions, priorAnswers, err := h.DB.GetQuestionsBySessionID(r.Context(), sessionID, 10)
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
	qaResponse, _, err := utils.GenerateAnswer(r.Context(), req.QuestionText, chunks, artifact.Title, utils.SessionContext{}, priorQA)
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

	// Broadcast question created event via WebSocket
	if h.Hub != nil {
		h.Hub.BroadcastQuestionCreated(sessionID, question)
		// Also broadcast answer created since it's created immediately
		h.Hub.BroadcastAnswerCreated(sessionID, answer)
	}

	response := AskQuestionResponse{
		Question: question,
		Answer:   answer,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}

// TranscribeSessionQuestionVoice accepts a short audio recording and returns a transcription.
// It does not persist audio or create a question. The client must confirm/edit and then call
// POST /sessions/{id}/questions with the resulting text.
func (h *Handlers) TranscribeSessionQuestionVoice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID from URL path: /sessions/{id}/questions/voice
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 4 || pathParts[0] != "sessions" || pathParts[2] != "questions" || pathParts[3] != "voice" {
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

	maxBytes := utils.VoiceMaxUploadBytesFromEnv()
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	if err := r.ParseMultipartForm(maxBytes); err != nil {
		http.Error(w, fmt.Sprintf("Invalid multipart form: %v", err), http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file is required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	tempFilePath, cleanup, err := saveMultipartToTempFile(file, header)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to store upload: %v", err), http.StatusInternalServerError)
		return
	}
	defer cleanup()

	transcriber := utils.NewSpeechToTextFromEnv()
	if !transcriber.CanTranscribe() {
		http.Error(w, "Speech-to-text is not available. Configure OPENAI_API_KEY (Whisper API) or WHISPER_CLI (local).", http.StatusServiceUnavailable)
		return
	}

	text, confidence, err := transcriber.TranscribeAudio(r.Context(), tempFilePath)
	if err != nil {
		if errors.Is(err, utils.ErrAudioTooLong) {
			http.Error(w, fmt.Sprintf("Audio exceeds maximum duration (STT_MAX_AUDIO_SECONDS). %v", err), http.StatusRequestEntityTooLarge)
			return
		}
		if errors.Is(err, utils.ErrDailyCapExceeded) {
			http.Error(w, "Daily speech-to-text limit reached. Try again tomorrow.", http.StatusTooManyRequests)
			return
		}
		http.Error(w, fmt.Sprintf("Transcription failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(transcribeVoiceResponse{
		TranscribedText: text,
		Confidence:      confidence,
	})
}

// CreateSessionAnswer allows admin or session creator to provide an answer to a question (RequireAuth).
func (h *Handlers) CreateSessionAnswer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user := UserFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	// Extract session ID and question ID from URL path: /sessions/{id}/questions/{question_id}/answers
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 5 || pathParts[0] != "sessions" || pathParts[2] != "questions" || pathParts[4] != "answers" {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}

	sessionID, err := uuid.Parse(pathParts[1])
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	questionID, err := uuid.Parse(pathParts[3])
	if err != nil {
		http.Error(w, "Invalid question ID", http.StatusBadRequest)
		return
	}

	// Verify session exists and user is admin or session creator
	session, err := h.DB.GetSession(r.Context(), sessionID)
	if err != nil || session == nil {
		http.Error(w, fmt.Sprintf("Session not found: %v", err), http.StatusNotFound)
		return
	}
	isCreator := session.CreatedBy != nil && *session.CreatedBy == user.Email
	isAdmin := user.GlobalRole == models.GlobalRoleAdmin
	if !isCreator && !isAdmin {
		http.Error(w, "Only the session creator or an admin can add an answer", http.StatusForbidden)
		return
	}

	// Verify question exists and belongs to this session
	question, err := h.DB.GetQuestionByID(r.Context(), questionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Question not found: %v", err), http.StatusNotFound)
		return
	}
	if question.SessionID != sessionID {
		http.Error(w, "Question does not belong to this session", http.StatusBadRequest)
		return
	}

	// Parse request body
	type CreateAnswerRequest struct {
		AnswerText string `json:"answer_text"`
		Status     string `json:"status,omitempty"` // "answered", "not_covered", "error"
	}

	var req CreateAnswerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if req.AnswerText == "" {
		http.Error(w, "answer_text is required", http.StatusBadRequest)
		return
	}

	// Determine answer status
	answerStatus := models.AnswerStatusAnswered
	if req.Status != "" {
		answerStatus = models.AnswerStatus(req.Status)
		// Validate status
		if answerStatus != models.AnswerStatusAnswered &&
			answerStatus != models.AnswerStatusNotCovered &&
			answerStatus != models.AnswerStatusError {
			answerStatus = models.AnswerStatusAnswered
		}
	}

	// Check if answer already exists - delete it to replace with new one
	existingAnswer, err := h.DB.GetAnswerByQuestionID(r.Context(), questionID)
	if err == nil && existingAnswer != nil {
		// Delete existing answer to replace it
		_ = h.DB.DeleteAnswer(r.Context(), existingAnswer.ID)
	}

	// Create new answer (human-provided: model=manual, answered_by=user email)
	answer := &models.Answer{
		ID:           uuid.New(),
		QuestionID:   questionID,
		AnswerText:   req.AnswerText,
		AnswerStatus: answerStatus,
		Confidence:   1.0, // Manual answers have full confidence
		Citations:    []models.Citation{},
		Model:        stringPtr("manual"),
		AnsweredBy:   stringPtr(user.Email),
	}

	if err := h.DB.CreateAnswer(r.Context(), answer); err != nil {
		log.Printf("Error creating answer: %v", err)
		http.Error(w, fmt.Sprintf("Failed to create answer: %v", err), http.StatusInternalServerError)
		return
	}

	// Broadcast answer created/updated event via WebSocket
	if h.Hub != nil {
		if existingAnswer != nil {
			h.Hub.BroadcastAnswerUpdated(sessionID, answer)
		} else {
			h.Hub.BroadcastAnswerCreated(sessionID, answer)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(answer)
}

// UpdateAnswerConfirmed updates the confirmed status of an answer (RequireAuth; admin or session creator only).
func (h *Handlers) UpdateAnswerConfirmed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch && r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user := UserFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	// Extract session ID and answer ID from URL path: /sessions/{id}/answers/{answer_id}/confirm
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 5 || pathParts[0] != "sessions" || pathParts[2] != "answers" || pathParts[4] != "confirm" {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}

	sessionID, err := uuid.Parse(pathParts[1])
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	answerID, err := uuid.Parse(pathParts[3])
	if err != nil {
		http.Error(w, "Invalid answer ID", http.StatusBadRequest)
		return
	}

	// Verify session exists and user is admin or session creator
	session, err := h.DB.GetSession(r.Context(), sessionID)
	if err != nil || session == nil {
		http.Error(w, fmt.Sprintf("Session not found: %v", err), http.StatusNotFound)
		return
	}
	isCreator := session.CreatedBy != nil && *session.CreatedBy == user.Email
	isAdmin := user.GlobalRole == models.GlobalRoleAdmin
	if !isCreator && !isAdmin {
		http.Error(w, "Only the session creator or an admin can confirm an answer", http.StatusForbidden)
		return
	}

	// Get the answer to verify it exists
	answer, err := h.DB.GetAnswerByID(r.Context(), answerID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Answer not found: %v", err), http.StatusNotFound)
		return
	}
	if answer == nil {
		http.Error(w, "Answer not found", http.StatusNotFound)
		return
	}

	// Verify the question belongs to this session
	question, err := h.DB.GetQuestionByID(r.Context(), answer.QuestionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Question not found: %v", err), http.StatusNotFound)
		return
	}
	if question.SessionID != sessionID {
		http.Error(w, "Answer does not belong to this session", http.StatusBadRequest)
		return
	}

	// Only allow confirmation for answers with status "answered"
	if answer.AnswerStatus != models.AnswerStatusAnswered {
		http.Error(w, "Only answers with status 'answered' can be confirmed", http.StatusBadRequest)
		return
	}

	// Parse request body
	type UpdateConfirmedRequest struct {
		Confirmed bool `json:"confirmed"`
	}

	var req UpdateConfirmedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	// Update confirmed status
	if err := h.DB.UpdateAnswerConfirmed(r.Context(), answerID, req.Confirmed); err != nil {
		log.Printf("Error updating answer confirmed status: %v", err)
		http.Error(w, fmt.Sprintf("Failed to update confirmed status: %v", err), http.StatusInternalServerError)
		return
	}

	// Get updated answer
	updatedAnswer, err := h.DB.GetAnswerByID(r.Context(), answerID)
	if err != nil || updatedAnswer == nil {
		log.Printf("Warning: Failed to get updated answer: %v", err)
		// Still return success, but use the original answer
		updatedAnswer = answer
		updatedAnswer.Confirmed = req.Confirmed
	}

	// Broadcast via WebSocket
	if h.Hub != nil {
		h.Hub.BroadcastAnswerUpdated(sessionID, updatedAnswer)
	}

	// Return updated answer
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(updatedAnswer)
}

// TranscribeSessionAnswerVoice accepts a short audio recording for an answer and returns a transcription (RequireAuth; admin or creator).
func (h *Handlers) TranscribeSessionAnswerVoice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user := UserFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	// Extract session ID and question ID from URL path: /sessions/{id}/questions/{question_id}/answers/voice
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 6 || pathParts[0] != "sessions" || pathParts[2] != "questions" || pathParts[4] != "answers" || pathParts[5] != "voice" {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}

	sessionID, err := uuid.Parse(pathParts[1])
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	questionID, err := uuid.Parse(pathParts[3])
	if err != nil {
		http.Error(w, "Invalid question ID", http.StatusBadRequest)
		return
	}

	// Verify session exists and user is admin or session creator
	session, err := h.DB.GetSession(r.Context(), sessionID)
	if err != nil || session == nil {
		http.Error(w, fmt.Sprintf("Session not found: %v", err), http.StatusNotFound)
		return
	}
	isCreator := session.CreatedBy != nil && *session.CreatedBy == user.Email
	isAdmin := user.GlobalRole == models.GlobalRoleAdmin
	if !isCreator && !isAdmin {
		http.Error(w, "Only the session creator or an admin can provide an answer", http.StatusForbidden)
		return
	}

	// Verify question exists and belongs to this session
	question, err := h.DB.GetQuestionByID(r.Context(), questionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Question not found: %v", err), http.StatusNotFound)
		return
	}
	if question.SessionID != sessionID {
		http.Error(w, "Question does not belong to this session", http.StatusBadRequest)
		return
	}

	maxBytes := utils.VoiceMaxUploadBytesFromEnv()
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	if err := r.ParseMultipartForm(maxBytes); err != nil {
		http.Error(w, fmt.Sprintf("Invalid multipart form: %v", err), http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file is required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	tempFilePath, cleanup, err := saveMultipartToTempFile(file, header)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to store upload: %v", err), http.StatusInternalServerError)
		return
	}
	defer cleanup()

	transcriber := utils.NewSpeechToTextFromEnv()
	if !transcriber.CanTranscribe() {
		http.Error(w, "Speech-to-text is not available. Configure OPENAI_API_KEY (Whisper API) or WHISPER_CLI (local).", http.StatusServiceUnavailable)
		return
	}

	text, confidence, err := transcriber.TranscribeAudio(r.Context(), tempFilePath)
	if err != nil {
		if errors.Is(err, utils.ErrAudioTooLong) {
			http.Error(w, fmt.Sprintf("Audio exceeds maximum duration (STT_MAX_AUDIO_SECONDS). %v", err), http.StatusRequestEntityTooLarge)
			return
		}
		if errors.Is(err, utils.ErrDailyCapExceeded) {
			http.Error(w, "Daily speech-to-text limit reached. Try again tomorrow.", http.StatusTooManyRequests)
			return
		}
		http.Error(w, fmt.Sprintf("Transcription failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(transcribeVoiceResponse{
		TranscribedText: text,
		Confidence:      confidence,
	})
}

func stringPtr(s string) *string {
	return &s
}

func saveMultipartToTempFile(file multipart.File, header *multipart.FileHeader) (string, func(), error) {
	ext := ""
	if header != nil && header.Filename != "" {
		ext = filepath.Ext(header.Filename)
	}
	if ext == "" {
		ext = ".webm"
	}

	// Temp location for voice recording (not under session storage; short-lived, cleaned up).
	dir := filepath.Join("tmp", "voice")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", nil, err
	}

	dstPath := filepath.Join(dir, fmt.Sprintf("voice_%d%s", time.Now().UnixNano(), ext))
	out, err := os.Create(dstPath)
	if err != nil {
		return "", nil, err
	}
	defer out.Close()

	if _, err := out.ReadFrom(file); err != nil {
		_ = os.Remove(dstPath)
		return "", nil, err
	}

	// Verify it exists and has some content.
	if info, err := os.Stat(dstPath); err != nil {
		_ = os.Remove(dstPath)
		return "", nil, err
	} else if info.Size() == 0 {
		_ = os.Remove(dstPath)
		return "", nil, fmt.Errorf("uploaded audio file is empty")
	}

	// Use absolute path so downstream tools (whisper) are not sensitive to process working directory.
	absPath, err := filepath.Abs(dstPath)
	if err != nil {
		_ = os.Remove(dstPath)
		return "", nil, err
	}

	cleanup := func() {
		_ = os.Remove(absPath)
	}

	return absPath, cleanup, nil
}

// GetSessionQuestions retrieves questions for a session
func (h *Handlers) GetSessionQuestions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID from URL path
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 3 || pathParts[0] != "sessions" || pathParts[2] != "questions" {
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

	// Get questions with answers
	questions, answers, err := h.DB.GetQuestionsBySessionID(r.Context(), sessionID, 20)
	if err != nil {
		log.Printf("Error getting questions: %v", err)
		http.Error(w, fmt.Sprintf("Failed to get questions: %v", err), http.StatusInternalServerError)
		return
	}

	// Enrich citations with source_id from chunks so participant view can open documents (e.g. "Slide 1")
	chunks, _ := h.DB.ListSessionChunksBySessionID(r.Context(), sessionID)
	chunkSourceByID := make(map[string]struct{ SourceID, SourceType string })
	for _, c := range chunks {
		if c.SourceID != nil {
			chunkSourceByID[c.ID.String()] = struct{ SourceID, SourceType string }{
				SourceID:   c.SourceID.String(),
				SourceType: c.SourceType,
			}
		}
	}
	for _, a := range answers {
		if a == nil {
			continue
		}
		for i := range a.Citations {
			c := &a.Citations[i]
			if c.SourceID != "" {
				continue
			}
			if info, ok := chunkSourceByID[c.ChunkID]; ok {
				c.SourceID = info.SourceID
				if c.SourceType == "" {
					c.SourceType = info.SourceType
				}
			}
		}
	}

	response := GetQuestionsResponse{
		Questions: questions,
		Answers:   answers,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// UpdateSessionStatus updates the status of a session (e.g., close it)
func (h *Handlers) UpdateSessionStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch && r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID from URL path (e.g., /sessions/{id})
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 2 || pathParts[0] != "sessions" {
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
	type UpdateSessionRequest struct {
		Status          *string `json:"status"`           // "open" or "closed" (optional)
		Premise         *string `json:"premise"`           // session premise (optional)
		PrimaryDecision *string `json:"primary_decision"`  // primary decision (optional)
		DecisionOutcome *string `json:"decision_outcome"`  // decision outcome (optional)
	}

	var req UpdateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	// Update status if provided
	if req.Status != nil {
		status := models.SessionStatus(*req.Status)
		if status != models.SessionStatusOpen && status != models.SessionStatusClosed {
			http.Error(w, fmt.Sprintf("Invalid status: %s. Must be 'open' or 'closed'", *req.Status), http.StatusBadRequest)
			return
		}
		if err = h.DB.UpdateSessionStatus(r.Context(), sessionID, status); err != nil {
			log.Printf("Error updating session status: %v", err)
			http.Error(w, fmt.Sprintf("Failed to update session status: %v", err), http.StatusInternalServerError)
			return
		}
	}

	// Update premise / primary_decision / decision_outcome if any is provided
	if req.Premise != nil || req.PrimaryDecision != nil || req.DecisionOutcome != nil {
		if err = h.DB.UpdateSessionContext(r.Context(), sessionID, req.Premise, req.PrimaryDecision, req.DecisionOutcome); err != nil {
			log.Printf("Error updating session context: %v", err)
			http.Error(w, fmt.Sprintf("Failed to update session context: %v", err), http.StatusInternalServerError)
			return
		}
	}

	// Get updated session
	session, err := h.DB.GetSession(r.Context(), sessionID)
	if err != nil {
		log.Printf("Error getting updated session: %v", err)
		http.Error(w, fmt.Sprintf("Failed to get updated session: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(session)
}

// GetSessionTimeline retrieves the timeline for a session (ordered questions and answers)
// This returns questions and answers in chronological order (oldest first) for timeline replay
func (h *Handlers) GetSessionTimeline(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID from URL path (e.g., /sessions/{id}/timeline)
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 3 || pathParts[0] != "sessions" || pathParts[2] != "timeline" {
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

	// Get limit from query parameter (default 100)
	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsedLimit, err := fmt.Sscanf(limitStr, "%d", &limit); err != nil || parsedLimit != 1 || limit <= 0 {
			limit = 100
		}
	}

	// Get timeline (ordered questions and answers)
	questions, answers, err := h.DB.GetSessionTimeline(r.Context(), sessionID, limit)
	if err != nil {
		log.Printf("Error getting session timeline: %v", err)
		http.Error(w, fmt.Sprintf("Failed to get timeline: %v", err), http.StatusInternalServerError)
		return
	}

	type TimelineEntry struct {
		Question      *models.Question `json:"question"`
		Answer        *models.Answer   `json:"answer,omitempty"`
		Timestamp     string           `json:"timestamp"`
		VideoTimeSecs *int             `json:"video_time_seconds,omitempty"`
	}

	// Build timeline entries (questions and answers paired)
	timeline := make([]TimelineEntry, 0, len(questions))

	// Create a map of question ID to answer for quick lookup
	answerMap := make(map[uuid.UUID]*models.Answer)
	for _, answer := range answers {
		answerMap[answer.QuestionID] = answer
	}

	// Create timeline entries in chronological order
	for _, question := range questions {
		entry := TimelineEntry{
			Question:      question,
			Answer:        answerMap[question.ID],
			Timestamp:     question.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			VideoTimeSecs: question.VideoTimeSeconds,
		}
		timeline = append(timeline, entry)
	}

	type GetTimelineResponse struct {
		SessionID string          `json:"session_id"`
		Timeline  []TimelineEntry `json:"timeline"`
		Count     int             `json:"count"`
	}

	response := GetTimelineResponse{
		SessionID: sessionID.String(),
		Timeline:  timeline,
		Count:     len(timeline),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// ZoomVideoStream is deprecated for playback: app uses in-app player only (primary video from R2/local).
// When session has primary_video_artifact_id this returns 410. Legacy sessions without primary could use it; frontend no longer requests it for playback.
// GET /sessions/{sessionId}/video-sources/{videoSourceId}/stream
func (h *Handlers) ZoomVideoStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 5 || pathParts[0] != "sessions" || pathParts[2] != "video-sources" || pathParts[4] != "stream" {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(pathParts[1])
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	videoSourceID, err := uuid.Parse(pathParts[3])
	if err != nil {
		http.Error(w, "Invalid video source ID", http.StatusBadRequest)
		return
	}
	session, err := h.DB.GetSession(r.Context(), sessionID)
	if err != nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}
	vs, err := h.DB.GetVideoSourceByID(r.Context(), videoSourceID)
	if err != nil || vs == nil {
		http.Error(w, "Video source not found", http.StatusNotFound)
		return
	}
	if vs.SessionID != sessionID {
		http.Error(w, "Video source does not belong to this session", http.StatusBadRequest)
		return
	}
	// Upload sources (e.g. video files from materials): stream from StoredVideoObjectKey (R2 or local).
	// Try R2 first when configured (e.g. Render) so R2-stored uploads play; fall back to local path when key looks like sessions/ or data/.
	if vs.SourceType == models.VideoSourceTypeUpload && vs.StoredVideoObjectKey != nil && *vs.StoredVideoObjectKey != "" {
		key := *vs.StoredVideoObjectKey
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Accept-Ranges", "bytes")
		if h.Storage != nil {
			exists, size, contentType, headErr := h.Storage.Head(r.Context(), key)
			if headErr == nil && exists {
				if contentType != "" {
					w.Header().Set("Content-Type", contentType)
				}
				if size > 0 {
					w.Header().Set("Content-Length", fmt.Sprintf("%d", size))
				}
				w.WriteHeader(http.StatusOK)
				if r.Method == http.MethodHead {
					return
				}
				rc, err := h.Storage.Get(r.Context(), key)
				if err != nil {
					log.Printf("ZoomVideoStream R2 Get %s: %v", key, err)
					return
				}
				defer rc.Close()
				_, _ = io.Copy(w, rc)
				return
			}
		}
		if strings.HasPrefix(key, "sessions/") || strings.HasPrefix(key, "data/") {
			absPath := filepath.Join(storage.UploadRoot(), filepath.FromSlash(key))
			f, err := os.Open(absPath)
			if err != nil {
				if os.IsNotExist(err) {
					http.Error(w, "Video file not found", http.StatusNotFound)
					return
				}
				log.Printf("ZoomVideoStream open %s: %v", absPath, err)
				http.Error(w, "Failed to open video", http.StatusInternalServerError)
				return
			}
			defer f.Close()
			info, err := f.Stat()
			if err != nil {
				http.Error(w, "Failed to stat video", http.StatusInternalServerError)
				return
			}
			modTime := info.ModTime()
			if r.Method == http.MethodHead {
				w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
				w.WriteHeader(http.StatusOK)
				return
			}
			http.ServeContent(w, r, "video.mp4", modTime, f)
			return
		}
		http.Error(w, "Video not found", http.StatusNotFound)
		return
	}
	// Playback is only from R2 when primary_video_artifact_id is set; do not proxy Zoom.
	if session.PrimaryVideoArtifactID != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusGone)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "Video not ingested",
			"message": "Video is served from R2. Use the session's video_access_url (from GET /api/sessions/:id) for playback.",
		})
		return
	}
	// Use session creator, or fallback to creator_identity query param (for sessions created before we set CreatedBy)
	creatorIdentity := ""
	if session.CreatedBy != nil && *session.CreatedBy != "" {
		creatorIdentity = *session.CreatedBy
	}
	if creatorIdentity == "" {
		creatorIdentity = r.URL.Query().Get("creator_identity")
	}
	if creatorIdentity == "" {
		http.Error(w, "Session has no creator. Reconnect Zoom and create a new session from Zoom, or append ?creator_identity=your_id to the stream URL.", http.StatusForbidden)
		return
	}
	if vs.Provider != "zoom" {
		http.Error(w, "Not a Zoom video source", http.StatusBadRequest)
		return
	}
	zoomURL := vs.VideoURL
	if vs.OriginalURL != nil && *vs.OriginalURL != "" {
		zoomURL = *vs.OriginalURL
	}
	meetingID, err := utils.ParseZoomRecordingURL(zoomURL)
	if err != nil {
		log.Printf("Zoom video stream: parse URL %q: %v", zoomURL, err)
		http.Error(w, "Invalid Zoom recording URL", http.StatusBadRequest)
		return
	}
	accessToken, _, err := h.GetValidZoomAccessToken(r, creatorIdentity)
	if err != nil {
		log.Printf("Zoom video stream: get token for creator %q: %v", creatorIdentity, err)
		msg := "Zoom not connected for this session's creator. The creator should connect (or reconnect) Zoom in TalkBack Settings once; then the video works for everyone without anyone logging into Zoom."
		if strings.Contains(err.Error(), "expired") || strings.Contains(err.Error(), "revoked") {
			msg = "Session creator's Zoom connection has expired. Ask the session creator to reconnect Zoom in TalkBack Settings."
		}
		http.Error(w, msg, http.StatusForbidden)
		return
	}
	rec, err := utils.GetMeetingRecordingsWithRetry(accessToken, meetingID)
	if err != nil {
		log.Printf("Zoom video stream: get recordings: %v", err)
		if err.Error() == "recording not found" {
			http.Error(w, "Zoom recording not found. It may have been deleted, expired, or the link may be for a different Zoom account.", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to load Zoom recording", http.StatusInternalServerError)
		return
	}
	mp4 := utils.FindMP4RecordingFile(rec.RecordingFiles)
	if mp4 == nil || mp4.DownloadURL == "" {
		http.Error(w, "No MP4 recording available for this meeting", http.StatusNotFound)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), "GET", mp4.DownloadURL, nil)
	if err != nil {
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	// Forward Range so browser video element can request byte ranges (required for play/seek)
	if rangeH := r.Header.Get("Range"); rangeH != "" {
		req.Header.Set("Range", rangeH)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("Zoom video stream: download %s: %v", mp4.DownloadURL, err)
		http.Error(w, "Failed to stream video from Zoom", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("Zoom video stream: Zoom returned %d: %s", resp.StatusCode, string(body))
		http.Error(w, "Zoom returned error", resp.StatusCode)
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Accept-Ranges", "bytes")
	if resp.ContentLength >= 0 {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", resp.ContentLength))
	}
	if resp.Header.Get("Content-Range") != "" {
		w.Header().Set("Content-Range", resp.Header.Get("Content-Range"))
	}
	// Pass through 206 Partial Content so video element can play and seek
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}
