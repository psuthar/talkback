package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/utils"
)

func zoomMaxVideoDurationSeconds() int64 {
	v := os.Getenv("ZOOM_MAX_VIDEO_DURATION_SECONDS")
	if v == "" {
		return 600
	}
	n, _ := strconv.ParseInt(v, 10, 64)
	if n <= 0 {
		return 600
	}
	return n
}

// ZoomImportRequest body for POST /api/zoom/import (create session + start import in one call)
type ZoomImportRequest struct {
	Title       string `json:"title"`
	MeetingUUID string `json:"meeting_uuid"`
	InstanceUUID string `json:"instance_uuid,omitempty"`
}

// ZoomImportResponse for POST /api/zoom/import
type ZoomImportResponse struct {
	ID     string `json:"id"`
	JobID  string `json:"job_id"`
	State  string `json:"state"`
}

// ZoomImport creates a session and starts Zoom import in one request (RequireAuth; requires admin or creator role).
func (h *Handlers) ZoomImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"message": "Method not allowed"})
		return
	}
	user := UserFromContext(r.Context())
	if user == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"code": "unauthorized", "message": "unauthorized"})
		return
	}
	if !user.GlobalRole.CanCreateSessions() {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"code": "forbidden", "message": "your role does not allow creating sessions"})
		return
	}
	creatorIdentity := r.Header.Get("X-Creator-Identity")
	if creatorIdentity == "" {
		creatorIdentity = r.URL.Query().Get("creator_identity")
	}
	if creatorIdentity == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"code": "zoom_not_connected", "message": "creator_identity required"})
		return
	}
	if creatorIdentity != user.Email {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"code": "forbidden", "message": "creator identity must match the authenticated user"})
		return
	}
	if _, _, err := h.GetValidZoomAccessToken(r, creatorIdentity); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"code": "zoom_not_connected", "message": "Zoom not connected. Connect Zoom first."})
		return
	}
	var req ZoomImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"message": "Invalid request body"})
		return
	}
	meetingUUID := strings.TrimSpace(req.MeetingUUID)
	if meetingUUID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"message": "meeting_uuid required"})
		return
	}
	instanceUUID := strings.TrimSpace(req.InstanceUUID)
	if instanceUUID == "" {
		instanceUUID = meetingUUID
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "Zoom Recording"
	}

	// Check for duplicate session title so user can be prompted to choose a unique name
	exists, err := h.DB.SessionWithTitleExistsForCreator(r.Context(), creatorIdentity, title, nil)
	if err != nil {
		log.Printf("ZoomImport SessionWithTitleExistsForCreator: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": "Failed to check session title"})
		return
	}
	if exists {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"message": "A session with this name already exists. Please use a unique name."})
		return
	}

	// Enforce Zoom max duration before creating session (return 422 so UI can show friendly error).
	accessToken, _, tokenErr := h.GetValidZoomAccessToken(r, creatorIdentity)
	if tokenErr == nil {
		rec, recErr := utils.GetMeetingRecordingsWithRetry(accessToken, instanceUUID)
		if recErr == nil && rec != nil && rec.Duration > 0 {
			maxSec := zoomMaxVideoDurationSeconds()
			durationSec := rec.Duration * 60
			if int64(durationSec) > maxSec {
				maxMin := maxSec / 60
				msg := "Demo limit: Zoom recordings must be 10 minutes or less."
				if maxMin != 10 {
					msg = "Demo limit: Zoom recordings must be " + strconv.FormatInt(maxMin, 10) + " minutes or less."
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnprocessableEntity)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"message":              msg,
					"reason_code":          "zoom_duration_limit",
					"duration_minutes":     rec.Duration,
					"max_duration_seconds": maxSec,
				})
				return
			}
		}
	}

	sessionID := uuid.New()
	session := &models.Session{
		ID:             sessionID,
		Title:          title,
		CreatedBy:      &creatorIdentity,
		Status:         models.SessionStatusOpen,
		SourceProvider: models.SessionSourceZoom,
	}
	if err := h.DB.CreateSession(r.Context(), session); err != nil {
		log.Printf("ZoomImport create session error: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": "Failed to create session"})
		return
	}
	if _, err := h.DB.CreateArtifact(r.Context(), sessionID, title, nil); err != nil {
		log.Printf("ZoomImport create artifact error: %v", err)
		// non-fatal
	}

	jobID := uuid.New()
	job := &models.SessionProcessingJob{
		ID:              jobID,
		SessionID:       sessionID,
		Source:          "zoom",
		State:           models.ProcessingStateQueued,
		Stage:           models.ProcessingStageFetch,
		MeetingUUID:     &meetingUUID,
		InstanceUUID:    &instanceUUID,
		CreatorIdentity: &creatorIdentity,
	}
	if err := h.DB.CreateOrGetSessionProcessingJob(r.Context(), job); err != nil {
		log.Printf("ZoomImport create job error: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": "Failed to create import job"})
		return
	}
	_ = h.DB.UpdateSessionProcessingMirror(r.Context(), sessionID, job.State)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(ZoomImportResponse{
		ID:    sessionID.String(),
		JobID: job.ID.String(),
		State: job.State,
	})
}

