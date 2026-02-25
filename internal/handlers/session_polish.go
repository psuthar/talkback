package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/utils"
)

// SessionPolishRequest body for POST /api/sessions/:id/questions/polish
type SessionPolishRequest struct {
	Text string `json:"text"`
}

// SessionPolishResponse response for POST /api/sessions/:id/questions/polish
type SessionPolishResponse struct {
	PolishedText string `json:"polished_text"`
}

// SessionPolishQuestion handles POST /api/sessions/:id/questions/polish.
// Query param mode=llm uses LLM to rewrite; otherwise rule-based polish.
func (h *Handlers) SessionPolishQuestion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	// /api/sessions/:id/questions/polish -> ["api","sessions",id,"questions","polish"]
	if len(pathParts) < 5 || pathParts[0] != "api" || pathParts[1] != "sessions" || pathParts[3] != "questions" || pathParts[4] != "polish" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	sessionIDStr := pathParts[2]
	sessionID, err := uuid.Parse(sessionIDStr)
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	_, err = h.DB.GetSession(ctx, sessionID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"message": "Session not found"})
		return
	}

	var req SessionPolishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"message": "text is required"})
		return
	}

	useLLM := r.URL.Query().Get("mode") == "llm"
	var polished string
	if useLLM {
		polished, err = utils.PolishSpokenQuestionWithLLM(ctx, req.Text)
		if err != nil {
			http.Error(w, "Polish failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		polished = utils.PolishSpokenQuestion(req.Text)
	}
	if polished == "" {
		polished = req.Text
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(SessionPolishResponse{PolishedText: polished})
}
