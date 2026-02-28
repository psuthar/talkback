package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/auth"
	"github.com/psuthar/talkback/internal/citation"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/rag"
	"github.com/psuthar/talkback/internal/utils"
)

// SessionAskRequest body for POST /api/sessions/:id/ask
type SessionAskRequest struct {
	QuestionText string `json:"question_text"`
	AskedVia     string `json:"asked_via"` // "text" | "voice"
}

// SessionAskCitation is one citation in the response (actionable citations, Mission #5)
type SessionAskCitation struct {
	CitationID string                   `json:"citation_id"`
	ChunkID    string                   `json:"chunk_id"`
	SourceType string                   `json:"source_type"`
	SourceID   string                   `json:"source_id"`
	Anchor     map[string]interface{}   `json:"anchor,omitempty"`
	Label      string                   `json:"label"`
	Excerpt    string                   `json:"excerpt"`
	Navigation *citation.CitationTarget `json:"navigation,omitempty"`
}

// SessionAskAnswerResponse is the answer part of the response
type SessionAskAnswerResponse struct {
	ID         string               `json:"id"`
	AnswerText string               `json:"answer_text"`
	Citations  []SessionAskCitation `json:"citations"`
	LLMModel   string               `json:"llm_model,omitempty"`
	CreatedAt  string               `json:"created_at"`
}

// SessionAskResponse for POST /api/sessions/:id/ask
type SessionAskResponse struct {
	Question SessionAskQuestionResponse `json:"question"`
	Answer   SessionAskAnswerResponse   `json:"answer"`
}

type SessionAskQuestionResponse struct {
	ID           string `json:"id"`
	QuestionText string `json:"question_text"`
	CreatedAt    string `json:"created_at"`
}

