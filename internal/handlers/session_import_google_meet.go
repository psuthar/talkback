package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
)

// GoogleMeetImportRequest is the JSON body for POST
// /api/sessions/:id/import/google-meet. conference_record and recording
// are full Meet resource names (conferenceRecords/{c} and
// conferenceRecords/{c}/recordings/{r}).
type GoogleMeetImportRequest struct {
	Title            string `json:"title"`
	ConferenceRecord string `json:"conference_record"`
	Recording        string `json:"recording"`
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
	// SCRUM-417 cross-tenant safety.
	if !strings.EqualFold(strings.TrimSpace(creatorIdentity), strings.TrimSpace(user.Email)) {
		writeJSONStatus(w, http.StatusForbidden, map[string]string{"message": "creator_identity must match authenticated user"})
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
		// SCRUM-471: post-creation attach — never auto-promote to primary.
		SetAsPrimary: false,
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
