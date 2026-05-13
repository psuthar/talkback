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

// UpdateVideoSourceDisplayTitle sets or clears the display_title on a video source. Creator or admin only.
// PATCH /sessions/{sessionId}/video-sources/{videoSourceId}/display-title
// Also accepts /api/sessions/... in case APISessionsRouter ever dispatches here.
// SCRUM-400: the SPA's PATCH currently hits the non-/api path (handled by
// SessionsRouter at router.go:648); the previous strict-/api check returned
// 400 on every call. Strip a leading "api" segment if present, then validate
// the remaining shape.
func (h *Handlers) UpdateVideoSourceDisplayTitle(w http.ResponseWriter, r *http.Request) {
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) > 0 && pathParts[0] == "api" {
		pathParts = pathParts[1:]
	}
	// expected after normalization: sessions / {id} / video-sources / {vsID} / display-title
	if len(pathParts) != 5 || pathParts[0] != "sessions" || pathParts[2] != "video-sources" || pathParts[4] != "display-title" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
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
	if err != nil || session == nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}
	// Ownership: header/query path is the legacy admin-utility shape; the SPA
	// uses cookie-auth (credentials: 'include') with no X-Current-User. Fall
	// back to UserFromContext when the header is empty so the session creator
	// can rename their own video — SCRUM-436.
	currentUser := r.Header.Get("X-Current-User")
	if currentUser == "" {
		currentUser = r.URL.Query().Get("user")
	}
	if currentUser == "" {
		if u := UserFromContext(r.Context()); u != nil {
			currentUser = u.Email
		}
	}
	if session.CreatedBy != nil && *session.CreatedBy != currentUser {
		if u := UserFromContext(r.Context()); u == nil || u.GlobalRole != models.GlobalRoleAdmin {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
	}
	var body struct {
		DisplayTitle *string `json:"display_title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if err := h.DB.UpdateVideoSourceDisplayTitle(r.Context(), sessionID, videoSourceID, body.DisplayTitle); err != nil {
		log.Printf("UpdateVideoSourceDisplayTitle: %v", err)
		http.Error(w, "Video source not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Display title updated"})
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
	outReq, err := http.NewRequestWithContext(r.Context(), "GET", mp4.DownloadURL, nil)
	if err != nil {
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}
	outReq.Header.Set("Authorization", "Bearer "+accessToken)
	// Forward Range so browser video element can request byte ranges (required for play/seek)
	if rangeH := r.Header.Get("Range"); rangeH != "" {
		outReq.Header.Set("Range", rangeH)
	}
	resp, err := http.DefaultClient.Do(outReq)
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
