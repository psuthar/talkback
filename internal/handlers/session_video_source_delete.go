// SCRUM-426: single video_source DELETE endpoint for the Recordings
// section's "Remove" kebab action. The schema cascade chain
// (video_sources → transcripts → transcript_segments,
// session_speaker_aliases.source_recording_id ON DELETE CASCADE)
// handles the per-row cleanup; this handler is the API surface that
// owns the authz gate + cross-session check.
package handlers

import (
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// DeleteSessionVideoSource handles DELETE /api/sessions/{id}/video-sources/{video_source_id}.
//
// Authz: userIsSessionEditor (the SCRUM-227 role check). Cross-session
// guard: 400 if the video_source doesn't belong to this session.
func (h *Handlers) DeleteSessionVideoSource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]string{"message": "method not allowed"})
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	// /api/sessions/:id/video-sources/:vsid -> 5 parts
	if len(pathParts) != 5 || pathParts[0] != "api" || pathParts[1] != "sessions" || pathParts[3] != "video-sources" {
		writeJSONStatus(w, http.StatusNotFound, map[string]string{"message": "invalid path"})
		return
	}
	sessionID, err := uuid.Parse(pathParts[2])
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"message": "invalid session id"})
		return
	}
	videoSourceID, err := uuid.Parse(pathParts[4])
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"message": "invalid video_source_id"})
		return
	}
	if _, err := h.DB.GetSession(r.Context(), sessionID); err != nil {
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
	vs, err := h.DB.GetVideoSourceByID(r.Context(), videoSourceID)
	if err != nil || vs == nil {
		// Idempotent: already-gone is a 204 from the caller's perspective.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if vs.SessionID != sessionID {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"message": "video_source_id does not belong to this session"})
		return
	}
	if err := h.DB.DeleteVideoSourceByID(r.Context(), videoSourceID); err != nil {
		log.Printf("DeleteSessionVideoSource: %v", err)
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"message": "failed to delete recording"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