// SessionAsk handles POST /api/sessions/:id/ask (session-scoped RAG Q&A)
func (h *Handlers) SessionAsk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 4 || pathParts[0] != "api" || pathParts[1] != "sessions" || pathParts[3] != "ask" {
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
	session, err := h.DB.GetSession(ctx, sessionID)
	if err != nil || session == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"message": "Session not found"})
		return
	}
	var req SessionAskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	req.QuestionText = strings.TrimSpace(req.QuestionText)
	if req.QuestionText == "" {
		http.Error(w, "question_text is required", http.StatusBadRequest)
		return
	}
	if req.AskedVia == "" {
		req.AskedVia = "text"
	}

	// First artifact for question row (questions require artifact_id)
	artifacts, err := h.DB.GetArtifactsBySessionID(ctx, sessionID)
	if err != nil || len(artifacts) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"message": "Session has no artifacts; create an artifact first"})
		return
	}
	artifactID := artifacts[0].ID

	// Optional: repeat-question cache by session
	existingQ, existingA, _ := h.DB.FindExistingQuestionByText(ctx, sessionID, req.QuestionText)
	if existingQ != nil && existingA != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(SessionAskResponse{
			Question: SessionAskQuestionResponse{
				ID:           existingQ.ID.String(),
				QuestionText: existingQ.QuestionText,
				CreatedAt:    existingQ.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			},
			Answer: h.sessionAskAnswerFromModelWithContext(ctx, sessionID, existingA),
		})
		return
	}

	// Ensure index (chunk + embed) if not ready
	embedder := &rag.OpenAIEmbedder{}
	if err := rag.EnsureSessionIndex(ctx, h.DB, embedder, sessionID, h.Storage); err != nil {
		log.Printf("SessionAsk EnsureSessionIndex: %v", err)
		// Continue with empty chunks; LLM will return not_covered
	}

	// Embed question and retrieve top-k (may retry after reindex if 0 chunks but session has transcript)
	questionEmbedding, err := embedder.Embed(ctx, []string{req.QuestionText})
	if err != nil || len(questionEmbedding) == 0 {
		log.Printf("SessionAsk Embed: %v", err)
	}
	var sessionChunks []models.SessionChunk
	if len(questionEmbedding) > 0 {
		sessionChunks, err = rag.RetrieveTopK(ctx, h.DB, sessionID, questionEmbedding[0], rag.DefaultTopK)
		if err != nil {
			log.Printf("SessionAsk RetrieveTopK: %v", err)
		}
		// If no chunks but session has transcript content, index may have been built before transcript was ready — force reindex and retry once
		if len(sessionChunks) == 0 && sessionHasTranscriptContent(ctx, h.DB, sessionID) {
			_ = h.DB.DeleteChunkEmbeddingsBySessionID(ctx, sessionID)
			_ = h.DB.DeleteSessionChunksBySessionID(ctx, sessionID)
			if reindexErr := rag.IndexSession(ctx, h.DB, embedder, sessionID, h.Storage); reindexErr != nil {
				log.Printf("SessionAsk reindex after 0 chunks: %v", reindexErr)
			} else {
				sessionChunks, err = rag.RetrieveTopK(ctx, h.DB, sessionID, questionEmbedding[0], rag.DefaultTopK)
				if err != nil {
					log.Printf("SessionAsk RetrieveTopK after reindex: %v", err)
				}
			}
		}
	}

	// Enforce per-session question limit
	count, err := h.DB.CountQuestionsBySessionID(ctx, sessionID)
	if err != nil {
		log.Printf("SessionAsk CountQuestionsBySessionID: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": "Failed to check question limit"})
		return
	}
	if count >= auth.Config.MaxQuestionsPerSession {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":          "session question limit reached",
			"max_questions":  auth.Config.MaxQuestionsPerSession,
		})
		return
	}

	// Create question
	question := &models.Question{
		ID:             uuid.New(),
		ArtifactID:     artifactID,
		SessionID:      sessionID,
		QuestionText:   req.QuestionText,
		QuestionSource: models.QuestionSourceText,
	}
	if err := h.DB.CreateQuestion(ctx, question); err != nil {
		log.Printf("SessionAsk CreateQuestion: %v", err)
		http.Error(w, "Failed to create question", http.StatusInternalServerError)
		return
	}

	// Convert to utils.Chunk for GenerateAnswer
	chunks := sessionChunksToUtilsChunks(sessionChunks)

	// When no chunks are available, use a friendlier message if content is still being imported
	emptyChunkMessage := ""
	if len(chunks) == 0 {
		if msg := sessionEmptyChunkMessage(ctx, h.DB, sessionID); msg != "" {
			emptyChunkMessage = msg
		}
	}

	// Prior Q&A for context
	priorQuestions, priorAnswers, _ := h.DB.GetQuestionsBySessionID(ctx, sessionID, 10)
	priorQA := buildPriorQA(priorQuestions, priorAnswers, question.ID)

	qaResponse, _, err := utils.GenerateAnswer(ctx, req.QuestionText, chunks, session.Title, priorQA)
	if emptyChunkMessage != "" && qaResponse != nil {
		qaResponse.AnswerText = emptyChunkMessage
	}
	if err != nil {
		log.Printf("SessionAsk GenerateAnswer: %v", err)
		qaResponse = &utils.QAResponse{
			AnswerStatus: "error",
			AnswerText:   fmt.Sprintf("Failed to generate answer: %v", err),
			Confidence:   0,
			Citations:    []models.Citation{},
		}
	}
	// Normalize citations to canonical form (citation_id, anchor, label, excerpt) before saving
	chunkMap := make(citation.ChunkByID)
	for _, c := range sessionChunks {
		chunkMap[c.ID.String()] = c
	}
	qaResponse.Citations = citation.NormalizeCitations(qaResponse.Citations, chunkMap)
	answer, err := utils.ConvertQAResponseToAnswer(question.ID, qaResponse, embedder.ModelName())
	if err != nil {
		log.Printf("SessionAsk ConvertQAResponseToAnswer: %v", err)
		http.Error(w, "Failed to process answer", http.StatusInternalServerError)
		return
	}
	if err := h.DB.CreateAnswer(ctx, answer); err != nil {
		log.Printf("SessionAsk CreateAnswer: %v", err)
		http.Error(w, "Failed to save answer", http.StatusInternalServerError)
		return
	}

	// Broadcast so creator view (and other clients) get real-time Q&A updates
	if h.Hub != nil {
		h.Hub.BroadcastQuestionCreated(sessionID, question)
		h.Hub.BroadcastAnswerCreated(sessionID, answer)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(SessionAskResponse{
		Question: SessionAskQuestionResponse{
			ID:           question.ID.String(),
			QuestionText: question.QuestionText,
			CreatedAt:    question.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		},
		Answer: h.sessionAskAnswerFromModelWithContext(ctx, sessionID, answer),
	})
}

func sessionChunksToUtilsChunks(chunks []models.SessionChunk) []utils.Chunk {
	out := make([]utils.Chunk, 0, len(chunks))
	for _, c := range chunks {
		locator := formatAnchorLocator(c.AnchorJSON)
		sourceID := ""
		if c.SourceID != nil {
			sourceID = c.SourceID.String()
		}
		out = append(out, utils.Chunk{
			ChunkID:    c.ID.String(),
			SourceType: c.SourceType,
			SourceID:   sourceID,
			Locator:    locator,
			Text:       c.Text,
		})
	}
	return out
}

func formatAnchorLocator(anchor map[string]interface{}) string {
	if anchor == nil {
		return ""
	}
	startMs, _ := anchor["start_ms"].(float64)
	endMs, _ := anchor["end_ms"].(float64)
	if startMs == 0 && endMs == 0 {
		if block, ok := anchor["block"].(float64); ok {
			return fmt.Sprintf("block %d", int(block))
		}
		return ""
	}
	// mm:ss–mm:ss
	sMin := int(startMs) / 60000
	sSec := (int(startMs) % 60000) / 1000
	eMin := int(endMs) / 60000
	eSec := (int(endMs) % 60000) / 1000
	return fmt.Sprintf("%d:%02d–%d:%02d", sMin, sSec, eMin, eSec)
}

