// SCRUM-424: People panel API. Surfaces the SCRUM-404 speaker-alias DB
// layer through three endpoints scoped to a session:
//
//   GET    /api/sessions/{id}/people          — distinct speaker_labels +
//                                                existing aliases
//   POST   /api/sessions/{id}/people/aliases  — upsert (merge speakers)
//   DELETE /api/sessions/{id}/people/aliases/{aliasID}  — unmap (split)
//
// Authz: every endpoint requires userIsSessionEditor before any mutation.
package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
)

// SessionPeopleResponse is the body for GET /api/sessions/{id}/people.
//
// Labels is every distinct speaker_label observed in
// transcript_segments for the session, alongside its segment count.
// Aliases is every session_speaker_aliases row for the session.
//
// The frontend joins the two by source_label to render the People panel:
// labels with no matching alias render as "Person N" placeholders; labels
// that share a canonical_person_id render as one merged person.
type SessionPeopleResponse struct {
	Labels  []models.SpeakerLabelObservation `json:"labels"`
	Aliases []models.SessionSpeakerAlias     `json:"aliases"`
}

// UpsertAliasRequest is the body for POST /api/sessions/{id}/people/aliases.
//
// canonical_person_id is optional: when omitted the handler generates a new
// UUID (creating a fresh canonical person). When provided, the handler maps
// source_label to that existing canonical (merging this label into the
// existing group).
type UpsertAliasRequest struct {
	SourceLabel          string     `json:"source_label"`
	SourceRecordingID    *uuid.UUID `json:"source_recording_id,omitempty"`
	CanonicalPersonID    *uuid.UUID `json:"canonical_person_id,omitempty"`
	CanonicalDisplayName string     `json:"canonical_display_name"`
	CanonicalEmail       *string    `json:"canonical_email,omitempty"`
}

// SessionPeopleRouter dispatches the three SCRUM-424 sub-routes off the
// shared /api/sessions/{id}/people prefix.
func (h *Handlers) SessionPeopleRouter(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	// /api/sessions/:id/people                 -> 4 parts
	// /api/sessions/:id/people/aliases         -> 5 parts
	// /api/sessions/:id/people/aliases/:alias  -> 6 parts
	if len(parts) < 4 || parts[0] != "api" || parts[1] != "sessions" || parts[3] != "people" {
		writeJSONStatus(w, http.StatusNotFound, map[string]string{"message": "invalid path"})
		return
	}
	sessionID, err := uuid.Parse(parts[2])
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"message": "invalid session id"})
		return
	}

	switch {
	case len(parts) == 4 && r.Method == http.MethodGet:
		h.sessionPeopleList(w, r, sessionID)
	case len(parts) == 5 && parts[4] == "aliases" && r.Method == http.MethodPost:
		h.sessionPeopleUpsert(w, r, sessionID)
	case len(parts) == 6 && parts[4] == "aliases" && r.Method == http.MethodDelete:
		aliasID, parseErr := uuid.Parse(parts[5])
		if parseErr != nil {
			writeJSONStatus(w, http.StatusBadRequest, map[string]string{"message": "invalid alias id"})
			return
		}
		h.sessionPeopleDelete(w, r, sessionID, aliasID)
	default:
		writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]string{"message": "method not allowed"})
	}
}

// sessionPeopleList returns distinct speaker labels + existing aliases.
// Read-only; any session member (not just editor) may call.
func (h *Handlers) sessionPeopleList(w http.ResponseWriter, r *http.Request, sessionID uuid.UUID) {
	if _, err := h.DB.GetSession(r.Context(), sessionID); err != nil {
		writeJSONStatus(w, http.StatusNotFound, map[string]string{"message": "session not found"})
		return
	}
	labels, err := h.DB.ListDistinctSpeakerLabels(r.Context(), sessionID)
	if err != nil {
		log.Printf("sessionPeopleList ListDistinctSpeakerLabels: %v", err)
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"message": "failed to list speaker labels"})
		return
	}
	aliases, err := h.DB.ListAliasesBySession(r.Context(), sessionID)
	if err != nil {
		log.Printf("sessionPeopleList ListAliasesBySession: %v", err)
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"message": "failed to list aliases"})
		return
	}
	writeJSONStatus(w, http.StatusOK, SessionPeopleResponse{Labels: labels, Aliases: aliases})
}

