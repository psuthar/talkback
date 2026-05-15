package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
)

// GoogleMeetImportRequest body for POST /api/google-meet/import.
// conference_record and recording are full Meet resource names
// (conferenceRecords/{c} and conferenceRecords/{c}/recordings/{r}).
type GoogleMeetImportRequest struct {
	Title            string `json:"title"`
	ConferenceRecord string `json:"conference_record"`
	Recording        string `json:"recording"`
}

// GoogleMeetImportResponse for POST /api/google-meet/import.
type GoogleMeetImportResponse struct {
	ID    string `json:"id"`
	JobID string `json:"job_id"`
	State string `json:"state"`
}

// GoogleMeetImport creates a session and enqueues a Google Meet processing job
// (mirrors TeamsImport). Routes: POST /api/google-meet/import.
func (h *Handlers) GoogleMeetImport(w http.ResponseWriter, r *http.Request) {
	if !googleMeetEnabled() {
		http.Error(w, "Google Meet integration disabled", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPost {
		writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]string{"message": "Method not allowed"})
		return
	}
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSONStatus(w, http.StatusUnauthorized, map[string]string{"code": "unauthorized", "message": "unauthorized"})
		return
	}
	if !user.GlobalRole.CanCreateSessions() {
		writeJSONStatus(w, http.StatusForbidden, map[string]string{"code": "forbidden", "message": "your role does not allow creating sessions"})
		return
	}
	creatorIdentity := readCreatorIdentity(r)
	if creatorIdentity == "" {
		writeJSONStatus(w, http.StatusUnauthorized, map[string]string{"code": "google_meet_not_connected", "message": "creator_identity required"})
		return
	}
	if creatorIdentity != user.Email {
		writeJSONStatus(w, http.StatusForbidden, map[string]string{"code": "forbidden", "message": "creator identity must match the authenticated user"})
		return
	}
	if _, _, err := h.GetValidGoogleMeetAccessTokenContext(r.Context(), creatorIdentity); err != nil {
		writeJSONStatus(w, http.StatusUnauthorized, map[string]string{"code": "google_meet_not_connected", "message": "Google Meet not connected. Connect Google Meet first."})
		return
	}
	var req GoogleMeetImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"message": "Invalid request body"})
		return
	}
	conferenceRecord := strings.TrimSpace(req.ConferenceRecord)
	recording := strings.TrimSpace(req.Recording)
	if conferenceRecord == "" || recording == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"message": "conference_record and recording required"})
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "Google Meet Recording"
	}
	exists, err := h.DB.SessionWithTitleExistsForCreator(r.Context(), creatorIdentity, title, nil)
	if err != nil {
		log.Printf("GoogleMeetImport SessionWithTitleExistsForCreator: %v", err)
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"message": "Failed to check session title"})
		return
	}
	if exists {
		writeJSONStatus(w, http.StatusConflict, map[string]string{"message": "A session with this name already exists. Please use a unique name."})
		return
	}

	sessionID := uuid.New()
	session := &models.Session{
		ID:             sessionID,
		Title:          title,
		CreatedBy:      &creatorIdentity,
		Status:         models.SessionStatusOpen,
		SourceProvider: models.SessionSourceGoogleMeet,
	}
	if err := h.DB.CreateSession(r.Context(), session); err != nil {
		log.Printf("GoogleMeetImport create session: %v", err)
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"message": "Failed to create session"})
		return
	}
	if _, err := h.DB.CreateArtifact(r.Context(), sessionID, title, nil); err != nil {
		log.Printf("GoogleMeetImport create artifact: %v", err)
	}

	jobID := uuid.New()
	cr := conferenceRecord
	rec := recording
	creatorPtr := &creatorIdentity
	job := &models.SessionProcessingJob{
		ID:              jobID,
		SessionID:       sessionID,
		Source:          models.SessionProcessingJobSourceGoogleMeet,
		State:           models.ProcessingStateQueued,
		Stage:           models.ProcessingStageFetch,
		MeetingUUID:     &cr,
		InstanceUUID:    &rec,
		CreatorIdentity: creatorPtr,
	}
	if err := h.DB.CreateOrGetSessionProcessingJob(r.Context(), job); err != nil {
		log.Printf("GoogleMeetImport create job: %v", err)
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"message": "Failed to create import job"})
		return
	}
	_ = h.DB.UpdateSessionProcessingMirror(r.Context(), sessionID, job.State)

	writeJSONStatus(w, http.StatusAccepted, GoogleMeetImportResponse{
		ID:    sessionID.String(),
		JobID: job.ID.String(),
		State: job.State,
	})
}