// SessionImportZoomRequest body for POST /api/sessions/:id/import/zoom
type SessionImportZoomRequest struct {
	MeetingUUID  string `json:"meeting_uuid"`
	InstanceUUID string `json:"instance_uuid,omitempty"`
}

// SessionImportZoomResponse for 202 response
type SessionImportZoomResponse struct {
	JobID string `json:"job_id"`
	State string `json:"state"`
}

// IngestionStatusResponse for GET /api/sessions/:id/ingestion
type IngestionStatusResponse struct {
	Source       string `json:"source"`
	State        string `json:"state"`
	LastError    string `json:"last_error,omitempty"`
	UpdatedAt    string `json:"updated_at"`
	MeetingUUID  string `json:"meeting_uuid,omitempty"`  // For Retry
	InstanceUUID string `json:"instance_uuid,omitempty"` // For Retry
}

// ProcessingStatusResponse for GET /api/sessions/:id/processing (Mission #4)
type ProcessingStatusResponse struct {
	State            string  `json:"state"`
	Stage            string  `json:"stage"`
	AttemptCount     int     `json:"attempt_count"`
	NextRetryAt      *string `json:"next_retry_at,omitempty"`
	LastErrorCode    string  `json:"last_error_code,omitempty"`
	LastErrorMessage string  `json:"last_error_message,omitempty"`
	UpdatedAt        string  `json:"updated_at"`
}

// SessionImportZoom handles POST /api/sessions/:id/import/zoom
func (h *Handlers) SessionImportZoom(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	creatorIdentity := r.Header.Get("X-Creator-Identity")
	if creatorIdentity == "" {
		creatorIdentity = r.URL.Query().Get("creator_identity")
	}
	if creatorIdentity == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"code": "zoom_not_connected", "message": "creator_identity required"})
		return
	}
	// Parse session ID from path: /api/sessions/:id/import/zoom or /sessions/:id/import/zoom
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	var sessionIDStr string
	if len(pathParts) >= 4 && pathParts[0] == "api" && pathParts[1] == "sessions" && pathParts[3] == "import" {
		sessionIDStr = pathParts[2]
	} else if len(pathParts) >= 4 && pathParts[0] == "sessions" && pathParts[3] == "import" {
		sessionIDStr = pathParts[1]
	} else {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(sessionIDStr)
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	session, err := h.DB.GetSession(r.Context(), sessionID)
	if err != nil || session == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"message": "Session not found"})
		return
	}
	var req SessionImportZoomRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"message": "Invalid request body"})
		return
	}
	if strings.TrimSpace(req.MeetingUUID) == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"message": "meeting_uuid required"})
		return
	}
	meetingUUID := strings.TrimSpace(req.MeetingUUID)
	instanceUUID := strings.TrimSpace(req.InstanceUUID)
	if instanceUUID == "" {
		instanceUUID = meetingUUID
	}
	accessToken, _, err := h.GetValidZoomAccessToken(r, creatorIdentity)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"code": "zoom_not_connected", "message": "Zoom not connected. Connect Zoom first."})
		return
	}
	// Enforce Zoom max duration before creating job (return 422 so UI can show friendly error).
	maxSec := zoomMaxVideoDurationSeconds()
	rec, recErr := utils.GetMeetingRecordingsWithRetry(accessToken, instanceUUID)
	if recErr == nil && rec != nil && rec.Duration > 0 {
		durationSec := rec.Duration * 60
		if int64(durationSec) > maxSec {
			maxMin := maxSec / 60
			msg := "Demo limit: Zoom recordings must be 10 minutes or less."
			if maxMin != 10 {
				msg = "Demo limit: Zoom recordings must be " + strconv.FormatInt(maxMin, 10) + " minutes or less."
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"message":                 msg,
				"reason_code":             "zoom_duration_limit",
				"duration_minutes":        rec.Duration,
				"max_duration_seconds":    maxSec,
			})
			return
		}
	}
	if recErr != nil {
		log.Printf("SessionImportZoom: pre-check recordings failed (will let job run): %v", recErr)
	}
	jobID := uuid.New()
	meetingUUIDPtr := &meetingUUID
	instanceUUIDPtr := &instanceUUID
	creatorIdentityPtr := &creatorIdentity
	job := &models.SessionProcessingJob{
		ID:              jobID,
		SessionID:       sessionID,
		Source:          "zoom",
		State:           models.ProcessingStateQueued,
		Stage:           models.ProcessingStageFetch,
		MeetingUUID:     meetingUUIDPtr,
		InstanceUUID:    instanceUUIDPtr,
		CreatorIdentity: creatorIdentityPtr,
	}
	if err := h.DB.CreateOrGetSessionProcessingJob(r.Context(), job); err != nil {
		log.Printf("Create session processing job error: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": "Failed to create import job"})
		return
	}
	_ = h.DB.UpdateSessionProcessingMirror(r.Context(), sessionID, job.State)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(SessionImportZoomResponse{
		JobID: job.ID.String(),
		State: job.State,
	})
}

