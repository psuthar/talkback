package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/rag"
)

// SessionReindex handles POST /api/sessions/:id/reindex — force rebuild of session RAG index (transcript + materials).
// Use this when the session transcript exists (e.g. on video_sources) but Q&A returns "not_covered" because
// the index was built before transcript was available or only used the transcripts table.
func (h *Handlers) SessionReindex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 4 || pathParts[0] != "api" || pathParts[1] != "sessions" || pathParts[3] != "reindex" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(pathParts[2])
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	if _, err := h.DB.GetSession(ctx, sessionID); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"message": "Session not found"})
		return
	}
	if err := h.DB.DeleteChunkEmbeddingsBySessionID(ctx, sessionID); err != nil {
		log.Printf("SessionReindex: delete embeddings: %v", err)
	}
	if err := h.DB.DeleteSessionChunksBySessionID(ctx, sessionID); err != nil {
		log.Printf("SessionReindex: delete chunks: %v", err)
	}
	embedder := &rag.OpenAIEmbedder{}
	if err := rag.IndexSession(ctx, h.DB, embedder, sessionID, h.Storage); err != nil {
		log.Printf("SessionReindex: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": "Reindex failed: " + err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "Index rebuilt. You can ask questions again."})
}
