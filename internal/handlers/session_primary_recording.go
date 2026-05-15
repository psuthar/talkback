// SCRUM-412: PATCH /api/sessions/{id}/primary-recording — multi-recording era
// canonical endpoint to reassign which video_source is the session's primary.
// Mirrors the body / outcome of the legacy POST /set-primary-video handler
// but uses userIsSessionEditor (SCRUM-227's role-based check) instead of the
// legacy email-match shortcut, and returns the standardized response shape
// used by the multi-recording attach handlers (SCRUM-411).
//
// RAG retrieval already reads the primary at query time:
// session_ask / orchestration_draft / mcpserver call
// GetVideoSourcesBySessionID + resolveEffectivePrimaryAndAdditional on every
// request. Reassignment via this endpoint is therefore observed by the very
// next RAG request — no cache flush needed.
package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
)

// PrimaryRecordingRequest is the body for PATCH /api/sessions/{id}/primary-recording.
type PrimaryRecordingRequest struct {
	VideoSourceID string `json:"video_source_id"`
}

// PrimaryRecordingResponse is the 200 body. video_role is always "primary"
// since the PATCH only flips primary on. The previous primary is implicitly
// demoted to "secondary" by the underlying SetVideoSourceVideoRole transaction.
type PrimaryRecordingResponse struct {
	VideoSourceID string `json:"video_source_id"`
	VideoRole     string `json:"video_role"`
}

// SessionPatchPrimaryRecording handles PATCH /api/sessions/{id}/primary-recording.
//
// Concurrent-write race (two creators in different tabs both PATCHing
// different recordings simultaneously) is intentionally last-write-wins for
// v1 — see the SCRUM-412 ticket note + the Hardening Epic backlog for the
// idempotency-token / If-Match hardening follow-up.
func (h *Handlers) SessionPatchPrimaryRecording(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]string{"message": "method not allowed"})
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 4 || pathParts[0] != "api" || pathParts[1] != "sessions" || pathParts[3] != "primary-recording" {
		writeJSONStatus(w, http.StatusNotFound, map[string]string{"message": "invalid path"})
		return
	}
	sessionID, err := uuid.Parse(pathParts[2])
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"message": "invalid session id"})
		return
	}
	session, err := h.DB.GetSession(r.Context(), sessionID)
	if err != nil || session == nil {
		writeJSONStatus(w, http.StatusNotFound, map[string]string{"message": "session not found"})
		return
	}
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
	var req PrimaryRecordingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"message": "invalid request body"})
		return
	}
	videoSourceID, err := uuid.Parse(strings.TrimSpace(req.VideoSourceID))
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"message": "invalid video_source_id"})
		return
	}
	// 400 if the recording doesn't belong to this session — prevents a creator
	// of session A from flipping primary on a recording owned by session B.
	vs, err := h.DB.GetVideoSourceByID(r.Context(), videoSourceID)
	if err != nil || vs == nil || vs.SessionID != sessionID {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"message": "video_source_id does not belong to this session"})
		return
	}
	if err := h.DB.SetVideoSourceVideoRole(r.Context(), sessionID, videoSourceID, models.VideoRolePrimary); err != nil {
		log.Printf("SessionPatchPrimaryRecording: %v", err)
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"message": "failed to set primary recording"})
		return
	}
	writeJSONStatus(w, http.StatusOK, PrimaryRecordingResponse{
		VideoSourceID: videoSourceID.String(),
		VideoRole:     string(models.VideoRolePrimary),
	})
}
