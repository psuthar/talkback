package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
)

// TeamsImportRequest body for POST /api/teams/import
type TeamsImportRequest struct {
	Title       string `json:"title"`
	MeetingID   string `json:"meeting_id"`
	RecordingID string `json:"recording_id"`
}

// TeamsImportResponse for POST /api/teams/import
type TeamsImportResponse struct {
	ID    string `json:"id"`
	JobID string `json:"job_id"`
	State string `json:"state"`
}

// TeamsImport creates a session and enqueues a Teams processing job (mirrors ZoomImport).
//
// SCRUM-416: legacy create-new endpoint, deprecated in favor of the
// attach-to-existing-session POST /api/sessions/:id/import/teams path.
// Every response carries the SCRUM-416 deprecation headers and emits a
// DEPRECATED_ENDPOINT_HIT structured log. SCRUM-XX17 schedules removal.
func (h *Handlers) TeamsImport(w http.ResponseWriter, r *http.Request) {
	markLegacyImportDeprecated(w, r, "/api/sessions/:id/import/teams")
	if !teamsEnabled() {
		http.Error(w, "Teams integration disabled", http.StatusNotFound)
		return
	}
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
		json.NewEncoder(w).Encode(map[string]string{"code": "teams_not_connected", "message": "creator_identity required"})
		return
	}
	if creatorIdentity != user.Email {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"code": "forbidden", "message": "creator identity must match the authenticated user"})
		return
	}
	if _, _, err := h.GetValidTeamsAccessTokenContext(r.Context(), creatorIdentity); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"code": "teams_not_connected", "message": "Teams not connected. Connect Teams first."})
		return
	}
	var req TeamsImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"message": "Invalid request body"})
		return
	}
	meetingID := strings.TrimSpace(req.MeetingID)
	recordingID := strings.TrimSpace(req.RecordingID)
	if meetingID == "" || recordingID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"message": "meeting_id and recording_id required"})
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "Teams Recording"
	}
	exists, err := h.DB.SessionWithTitleExistsForCreator(r.Context(), creatorIdentity, title, nil)
	if err != nil {
		log.Printf("TeamsImport SessionWithTitleExistsForCreator: %v", err)
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

	sessionID := uuid.New()
	session := &models.Session{
		ID:             sessionID,
		Title:          title,
		CreatedBy:      &creatorIdentity,
		Status:         models.SessionStatusOpen,
		SourceProvider: models.SessionSourceTeams,
	}
	if err := h.DB.CreateSession(r.Context(), session); err != nil {
		log.Printf("TeamsImport create session error: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": "Failed to create session"})
		return
	}
	if _, err := h.DB.CreateArtifact(r.Context(), sessionID, title, nil); err != nil {
		log.Printf("TeamsImport create artifact error: %v", err)
	}

	jobID := uuid.New()
	mtg := meetingID
	rec := recordingID
	creatorIdentityPtr := &creatorIdentity
	job := &models.SessionProcessingJob{
		ID:              jobID,
		SessionID:       sessionID,
		Source:          "teams",
		State:           models.ProcessingStateQueued,
		Stage:           models.ProcessingStageFetch,
		MeetingUUID:     &mtg,
		InstanceUUID:    &rec,
		CreatorIdentity: creatorIdentityPtr,
	}
	if err := h.DB.CreateOrGetSessionProcessingJob(r.Context(), job); err != nil {
		log.Printf("TeamsImport create job error: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": "Failed to create import job"})
		return
	}
	_ = h.DB.UpdateSessionProcessingMirror(r.Context(), sessionID, job.State)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(TeamsImportResponse{
		ID:    sessionID.String(),
		JobID: job.ID.String(),
		State: job.State,
	})
}

// SessionImportTeams handles POST /api/sessions/:id/import/teams (import into an existing session).
func (h *Handlers) SessionImportTeams(w http.ResponseWriter, r *http.Request) {
	if !teamsEnabled() {
		http.Error(w, "Teams integration disabled", http.StatusNotFound)
		return
	}
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
		json.NewEncoder(w).Encode(map[string]string{"code": "teams_not_connected", "message": "creator_identity required"})
		return
	}
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
	// SCRUM-411 authz hardening: editor check before any state mutation.
	user := UserFromContext(r.Context())
	if user == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"message": "unauthorized"})
		return
	}
	editor, editorErr := h.userIsSessionEditor(r.Context(), sessionID, user)
	if editorErr != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": "authz check failed"})
		return
	}
	if !editor {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"message": "session editor role required"})
		return
	}
	var req TeamsImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"message": "Invalid request body"})
		return
	}
	meetingID := strings.TrimSpace(req.MeetingID)
	recordingID := strings.TrimSpace(req.RecordingID)
	if meetingID == "" || recordingID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"message": "meeting_id and recording_id required"})
		return
	}
	mtg := meetingID
	rec := recordingID
	creatorIdentityPtr := &creatorIdentity
	// SCRUM-413 dedupe + SCRUM-414 cap fire BEFORE the Teams OAuth check so
	// an idempotent re-attach works even when the user's Teams connection
	// is stale.
	if dedupe, err := dedupeExistingAttach(r.Context(), h.DB, sessionID, "teams", &mtg, &rec, creatorIdentityPtr); err != nil {
		log.Printf("SessionImportTeams dedupe lookup: %v", err)
	} else if dedupe.Existing != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(dedupe.Status)
		json.NewEncoder(w).Encode(dedupe.Response)
		return
	}
	if h.enforceRecordingCap(w, r, sessionID) {
		return
	}
	if _, _, err := h.GetValidTeamsAccessTokenContext(r.Context(), creatorIdentity); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"code": "teams_not_connected", "message": "Teams not connected. Connect Teams first."})
		return
	}
	jobID := uuid.New()
	job := &models.SessionProcessingJob{
		ID:              jobID,
		SessionID:       sessionID,
		Source:          "teams",
		State:           models.ProcessingStateQueued,
		Stage:           models.ProcessingStageFetch,
		MeetingUUID:     &mtg,
		InstanceUUID:    &rec,
		CreatorIdentity: creatorIdentityPtr,
	}
	if err := h.DB.CreateOrGetSessionProcessingJob(r.Context(), job); err != nil {
		log.Printf("SessionImportTeams create job error: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": "Failed to create import job"})
		return
	}
	_ = h.DB.UpdateSessionProcessingMirror(r.Context(), sessionID, job.State)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(SessionImportResponse{
		JobID: job.ID.String(),
		State: job.State,
	})
}