// SessionIngestionStatus handles GET /api/sessions/:id/ingestion
func (h *Handlers) SessionIngestionStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	var sessionIDStr string
	if len(pathParts) >= 4 && pathParts[0] == "api" && pathParts[1] == "sessions" && pathParts[3] == "ingestion" {
		sessionIDStr = pathParts[2]
	} else if len(pathParts) >= 4 && pathParts[0] == "sessions" && pathParts[3] == "ingestion" {
		sessionIDStr = pathParts[1]
	} else {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(sessionIDStr)
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	job, err := h.DB.GetIngestionJobBySessionID(r.Context(), sessionID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": err.Error()})
		return
	}
	if job == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"source": "", "state": "", "updated_at": ""})
		return
	}
	lastError := ""
	if job.LastError != nil {
		lastError = *job.LastError
	}
	meetingUUID := ""
	if job.MeetingUUID != nil {
		meetingUUID = *job.MeetingUUID
	}
	instanceUUID := ""
	if job.InstanceUUID != nil {
		instanceUUID = *job.InstanceUUID
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(IngestionStatusResponse{
		Source:       job.Source,
		State:        job.State,
		LastError:    lastError,
		UpdatedAt:    job.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		MeetingUUID:  meetingUUID,
		InstanceUUID: instanceUUID,
	})
}

// sessionSourceForID returns the job source string for a session by looking up the
// session's source_provider field. This ensures status/retry/cancel operations target
// the correct job row regardless of video provider.
// On any DB error or unrecognised provider it returns "" which causes the DB helpers
// to apply their own backward-compatible "zoom" default.
func sessionSourceForID(ctx context.Context, db *database.DB, sessionID uuid.UUID) string {
	sess, err := db.GetSession(ctx, sessionID)
	if err != nil || sess == nil {
		return "" // DB default ("zoom") will apply
	}
	return string(sess.SourceProvider)
}

// parseSessionIDFromProcessingPath extracts session ID from /api/sessions/:id/processing or /api/sessions/:id/processing/retry etc.
func parseSessionIDFromProcessingPath(path string) (string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 4 && parts[0] == "api" && parts[1] == "sessions" && parts[3] == "processing" {
		return parts[2], true
	}
	if len(parts) >= 4 && parts[0] == "sessions" && parts[3] == "processing" {
		return parts[1], true
	}
	return "", false
}