func buildPriorQA(questions []*models.Question, answers []*models.Answer, excludeQuestionID uuid.UUID) []utils.PriorQAPair {
	answerMap := make(map[uuid.UUID]*models.Answer)
	for _, a := range answers {
		answerMap[a.QuestionID] = a
	}
	var out []utils.PriorQAPair
	for _, q := range questions {
		if q.ID == excludeQuestionID {
			continue
		}
		if a, ok := answerMap[q.ID]; ok && a != nil {
			labels := make([]string, 0, len(a.Citations))
			for _, c := range a.Citations {
				if c.CitationID != "" && c.Label != "" {
					labels = append(labels, c.CitationID+": "+c.Label)
				}
			}
			out = append(out, utils.PriorQAPair{
				Question:       q.QuestionText,
				Answer:         a.AnswerText,
				CitationLabels: labels,
			})
		}
	}
	return out
}

// sessionAskAnswerFromModelWithContext builds the answer response with citation_id, label, anchor, excerpt, and navigation.
func (h *Handlers) sessionAskAnswerFromModelWithContext(ctx context.Context, sessionID uuid.UUID, a *models.Answer) SessionAskAnswerResponse {
	videoSources, _ := h.DB.GetVideoSourcesBySessionID(ctx, sessionID)
	materials, _ := h.DB.GetActiveMaterialsBySessionID(ctx, sessionID)
	return sessionAskAnswerFromAnswer(a, videoSources, materials)
}

func sessionAskAnswerFromAnswer(a *models.Answer, videoSources []*models.VideoSource, materials []*models.Material) SessionAskAnswerResponse {
	citations := make([]SessionAskCitation, 0, len(a.Citations))
	for _, c := range a.Citations {
		cite := SessionAskCitation{
			CitationID: c.CitationID,
			ChunkID:    c.ChunkID,
			SourceType: c.SourceType,
			SourceID:   c.SourceID,
			Label:      c.Label,
			Excerpt:    c.Excerpt,
		}
		if cite.CitationID == "" {
			cite.CitationID = "C?"
		}
		if cite.Label == "" {
			cite.Label = c.SourceType + " " + c.ChunkID
		}
		if cite.Excerpt == "" {
			cite.Excerpt = c.Snippet
		}
		if c.Anchor != nil {
			cite.Anchor = citationAnchorToMap(c.Anchor)
		}
		cite.Navigation = ptr(citation.ResolveCitationTarget(c, videoSources, materials))
		citations = append(citations, cite)
	}
	modelStr := ""
	if a.Model != nil {
		modelStr = *a.Model
	}
	return SessionAskAnswerResponse{
		ID:         a.ID.String(),
		AnswerText: a.AnswerText,
		Citations:  citations,
		LLMModel:   modelStr,
		CreatedAt:  a.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

func citationAnchorToMap(a *models.CitationAnchor) map[string]interface{} {
	if a == nil {
		return nil
	}
	m := map[string]interface{}{"type": a.Type}
	if a.StartMs != nil {
		m["start_ms"] = *a.StartMs
	}
	if a.EndMs != nil {
		m["end_ms"] = *a.EndMs
	}
	if a.Page != nil {
		m["page"] = *a.Page
	}
	if a.Section != "" {
		m["section"] = a.Section
	}
	if a.Block != nil {
		m["block"] = *a.Block
	}
	return m
}

func ptr(t citation.CitationTarget) *citation.CitationTarget {
	return &t
}

// sessionEmptyChunkMessage returns a user-facing message when the session has no indexed chunks but content might be on the way (e.g. ingestion in progress). Otherwise returns "".
func sessionEmptyChunkMessage(ctx context.Context, db *database.DB, sessionID uuid.UUID) string {
	job, err := db.GetIngestionJobBySessionID(ctx, sessionID)
	if err != nil || job == nil {
		return ""
	}
	switch job.State {
	case "queued", "fetching":
		return "Transcript or content is still being imported. Please wait a moment and try again."
	}
	return ""
}

// sessionHasTranscriptContent returns true if the session has transcript content (transcripts table or video_sources) so reindex might yield chunks.
func sessionHasTranscriptContent(ctx context.Context, db *database.DB, sessionID uuid.UUID) bool {
	trans, err := db.GetTranscriptBySessionID(ctx, sessionID, "")
	if err == nil && trans != nil && trans.Status == models.TranscriptStatusReady {
		segments, err := db.ListSegmentsByTranscriptID(ctx, trans.ID)
		if err == nil && len(segments) > 0 {
			return true
		}
	}
	videoSources, err := db.GetVideoSourcesBySessionID(ctx, sessionID)
	if err != nil {
		return false
	}
	for _, vs := range videoSources {
		if vs.TranscriptText != nil && *vs.TranscriptText != "" {
			return true
		}
		if len(vs.TranscriptSegments) > 0 {
			return true
		}
	}
	return false
}
