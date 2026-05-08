package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/citation"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/sessionimport"
	"github.com/psuthar/talkback/internal/storage"
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
	Session              *models.Session       `json:"session"`
	Artifacts            []*models.Artifact    `json:"artifacts"`
	Materials            []*models.Material    `json:"materials"`
	VideoSources         []*models.VideoSource `json:"video_sources"`
	PrimaryVideo         *models.VideoSource   `json:"primary_video,omitempty"`     // effective primary (explicit primary, else first ready, else first)
	AdditionalVideos     []*models.VideoSource `json:"additional_videos,omitempty"` // all other session videos
	RecentQuestions      []*models.Question    `json:"recent_questions"`
	RecentAnswers        []*models.Answer      `json:"recent_answers"`
	Mode                 string                `json:"mode"`                              // "creator" or "participant"
	CreatedByDisplayName *string               `json:"created_by_display_name,omitempty"` // session creator display name for UI
	UnreadMaterialIDs    []string              `json:"unread_material_ids,omitempty"`     // only when participant_ref provided
	VideoAccessURL       string                `json:"video_access_url,omitempty"`        // presigned R2 URL only when primary artifact is ready, r2, video
	PlaybackReasonCode   string                `json:"playback_reason_code,omitempty"`    // VIDEO_NOT_INGESTED, VIDEO_INGEST_PENDING, VIDEO_INGEST_FAILED
	PlaybackMessage      string                `json:"playback_message,omitempty"`        // safe message when video not playable
	Links                []*models.SessionLink `json:"links,omitempty"`                   // session links for citation URL resolution
	MaterialSlidesReady  map[string]bool       `json:"material_slides_ready,omitempty"`   // material ID -> true when slides manifest exists (PPT/PPTX only)
	MaterialSlidesStatus map[string]string     `json:"material_slides_status,omitempty"`  // material ID -> processing|ready|failed (PPT/PPTX only)
	Primary              *SessionPrimaryDescriptor `json:"primary,omitempty"`             // SCRUM-271: resolved center-pane primary (kind + id; legacy primary_video_artifact_id falls back to kind="video")
}

// SessionWithRole is one session plus the current user's role for it (for GET /api/sessions).
type SessionWithRole = database.SessionListRow

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
	out, err := h.DB.ListSessionsWithRolesForUser(ctx, user)
	if err != nil {
		log.Printf("ListSessions: %v", err)
		http.Error(w, "Failed to list sessions", http.StatusInternalServerError)
		return
	}

	// Optional limit/offset pagination. Both default to "no limit / no offset" when absent or zero.
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 {
			offset := 0
			if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
				if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
					offset = o
				}
			}
			out = database.ApplySessionListPagination(out, limit, offset)
		}
	}

	// SCRUM-280: resolve and enrich the primary descriptor for each row in the
	// page using batched fetches (one per kind that appears in the page).
	// Existing raw primary_* columns on Session stay; the new `primary` block
	// is additive so this is wire-shape backward compatible.
	sessions := make([]*models.Session, len(out))
	for i := range out {
		sessions[i] = out[i].Session
	}
	primaries := h.resolveAndEnrichSessionPrimaryForList(ctx, sessions)
	enriched := make([]sessionListRowWithPrimary, len(out))
	for i, row := range out {
		enriched[i] = sessionListRowWithPrimary{SessionListRow: row, Primary: primaries[i]}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(enriched)
}