// SessionProcessingStatus handles GET /api/sessions/:id/processing (Mission #4)
func (h *Handlers) SessionProcessingStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sessionIDStr, ok := parseSessionIDFromProcessingPath(r.URL.Path)
	if !ok {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(sessionIDStr)
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	// Derive source from the session's source_provider so non-Zoom sessions are not
	// permanently stuck. Falls back to "zoom" (via DB default) if the session is not found.
	source := sessionSourceForID(r.Context(), h.DB, sessionID)
	job, err := h.DB.GetSessionProcessingJobBySessionID(r.Context(), sessionID, source)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": err.Error()})
		return
	}
	if job == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(ProcessingStatusResponse{
			State:     "",
			Stage:     "",
			UpdatedAt: "",
		})
		return
	}
	var nextRetryAt *string
	if job.NextRetryAt != nil {
		s := job.NextRetryAt.Format("2006-01-02T15:04:05Z07:00")
		nextRetryAt = &s
	}
	lastCode := ""
	if job.LastErrorCode != nil {
		lastCode = *job.LastErrorCode
	}
	lastMsg := ""
	if job.LastErrorMessage != nil {
		lastMsg = *job.LastErrorMessage
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(ProcessingStatusResponse{
		State:            job.State,
		Stage:            job.Stage,
		AttemptCount:     job.AttemptCount,
		NextRetryAt:      nextRetryAt,
		LastErrorCode:    lastCode,
		LastErrorMessage: lastMsg,
		UpdatedAt:        job.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}

// SessionProcessingRetry handles POST /api/sessions/:id/processing/retry
func (h *Handlers) SessionProcessingRetry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sessionIDStr, ok := parseSessionIDFromProcessingPath(r.URL.Path)
	if !ok {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(sessionIDStr)
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	// Derive source from the session's source_provider so retry works for any provider.
	source := sessionSourceForID(r.Context(), h.DB, sessionID)
	if err := h.DB.RetrySessionProcessingJob(r.Context(), sessionID, source); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": err.Error()})
		return
	}
	_ = h.DB.UpdateSessionProcessingMirror(r.Context(), sessionID, models.ProcessingStateQueued)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"state": "queued"})
}

// SessionProcessingCancel handles POST /api/sessions/:id/processing/cancel
func (h *Handlers) SessionProcessingCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sessionIDStr, ok := parseSessionIDFromProcessingPath(r.URL.Path)
	if !ok {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(sessionIDStr)
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	// Derive source from the session's source_provider so cancel works for any provider.
	source := sessionSourceForID(r.Context(), h.DB, sessionID)
	if err := h.DB.CancelSessionProcessingJob(r.Context(), sessionID, source); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": err.Error()})
		return
	}
	_ = h.DB.UpdateSessionProcessingMirror(r.Context(), sessionID, models.ProcessingStateCanceled)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"state": "canceled"})
}

// SessionTranscriptResponse for GET /api/sessions/:id/transcript
type SessionTranscriptResponse struct {
	Status       string                     `json:"status"` // "parsing" | "ready" | "failed" | "none"
	Source       string                     `json:"source,omitempty"`
	UpdatedAt    string                     `json:"updated_at,omitempty"`
	ErrorMessage *string                    `json:"error_message,omitempty"`
	Segments     []SessionTranscriptSegment `json:"segments"`
}

// SessionTranscriptSegment is one segment in the API response
type SessionTranscriptSegment struct {
	Idx     int    `json:"idx"`
	StartMs int    `json:"start_ms"`
	EndMs   int    `json:"end_ms"`
	Text    string `json:"text"`
}

// SessionTranscript handles GET /api/sessions/:id/transcript
func (h *Handlers) SessionTranscript(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	var sessionIDStr string
	if len(pathParts) >= 4 && pathParts[0] == "api" && pathParts[1] == "sessions" && pathParts[3] == "transcript" {
		sessionIDStr = pathParts[2]
	} else if len(pathParts) >= 4 && pathParts[0] == "sessions" && pathParts[3] == "transcript" {
		sessionIDStr = pathParts[1]
	} else {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(sessionIDStr)
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	t, err := h.DB.GetTranscriptBySessionID(ctx, sessionID, "")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": err.Error()})
		return
	}
	if t == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(SessionTranscriptResponse{
			Status:   "none",
			Segments: []SessionTranscriptSegment{},
		})
		return
	}
	resp := SessionTranscriptResponse{
		Status:   string(t.Status),
		Source:   t.Source,
		Segments: []SessionTranscriptSegment{},
	}
	if !t.UpdatedAt.IsZero() {
		resp.UpdatedAt = t.UpdatedAt.Format("2006-01-02T15:04:05Z07:00")
	}
	resp.ErrorMessage = t.ErrorMessage
	segments, err := h.DB.ListSegmentsByTranscriptID(ctx, t.ID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": err.Error()})
		return
	}
	for _, s := range segments {
		resp.Segments = append(resp.Segments, SessionTranscriptSegment{
			Idx:     s.Idx,
			StartMs: s.StartMs,
			EndMs:   s.EndMs,
			Text:    s.Text,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}