// SessionImportGoogleMeet enqueues a Google Meet job for an existing session.
// Routes: POST /api/sessions/:id/import/google-meet (dispatched from APISessionsRouter).
func (h *Handlers) SessionImportGoogleMeet(w http.ResponseWriter, r *http.Request) {
	if !googleMeetEnabled() {
		http.Error(w, "Google Meet integration disabled", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	creatorIdentity := readCreatorIdentity(r)
	if creatorIdentity == "" {
		writeJSONStatus(w, http.StatusUnauthorized, map[string]string{"code": "google_meet_not_connected", "message": "creator_identity required"})
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	var sessionIDStr string
	if len(pathParts) >= 5 && pathParts[0] == "api" && pathParts[1] == "sessions" && pathParts[3] == "import" {
		sessionIDStr = pathParts[2]
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
		writeJSONStatus(w, http.StatusNotFound, map[string]string{"message": "Session not found"})
		return
	}
	// SCRUM-411 authz hardening: editor check before any state mutation.
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSONStatus(w, http.StatusUnauthorized, map[string]string{"message": "unauthorized"})
		return
	}
	editor, editorErr := h.userIsSessionEditor(r.Context(), sessionID, user)
	if editorErr != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"message": "authz check failed"})
		return
	}
	if !editor {
		writeJSONStatus(w, http.StatusForbidden, map[string]string{"message": "session editor role required"})
		return
	}
	var req GoogleMeetImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"message": "Invalid request body"})
		return
	}
	conferenceRecord := strings.TrimSpace(req.ConferenceRecord)
	recording := strings.TrimSpace(req.Recording)
	if conferenceRecord == "" || recording == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"message": "conference_record and recording required"})
		return
	}
	cr := conferenceRecord
	rec := recording
	creatorPtr := &creatorIdentity
	// SCRUM-413 dedupe + SCRUM-414 cap fire BEFORE the Google Meet OAuth
	// check so an idempotent re-attach works even when the user's Google
	// connection is stale.
	if dedupe, err := dedupeExistingAttach(r.Context(), h.DB, sessionID, models.SessionProcessingJobSourceGoogleMeet, &cr, &rec, creatorPtr); err != nil {
		log.Printf("SessionImportGoogleMeet dedupe lookup: %v", err)
	} else if dedupe.Existing != nil {
		writeJSONStatus(w, dedupe.Status, dedupe.Response)
		return
	}
	if h.enforceRecordingCap(w, r, sessionID) {
		return
	}
	if _, _, err := h.GetValidGoogleMeetAccessTokenContext(r.Context(), creatorIdentity); err != nil {
		writeJSONStatus(w, http.StatusUnauthorized, map[string]string{"code": "google_meet_not_connected", "message": "Google Meet not connected. Connect Google Meet first."})
		return
	}
	jobID := uuid.New()
	job := &models.SessionProcessingJob{
		ID:              jobID,
		SessionID:       sessionID,
		Source:          models.SessionProcessingJobSourceGoogleMeet,
		State:           models.ProcessingStateQueued,
		Stage:           models.ProcessingStageFetch,
		MeetingUUID:     &cr,
		InstanceUUID:    &rec,
		CreatorIdentity: creatorPtr,
	}
	if err := h.DB.CreateOrGetSessionProcessingJob(r.Context(), job); err != nil {
		log.Printf("SessionImportGoogleMeet create job: %v", err)
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"message": "Failed to create import job"})
		return
	}
	_ = h.DB.UpdateSessionProcessingMirror(r.Context(), sessionID, job.State)
	writeJSONStatus(w, http.StatusAccepted, SessionImportResponse{
		JobID: job.ID.String(),
		State: job.State,
	})
}