// sessionListRowWithPrimary embeds the existing list row and adds the
// resolved primary descriptor. JSON encodes flat: every prior field stays at
// the top level (embedding), with the new `primary` block appended when
// present.
type sessionListRowWithPrimary struct {
	database.SessionListRow
	Primary *SessionPrimaryDescriptor `json:"primary,omitempty"`
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

	title := strings.TrimSpace(req.Title)
	if title == "" {
		http.Error(w, "title is required", http.StatusBadRequest)
		return
	}

	exists, err := h.DB.SessionWithTitleExistsForCreator(r.Context(), user.Email, title, nil)
	if err != nil {
		log.Printf("CreateSession SessionWithTitleExistsForCreator: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to check session title"})
		return
	}
	if exists {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "A session with this name already exists. Please use a unique name."})
		return
	}

	createdBy := user.Email
	// Create session (creator is the authenticated user)
	session := &models.Session{
		ID:        uuid.New(),
		Title:     title,
		CreatedBy: &createdBy,
		Status:    models.SessionStatusOpen,
	}

	if err := h.DB.CreateSession(r.Context(), session); err != nil {
		log.Printf("Error creating session: %v", err)
		http.Error(w, fmt.Sprintf("Failed to create session: %v", err), http.StatusInternalServerError)
		return
	}

	// Create a first artifact so the session is immediately usable (edit view, Q&A, and "session has no artifacts" is avoided)
	artifactTitle := session.Title
	if artifactTitle == "" {
		artifactTitle = "Session content"
	}
	if _, err := h.DB.CreateArtifact(r.Context(), session.ID, artifactTitle, nil); err != nil {
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

// CopySessionRequest is the optional body for POST /api/sessions/:id/copy
type CopySessionRequest struct {
	Title *string `json:"title,omitempty"`
}

// CopySessionResponse is the response for POST /api/sessions/:id/copy.
// partial_failures (SCRUM-344) lists categories that had at least one
// non-fatal failure during copy (e.g. one material's R2 Get failed but the
// rest of the copy succeeded). The field is additive and omitted when empty,
// so existing API consumers are unaffected.
type CopySessionResponse struct {
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	CreatedBy       *string  `json:"created_by,omitempty"`
	Status          string   `json:"status"`
	CreatedAt       string   `json:"created_at"`
	PartialFailures []string `json:"partial_failures,omitempty"`
}

// CopySession creates a new session with the same content as the source
// (Creator or Admin only). Everything in the new session is a standalone copy:
// new IDs, new storage keys, and new file paths; nothing references the source
// session's storage or records.
//
// The handler is a thin orchestrator: it loads source-side data slices from
// the DB, builds an import context, and threads them through the primitives in
// internal/sessionimport. That package is intentionally session-source-agnostic
// so a future Session Templates feature (SCRUM-347 spike) can reuse it.
//
// COPY (per SCRUM-338 plan):
//   - artifacts
//   - materials (+ slide manifest / PNGs)
//   - session_links (+ extracted_text)
//   - video_sources (+ stored MP4 + on-row transcript)
//   - all session-scoped file_artifacts (not just the primary video)
//   - transcripts + transcript_segments
//   - session metadata: premise, primary_decision, decision_outcome,
//     source_reference_url, primary_content_kind + primary_*_id (remapped
//     through the per-category remap maps)
//   - session_processing_jobs (re-enqueued only when no primary video file
//     could be reproduced and the source had a cloud-import job)
//
// SKIP (intentionally — see SCRUM-338 epic non-goals):
//   - members: session_memberships, session_invitations
//   - user/audience-scoped data: questions, answers, decision_stances,
//     material_views, question_views, session_participants, session_events
//   - LLM outputs the user has acted on: orchestration_recommendations
//     (+ status audit), sessions_primary_history
//   - ephemeral processing state: transcript_jobs, ingestion_jobs
//
// RECOMPUTE: session_chunks + session_chunk_embeddings via triggerIndex
// (cheap async embedding pass; see plan §2 for the recompute trade-off).
//
// Failure semantics (SCRUM-344):
//   - Per-row child copies (materials/links/video_sources/file_artifacts/
//     slide assets) are best-effort: per-row failures are logged and the
//     affected category is recorded in CopySessionResponse.partial_failures
//     so callers can surface a "your clone is missing 2 of 5 slide decks"
//     warning. The HTTP response is 201.
//   - Critical-step failures (ImportSessionRow, ImportTranscripts,
//     ImportSessionMetadata) trigger a session-row delete (children cascade)
//     plus best-effort R2 prefix delete, and return HTTP 500 — a clone
//     missing its framing or transcript is misleading enough not to expose.
func (h *Handlers) CopySession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
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
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 4 || parts[0] != "api" || parts[1] != "sessions" || parts[3] != "copy" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}
	sourceSessionID, err := uuid.Parse(parts[2])
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid session id"})
		return
	}
	ctx := r.Context()
	sourceSession, err := h.DB.GetSession(ctx, sourceSessionID)
	if err != nil || sourceSession == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	// Only source creator or admin can copy
	if user.GlobalRole != models.GlobalRoleAdmin {
		if sourceSession.CreatedBy == nil || *sourceSession.CreatedBy != user.Email {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "only the session creator or an admin can copy this session"})
			return
		}
	}

	var copyReq CopySessionRequest
	_ = json.NewDecoder(r.Body).Decode(&copyReq) // optional body; ignore decode error for empty body

	createdBy := user.Email
	title := "Copy of " + sourceSession.Title
	if copyReq.Title != nil && strings.TrimSpace(*copyReq.Title) != "" {
		title = strings.TrimSpace(*copyReq.Title)
		exists, err := h.DB.SessionWithTitleExistsForCreator(ctx, createdBy, title, nil)
		if err != nil {
			log.Printf("CopySession SessionWithTitleExistsForCreator: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to check session title"})
			return
		}
		if exists {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "A session with this name already exists. Please use a unique name."})
			return
		}
	} else {
		// Default title: ensure uniqueness by appending (2), (3), ... as needed
		if len(title) > 512 {
			title = title[:512]
		}
		for n := 1; ; n++ {
			if n > 1 {
				suffix := fmt.Sprintf(" (%d)", n)
				title = "Copy of " + sourceSession.Title + suffix
				if len(title) > 512 {
					title = title[:512]
				}
			}
			exists, err := h.DB.SessionWithTitleExistsForCreator(ctx, createdBy, title, nil)
			if err != nil {
				log.Printf("CopySession SessionWithTitleExistsForCreator: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to check session title"})
				return
			}
			if !exists {
				break
			}
		}
	}

	newSession := &models.Session{
		ID:                 uuid.New(),
		Title:              title,
		CreatedBy:          &createdBy,
		Status:             models.SessionStatusOpen,
		SourceReferenceURL: sourceSession.SourceReferenceURL, // SCRUM-340: carry framing from source; CreateSession persists at INSERT time
	}
	// Load source-side data slices. Errors loading artifacts are fatal (the
	// import primitives need them); other top-level reads are best-effort.
	artifacts, err := h.DB.GetArtifactsBySessionID(ctx, sourceSessionID)
	if err != nil {
		log.Printf("CopySession GetArtifactsBySessionID: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list source artifacts"})
		return
	}
	materials, err := h.DB.GetActiveMaterialsBySessionID(ctx, sourceSessionID)
	if err != nil {
		log.Printf("CopySession GetActiveMaterialsBySessionID: %v", err)
		materials = nil
	}
	sourceLinks, _ := h.DB.GetSessionLinksBySessionID(ctx, sourceSessionID)
	sourceVideoSources, _ := h.DB.GetVideoSourcesBySessionID(ctx, sourceSessionID)
	// SCRUM-343: load all session-scoped file_artifacts (was: only the primary).
	allFAs, _ := h.DB.ListFileArtifactsBySessionID(ctx, sourceSessionID)
	jobSource := string(sourceSession.SourceProvider)
	if jobSource == "" {
		jobSource = models.SessionProcessingJobSourceZoom
	}
	sourceJob, _ := h.DB.GetSessionProcessingJobBySessionID(ctx, sourceSessionID, jobSource)

	deps := sessionimport.Deps{
		DB:           h.DB,
		Storage:      h.Storage,
		R2Prefix:     strings.TrimSuffix(strings.TrimSpace(os.Getenv("R2_PREFIX")), "/"),
		TriggerIndex: h.triggerIndex,
	}
	c := sessionimport.NewCtx(deps, newSession)

	if err := sessionimport.ImportSessionRow(ctx, c); err != nil {
		log.Printf("CopySession ImportSessionRow: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create session"})
		return
	}
	// SCRUM-344: critical-step rollback. Per-row child failures
	// (materials/links/video_sources/file_artifacts) accumulate in
	// c.PartialFailures and surface in the response. Failures of the
	// session-level steps (transcripts, metadata) are critical: they leave
	// the clone in an inconsistent state, so we delete the new session row
	// (children cascade) and return 500.
	rollback := func(reason string, err error) {
		log.Printf("CopySession critical failure (%s): %v; rolling back session %s", reason, err, newSession.ID)
		if delErr := h.DB.DeleteSession(ctx, newSession.ID); delErr != nil {
			log.Printf("CopySession rollback DeleteSession: %v", delErr)
		}
		if h.Storage != nil {
			if _, dpErr := h.Storage.DeletePrefix(ctx, "sessions/"+newSession.ID.String()+"/"); dpErr != nil {
				log.Printf("CopySession rollback DeletePrefix: %v", dpErr)
			}
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to copy session: " + reason})
	}
	_ = sessionimport.ImportArtifacts(ctx, c, artifacts)
	_ = sessionimport.ImportFileArtifacts(ctx, c, allFAs, sourceSession.PrimaryVideoArtifactID)
	_ = sessionimport.ImportMaterials(ctx, c, materials, sourceSessionID.String())
	_ = sessionimport.ImportSessionLinks(ctx, c, sourceLinks)
	_ = sessionimport.ImportVideoSources(ctx, c, sourceVideoSources)
	sourceTranscripts, _ := h.DB.ListTranscriptsBySessionID(ctx, sourceSessionID)
	if err := sessionimport.ImportTranscripts(ctx, c, sourceTranscripts); err != nil {
		rollback("transcripts", err)
		return
	}
	if err := sessionimport.ImportSessionMetadata(ctx, c, sourceSession); err != nil {
		rollback("session_metadata", err)
		return
	}
	sessionimport.MaybeEnqueueProcessingJob(ctx, c, sourceJob, sourceSession)
	h.triggerIndex(newSession.ID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(CopySessionResponse{
		ID:              newSession.ID.String(),
		Title:           newSession.Title,
		CreatedBy:       newSession.CreatedBy,
		Status:          string(newSession.Status),
		CreatedAt:       newSession.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		PartialFailures: c.PartialFailures,
	})
}

// DeleteSession removes a session and all its DB/file data. Admin-only.
func (h *Handlers) DeleteSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if user.GlobalRole != models.GlobalRoleAdmin {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only an admin can delete sessions"})
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 3 || pathParts[0] != "api" || pathParts[1] != "sessions" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}
	sessionID, err := uuid.Parse(pathParts[2])
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid session id"})
		return
	}
	ctx := r.Context()
	session, err := h.DB.GetSession(ctx, sessionID)
	if err != nil || session == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	// 1. R2: delete all objects under sessions/{sessionID}/
	if h.Storage != nil {
		r2Prefix := strings.TrimSuffix(strings.TrimSpace(os.Getenv("R2_PREFIX")), "/")
		prefix := "sessions/" + sessionID.String() + "/"
		if r2Prefix != "" {
			prefix = r2Prefix + "/" + prefix
		}
		if _, delErr := h.Storage.DeletePrefix(ctx, prefix); delErr != nil {
			log.Printf("DeleteSession R2 DeletePrefix %s: %v", prefix, delErr)
		}
	}
	// 2. Local: remove session directory (uploads, videos, transcripts)
	localDir := filepath.Join(storage.UploadRoot(), storage.SessionStorageRoot, sessionID.String())
	if err := os.RemoveAll(localDir); err != nil {
		log.Printf("DeleteSession RemoveAll %s: %v", localDir, err)
	}
	// 3. DB: delete questions first (avoids NOT NULL on questions.session_id if FK is ON DELETE SET NULL), then file_artifacts, then session
	if err := h.DB.DeleteQuestionsBySessionID(ctx, sessionID); err != nil {
		log.Printf("DeleteSession DeleteQuestionsBySessionID: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete session data"})
		return
	}
	if err := h.DB.DeleteFileArtifactsBySessionID(ctx, sessionID); err != nil {
		log.Printf("DeleteSession DeleteFileArtifactsBySessionID: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete session data"})
		return
	}
	if err := h.DB.DeleteSession(ctx, sessionID); err != nil {
		log.Printf("DeleteSession DeleteSession: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete session"})
		return
	}
	if h.Hub != nil {
		h.Hub.BroadcastSessionDeleted(sessionID)
	}
	w.WriteHeader(http.StatusNoContent)
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

	links, _ := h.DB.GetSessionLinksBySessionID(r.Context(), sessionID)
	if links == nil {
		links = []*models.SessionLink{}
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
	// Enrich citations with navigation (url for link citations, video seek, doc page) so frontend can open links/sections
	chunkURLByChunkID := make(citation.ChunkURLByChunkID)
	for _, ch := range chunks {
		if ch.SourceType == "link" && ch.AnchorJSON != nil {
			if u, ok := ch.AnchorJSON["url"].(string); ok && u != "" {
				chunkURLByChunkID[ch.ID.String()] = u
			}
		}
	}
	for _, a := range answers {
		if a == nil {
			continue
		}
		for i := range a.Citations {
			c := &a.Citations[i]
			t := citation.ResolveCitationTarget(*c, allVideoSources, allMaterials, links, chunkURLByChunkID)
			c.Navigation = &models.CitationNavigation{
				Type:     t.Type,
				URL:      t.URL,
				Fragment: t.Fragment,
				SeekMs:   t.SeekMs,
				Page:     t.Page,
				Block:    t.Block,
			}
		}
	}
	h.enrichAnswersWithDisplayNames(r.Context(), answers)

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
	var primaryVideoArtifact *models.FileArtifact // captured for SessionPrimaryDescriptor enrichment below
	if session.PrimaryVideoArtifactID != nil {
		fa, err := h.DB.GetFileArtifactByID(r.Context(), *session.PrimaryVideoArtifactID)
		primaryVideoArtifact = fa
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
	var createdByDisplayName *string
	if session.CreatedBy != nil && *session.CreatedBy != "" {
		if creator, err := h.DB.GetUserByEmail(r.Context(), *session.CreatedBy); err == nil && creator != nil {
			createdByDisplayName = &creator.DisplayName
		}
	}
	// For PPT/PPTX materials, indicate whether derived slides manifest exists and explicit preview status.
	materialSlidesReady := make(map[string]bool)
	materialSlidesStatus := make(map[string]string)
	for _, m := range allMaterials {
		if m != nil && models.MaterialSupportsDerivedSlideDeck(m) {
			status := h.GetSlidesStatus(r.Context(), m)
			materialSlidesStatus[m.ID.String()] = status
			materialSlidesReady[m.ID.String()] = status == "ready"
		}
	}
	// SCRUM-271: resolve primary descriptor for the center pane. The pure
	// resolver returns kind+id; the enrichment helpers fill title/status from
	// data already loaded above (materials, links, the file_artifact fetched
	// for video playback). Both legacy video-first sessions
	// (primary_content_kind NULL + primary_video_artifact_id set) and
	// explicit kind=document/link rows produce a stable Primary block.
	primary := resolveSessionPrimary(session)
	primary = enrichSessionPrimaryFromMaterials(primary, allMaterials)
	primary = enrichSessionPrimaryFromLinks(primary, links)
	primary = enrichSessionPrimaryFromFileArtifact(primary, primaryVideoArtifact)

	response := GetSessionResponse{
		Session:              session,
		Artifacts:            artifacts,
		Materials:            allMaterials,
		VideoSources:         allVideoSources,
		PrimaryVideo:         primaryVideo,
		AdditionalVideos:     additionalVideos,
		RecentQuestions:      questions,
		RecentAnswers:        answers,
		Mode:                 mode,
		CreatedByDisplayName: createdByDisplayName,
		UnreadMaterialIDs:    unreadMaterialIDs,
		VideoAccessURL:       videoAccessURL,
		PlaybackReasonCode:   playbackReasonCode,
		PlaybackMessage:      playbackMessage,
		Links:                links,
		MaterialSlidesReady:  materialSlidesReady,
		MaterialSlidesStatus: materialSlidesStatus,
		Primary:              primary,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store") // video_access_url is presigned and time-limited; do not cache session response
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// UpdateSessionStatus updates the status, title, premise, etc. of a session (creator or admin only).
func (h *Handlers) UpdateSessionStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch && r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user := UserFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	// Extract session ID from URL path (/api/sessions/{id} or /sessions/{id})
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	var sessionIDStr string
	if len(pathParts) >= 3 && pathParts[0] == "api" && pathParts[1] == "sessions" {
		sessionIDStr = pathParts[2]
	} else if len(pathParts) >= 2 && pathParts[0] == "sessions" {
		sessionIDStr = pathParts[1]
	} else {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}

	sessionID, err := uuid.Parse(sessionIDStr)
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	// Load session (need it for auth and for title uniqueness check)
	session, err := h.DB.GetSession(r.Context(), sessionID)
	if err != nil || session == nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	// SCRUM-227: editor authority is granted by global admin OR per-session
	// session_memberships.role='creator' — including promoted creators, not
	// only the original by-email creator.
	canEdit, err := h.userIsSessionEditor(r.Context(), sessionID, user)
	if err != nil {
		log.Printf("UpdateSessionStatus userIsSessionEditor: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to check authorization"})
		return
	}
	if !canEdit {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only the session creator or an admin can update this session"})
		return
	}

	// Parse request body
	type UpdateSessionRequest struct {
		Status          *string               `json:"status"`
		Title           *string               `json:"title"`
		Premise         *string               `json:"premise"`
		PrimaryDecision *string               `json:"primary_decision"`
		DecisionOutcome *string               `json:"decision_outcome"`
		Primary         *SessionPrimaryUpdate `json:"primary,omitempty"` // SCRUM-272
	}

	var req UpdateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	// Update title if provided
	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if title == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title cannot be empty"})
			return
		}
		createdBy := ""
		if session.CreatedBy != nil {
			createdBy = *session.CreatedBy
		}
		exists, err := h.DB.SessionWithTitleExistsForCreator(r.Context(), createdBy, title, &sessionID)
		if err != nil {
			log.Printf("UpdateSessionStatus SessionWithTitleExistsForCreator: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to check session title"})
			return
		}
		if exists {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "A session with this name already exists. Please use a unique name."})
			return
		}
		if err := h.DB.UpdateSessionTitle(r.Context(), sessionID, title); err != nil {
			log.Printf("UpdateSessionStatus UpdateSessionTitle: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update session title"})
			return
		}
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

	// SCRUM-272: update primary content kind + matching pointer with
	// session-ownership validation. Empty kind clears the explicit primary.
	if req.Primary != nil {
		// SCRUM-283: snapshot the prior primary state before the apply so we
		// can write an audit row capturing the before/after transition. The
		// `session` variable was loaded at the top of the handler and reflects
		// the pre-PATCH state; use it as the source of truth.
		prevKind, prevID := snapshotSessionPrimary(session)
		if err := h.applySessionPrimaryUpdate(r.Context(), sessionID, req.Primary); err != nil {
			var derr *primaryUpdateError
			if errors.As(err, &derr) {
				writeJSON(w, derr.status, map[string]string{"error": derr.message, "code": derr.code})
				return
			}
			log.Printf("UpdateSessionStatus applySessionPrimaryUpdate: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update session primary"})
			return
		}
		// SCRUM-283: persistent audit row + websocket broadcast so participants
		// see the change without polling. Both are best-effort: if the audit
		// insert or broadcast fails, the PATCH still succeeded — log and move
		// on rather than rolling back the user's intentional change.
		newKind, newID := parseRequestedPrimary(req.Primary)
		actorID := actorUserID(user)
		if err := h.DB.InsertSessionPrimaryHistory(r.Context(), &database.SessionPrimaryHistoryRow{
			SessionID:   sessionID,
			ActorUserID: actorID,
			PrevKind:    prevKind,
			PrevID:      prevID,
			NewKind:     newKind,
			NewID:       newID,
		}); err != nil {
			log.Printf("UpdateSessionStatus InsertSessionPrimaryHistory: %v", err)
		}
		if h.Hub != nil {
			h.Hub.BroadcastSessionUpdated(sessionID)
		}
	}

	// Get updated session
	session, err = h.DB.GetSession(r.Context(), sessionID)
	if err != nil {
		log.Printf("Error getting updated session: %v", err)
		http.Error(w, fmt.Sprintf("Failed to get updated session: %v", err), http.StatusInternalServerError)
		return
	}

	// SCRUM-279: include the resolved primary descriptor so a creator who just
	// PATCHed the primary doesn't need a follow-up GET to read title/status.
	// The wrapper embeds *models.Session, so all existing wire fields stay at
	// the top level — only the new optional `primary` is added.
	resp := struct {
		*models.Session
		Primary *SessionPrimaryDescriptor `json:"primary,omitempty"`
	}{
		Session: session,
		Primary: h.resolveAndEnrichSessionPrimaryForResponse(r.Context(), session),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}
