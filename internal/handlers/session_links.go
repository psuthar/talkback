package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/auth"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/rag"
	"github.com/psuthar/talkback/internal/urlextract"
)

// AddSessionLinkRequest is the body for POST /api/sessions/:id/links.
type AddSessionLinkRequest struct {
	URL string `json:"url"`
}

// AddSessionLink handles POST /api/sessions/:id/links — validate URL, verify accessibility, extract content, store, reindex.
func (h *Handlers) AddSessionLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 4 || pathParts[0] != "api" || pathParts[1] != "sessions" || pathParts[3] != "links" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(pathParts[2])
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	session, err := h.DB.GetSession(r.Context(), sessionID)
	if err != nil || session == nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	isCreator := session.CreatedBy != nil && *session.CreatedBy == user.Email
	isAdmin := user.GlobalRole == models.GlobalRoleAdmin
	if !isCreator && !isAdmin {
		http.Error(w, "Only the session creator or an admin can add links", http.StatusForbidden)
		return
	}
	count, err := h.DB.CountSessionLinksBySessionID(r.Context(), sessionID)
	if err != nil {
		log.Printf("AddSessionLink count: %v", err)
		http.Error(w, "Failed to check links limit", http.StatusInternalServerError)
		return
	}
	if auth.Config.MaxLinksPerSession > 0 && count >= auth.Config.MaxLinksPerSession {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":           "session links limit reached",
			"max_links":       auth.Config.MaxLinksPerSession,
		})
		return
	}
	var req AddSessionLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	normalizedURL, err := urlextract.ValidateAndNormalizeURL(req.URL)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	link := &models.SessionLink{
		ID:        uuid.New(),
		SessionID: sessionID,
		URL:       normalizedURL,
		Status:    models.SessionLinkStatusPending,
	}
	result, err := urlextract.FetchAndExtract(r.Context(), normalizedURL, urlextract.DefaultMaxBytes)
	if err != nil {
		link.Status = models.SessionLinkStatusFailed
		errMsg := err.Error()
		if len(errMsg) > 500 {
			errMsg = errMsg[:500]
		}
		link.ErrorMessage = &errMsg
		if createErr := h.DB.CreateSessionLink(r.Context(), link); createErr != nil {
			log.Printf("AddSessionLink CreateSessionLink (failed): %v", createErr)
			http.Error(w, "Failed to save link", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(link)
		return
	}
	link.URL = result.FinalURL
	link.Status = models.SessionLinkStatusVerified
	if result.Title != "" {
		link.Title = &result.Title
	}
	bodyTrim := strings.TrimSpace(result.Body)
	if len(bodyTrim) > 0 {
		link.ExtractedText = &bodyTrim
	}
	if err := h.DB.CreateSessionLink(r.Context(), link); err != nil {
		log.Printf("AddSessionLink CreateSessionLink: %v", err)
		http.Error(w, "Failed to save link", http.StatusInternalServerError)
		return
	}
	// Index synchronously so the link is searchable before we return; avoids "no answer" when user asks immediately
	if link.ExtractedText != nil && *link.ExtractedText != "" {
		embedder := &rag.OpenAIEmbedder{}
		if indexErr := rag.IndexSession(r.Context(), h.DB, embedder, sessionID, h.Storage); indexErr != nil {
			log.Printf("AddSessionLink IndexSession: %v", indexErr)
		}
	} else {
		rag.IndexSessionAsync(sessionID, h.DB, h.Storage)
	}
	if h.Hub != nil {
		h.Hub.BroadcastSessionUpdated(sessionID)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(link)
}

// ListSessionLinks handles GET /api/sessions/:id/links.
func (h *Handlers) ListSessionLinks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 4 || pathParts[0] != "api" || pathParts[1] != "sessions" || pathParts[3] != "links" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(pathParts[2])
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	if _, err := h.DB.GetSession(r.Context(), sessionID); err != nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}
	links, err := h.DB.GetSessionLinksBySessionID(r.Context(), sessionID)
	if err != nil {
		log.Printf("ListSessionLinks: %v", err)
		http.Error(w, "Failed to list links", http.StatusInternalServerError)
		return
	}
	if links == nil {
		links = []*models.SessionLink{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(links)
}

// DeleteSessionLink handles DELETE /api/sessions/:id/links/:link_id.
func (h *Handlers) DeleteSessionLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 5 || pathParts[0] != "api" || pathParts[1] != "sessions" || pathParts[3] != "links" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(pathParts[2])
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	linkID, err := uuid.Parse(pathParts[4])
	if err != nil {
		http.Error(w, "Invalid link ID", http.StatusBadRequest)
		return
	}
	session, err := h.DB.GetSession(r.Context(), sessionID)
	if err != nil || session == nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	isCreator := session.CreatedBy != nil && *session.CreatedBy == user.Email
	isAdmin := user.GlobalRole == models.GlobalRoleAdmin
	if !isCreator && !isAdmin {
		http.Error(w, "Only the session creator or an admin can delete links", http.StatusForbidden)
		return
	}
	link, err := h.DB.GetSessionLinkByID(r.Context(), linkID)
	if err != nil || link == nil || link.SessionID != sessionID {
		http.Error(w, "Link not found", http.StatusNotFound)
		return
	}
	if err := h.DB.DeleteSessionChunksBySource(r.Context(), sessionID, "link", linkID); err != nil {
		log.Printf("DeleteSessionLink DeleteSessionChunksBySource: %v", err)
	}
	if err := h.DB.DeleteSessionLink(r.Context(), linkID); err != nil {
		log.Printf("DeleteSessionLink: %v", err)
		http.Error(w, "Failed to delete link", http.StatusInternalServerError)
		return
	}
	rag.IndexSessionAsync(sessionID, h.DB, h.Storage)
	if h.Hub != nil {
		h.Hub.BroadcastSessionUpdated(sessionID)
	}
	w.WriteHeader(http.StatusNoContent)
}