// sessionPeopleUpsert is the merge / rename endpoint. Mutation; requires
// editor role.
func (h *Handlers) sessionPeopleUpsert(w http.ResponseWriter, r *http.Request, sessionID uuid.UUID) {
	if _, err := h.DB.GetSession(r.Context(), sessionID); err != nil {
		writeJSONStatus(w, http.StatusNotFound, map[string]string{"message": "session not found"})
		return
	}
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSONStatus(w, http.StatusUnauthorized, map[string]string{"message": "unauthorized"})
		return
	}
	editor, err := h.userIsSessionEditor(r.Context(), sessionID, user)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"message": "authz check failed"})
		return
	}
	if !editor {
		writeJSONStatus(w, http.StatusForbidden, map[string]string{"message": "session editor role required"})
		return
	}

	var req UpsertAliasRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"message": "invalid request body"})
		return
	}
	if strings.TrimSpace(req.SourceLabel) == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"message": "source_label required"})
		return
	}
	if strings.TrimSpace(req.CanonicalDisplayName) == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"message": "canonical_display_name required"})
		return
	}
	canonical := req.CanonicalPersonID
	if canonical == nil {
		nid := uuid.New()
		canonical = &nid
	}

	if err := h.DB.UpsertAlias(
		r.Context(),
		sessionID,
		strings.TrimSpace(req.SourceLabel),
		req.SourceRecordingID,
		*canonical,
		strings.TrimSpace(req.CanonicalDisplayName),
		req.CanonicalEmail,
	); err != nil {
		log.Printf("sessionPeopleUpsert UpsertAlias: %v", err)
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"message": "failed to upsert alias"})
		return
	}
	writeJSONStatus(w, http.StatusOK, map[string]string{"canonical_person_id": canonical.String()})
}

// sessionPeopleDelete unmaps (splits) a single alias row.
func (h *Handlers) sessionPeopleDelete(w http.ResponseWriter, r *http.Request, sessionID, aliasID uuid.UUID) {
	if _, err := h.DB.GetSession(r.Context(), sessionID); err != nil {
		writeJSONStatus(w, http.StatusNotFound, map[string]string{"message": "session not found"})
		return
	}
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSONStatus(w, http.StatusUnauthorized, map[string]string{"message": "unauthorized"})
		return
	}
	editor, err := h.userIsSessionEditor(r.Context(), sessionID, user)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"message": "authz check failed"})
		return
	}
	if !editor {
		writeJSONStatus(w, http.StatusForbidden, map[string]string{"message": "session editor role required"})
		return
	}
	// Verify the alias belongs to this session so a creator of session A
	// can't delete session B's alias rows.
	aliases, err := h.DB.ListAliasesBySession(r.Context(), sessionID)
	if err != nil {
		log.Printf("sessionPeopleDelete ListAliasesBySession: %v", err)
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"message": "lookup failed"})
		return
	}
	found := false
	for _, a := range aliases {
		if a.ID == aliasID {
			found = true
			break
		}
	}
	if !found {
		// Idempotent semantics: 200 either way (DeleteAlias is itself
		// idempotent), but return a hint when the alias clearly doesn't
		// belong to this session so the frontend doesn't think the call
		// succeeded against a different session's row.
		writeJSONStatus(w, http.StatusNotFound, map[string]string{"message": "alias not found in this session"})
		return
	}
	if err := h.DB.DeleteAlias(r.Context(), aliasID); err != nil {
		log.Printf("sessionPeopleDelete DeleteAlias: %v", err)
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"message": "failed to delete alias"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
