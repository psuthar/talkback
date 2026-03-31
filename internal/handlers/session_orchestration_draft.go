package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/citation"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/orchestration"
	"github.com/psuthar/talkback/internal/rag"
	"github.com/psuthar/talkback/internal/utils"
)

var (
	errOrchestrationQuestionAnswered    = errors.New("question already has a published or non-draft answer")
	errOrchestrationQuestionNotFound    = errors.New("question not found")
	errOrchestrationQuestionWrongSession = errors.New("question does not belong to this session")
)

// CreateOrchestrationDraftAnswer handles POST /api/sessions/:id/orchestration/draft-answers and
// POST /sessions/:id/orchestration/draft-answers (RequireAuth; session creator or admin).
// Generates an AI draft for an existing question, persists it with confirmed=false, and does not broadcast.
func (h *Handlers) CreateOrchestrationDraftAnswer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	sessionID, ok := parseOrchestrationDraftPostPath(r.URL.Path)
	if !ok {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	var body struct {
		QuestionID string `json:"question_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	questionID, err := uuid.Parse(strings.TrimSpace(body.QuestionID))
	if err != nil {
		http.Error(w, "question_id is required and must be a UUID", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	session, err := h.DB.GetSession(ctx, sessionID)
	if err != nil || session == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Session not found"})
		return
	}
	isCreator := session.CreatedBy != nil && *session.CreatedBy == user.Email
	isAdmin := user.GlobalRole == models.GlobalRoleAdmin
	if !isCreator && !isAdmin {
		http.Error(w, "Only the session creator or an admin can create a draft answer", http.StatusForbidden)
		return
	}

	answer, genErr := h.generateOrchestrationDraftAnswer(ctx, session, sessionID, questionID)
	if genErr != nil {
		switch {
		case errors.Is(genErr, errOrchestrationQuestionAnswered):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": genErr.Error()})
			return
		case errors.Is(genErr, errOrchestrationQuestionNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"message": genErr.Error()})
			return
		case errors.Is(genErr, errOrchestrationQuestionWrongSession):
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": genErr.Error()})
			return
		}
		log.Printf("CreateOrchestrationDraftAnswer: %v", genErr)
		http.Error(w, "Failed to generate draft answer", http.StatusInternalServerError)
		return
	}
	if err := h.DB.CreateAnswer(ctx, answer); err != nil {
		log.Printf("CreateOrchestrationDraftAnswer CreateAnswer: %v", err)
		http.Error(w, "Failed to save draft answer", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(answer)
}

// DeleteOrchestrationDraftAnswer handles DELETE .../orchestration/draft-answers/:answer_id (creator or admin).
// Removes an unconfirmed orchestration draft only.
func (h *Handlers) DeleteOrchestrationDraftAnswer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	sessionID, answerID, ok := parseOrchestrationDraftDeletePath(r.URL.Path)
	if !ok {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	session, err := h.DB.GetSession(ctx, sessionID)
	if err != nil || session == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Session not found"})
		return
	}
	isCreator := session.CreatedBy != nil && *session.CreatedBy == user.Email
	isAdmin := user.GlobalRole == models.GlobalRoleAdmin
	if !isCreator && !isAdmin {
		http.Error(w, "Only the session creator or an admin can delete a draft answer", http.StatusForbidden)
		return
	}
	ans, err := h.DB.GetAnswerByID(ctx, answerID)
	if err != nil || ans == nil {
		http.Error(w, "Answer not found", http.StatusNotFound)
		return
	}
	q, err := h.DB.GetQuestionByID(ctx, ans.QuestionID)
	if err != nil || q == nil || q.SessionID != sessionID {
		http.Error(w, "Answer does not belong to this session", http.StatusBadRequest)
		return
	}
	if !orchestration.IsOrchestrationDraftAnswer(ans) {
		http.Error(w, "Only orchestration draft answers can be deleted here", http.StatusBadRequest)
		return
	}
	if ans.Confirmed {
		http.Error(w, "Draft is already confirmed; use session flows to replace if needed", http.StatusBadRequest)
		return
	}
	if err := h.DB.DeleteAnswer(ctx, answerID); err != nil {
		log.Printf("DeleteOrchestrationDraftAnswer: %v", err)
		http.Error(w, "Failed to delete draft answer", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func parseOrchestrationDraftPostPath(path string) (sessionID uuid.UUID, ok bool) {
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	if len(parts) == 5 && parts[0] == "api" && parts[1] == "sessions" && parts[3] == "orchestration" && parts[4] == "draft-answers" {
		sessionID, err := uuid.Parse(parts[2])
		if err != nil {
			return uuid.UUID{}, false
		}
		return sessionID, true
	}
	if len(parts) == 4 && parts[0] == "sessions" && parts[2] == "orchestration" && parts[3] == "draft-answers" {
		sessionID, err := uuid.Parse(parts[1])
		if err != nil {
			return uuid.UUID{}, false
		}
		return sessionID, true
	}
	return uuid.UUID{}, false
}

func parseOrchestrationDraftDeletePath(path string) (sessionID, answerID uuid.UUID, ok bool) {
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	if len(parts) == 6 && parts[0] == "api" && parts[1] == "sessions" && parts[3] == "orchestration" && parts[4] == "draft-answers" {
		var err error
		sessionID, err = uuid.Parse(parts[2])
		if err != nil {
			return uuid.UUID{}, uuid.UUID{}, false
		}
		answerID, err = uuid.Parse(parts[5])
		if err != nil {
			return uuid.UUID{}, uuid.UUID{}, false
		}
		return sessionID, answerID, true
	}
	if len(parts) == 5 && parts[0] == "sessions" && parts[2] == "orchestration" && parts[3] == "draft-answers" {
		var err error
		sessionID, err = uuid.Parse(parts[1])
		if err != nil {
			return uuid.UUID{}, uuid.UUID{}, false
		}
		answerID, err = uuid.Parse(parts[4])
		if err != nil {
			return uuid.UUID{}, uuid.UUID{}, false
		}
		return sessionID, answerID, true
	}
	return uuid.UUID{}, uuid.UUID{}, false
}

func (h *Handlers) generateOrchestrationDraftAnswer(ctx context.Context, session *models.Session, sessionID, questionID uuid.UUID) (*models.Answer, error) {
	question, err := h.DB.GetQuestionByID(ctx, questionID)
	if err != nil || question == nil {
		return nil, errOrchestrationQuestionNotFound
	}
	if question.SessionID != sessionID {
		return nil, errOrchestrationQuestionWrongSession
	}

	latest, err := h.DB.GetLatestAnswerByQuestionID(ctx, questionID)
	if err != nil {
		return nil, fmt.Errorf("load answer: %w", err)
	}
	if latest != nil {
		if orchestration.IsOrchestrationDraftAnswer(latest) && !latest.Confirmed {
			if err := h.DB.DeleteAnswer(ctx, latest.ID); err != nil {
				return nil, fmt.Errorf("replace draft: %w", err)
			}
		} else {
			return nil, errOrchestrationQuestionAnswered
		}
	}

	embedder := &rag.OpenAIEmbedder{}
	if err := rag.EnsureSessionIndex(ctx, h.DB, embedder, sessionID, h.Storage); err != nil {
		log.Printf("CreateOrchestrationDraft EnsureSessionIndex: %v", err)
	}

	videoSources, _ := h.DB.GetVideoSourcesBySessionID(ctx, sessionID)
	primary, _ := resolveEffectivePrimaryAndAdditional(videoSources)
	var primaryVideoID *uuid.UUID
	if primary != nil {
		primaryVideoID = &primary.ID
	}

	qText := question.QuestionText
	questionEmbedding, err := embedder.Embed(ctx, []string{qText})
	if err != nil || len(questionEmbedding) == 0 {
		log.Printf("CreateOrchestrationDraft Embed: %v", err)
	}
	var sessionChunks []models.SessionChunk
	if len(questionEmbedding) > 0 {
		sessionChunks, err = rag.RetrieveTopK(ctx, h.DB, sessionID, questionEmbedding[0], rag.DefaultTopK, primaryVideoID)
		if err != nil {
			log.Printf("CreateOrchestrationDraft RetrieveTopK: %v", err)
		}
		if len(sessionChunks) == 0 && sessionHasIndexableContent(ctx, h.DB, sessionID) {
			_ = h.DB.DeleteChunkEmbeddingsBySessionID(ctx, sessionID)
			_ = h.DB.DeleteSessionChunksBySessionID(ctx, sessionID)
			if reindexErr := rag.IndexSession(ctx, h.DB, embedder, sessionID, h.Storage); reindexErr != nil {
				log.Printf("CreateOrchestrationDraft reindex: %v", reindexErr)
			} else {
				sessionChunks, err = rag.RetrieveTopK(ctx, h.DB, sessionID, questionEmbedding[0], rag.DefaultTopK, primaryVideoID)
				if err != nil {
					log.Printf("CreateOrchestrationDraft RetrieveTopK after reindex: %v", err)
				}
			}
		}
	}

	if os.Getenv("ASK_TRACE") == "1" {
		traceAskLookup(qText, sessionChunks)
	}
	chunks := sessionChunksToUtilsChunks(sessionChunks)
	emptyChunkMessage := ""
	if len(chunks) == 0 {
		if msg := sessionEmptyChunkMessage(ctx, h.DB, sessionID); msg != "" {
			emptyChunkMessage = msg
		}
	}

	priorQA := buildThreadPriorQA(ctx, h.DB, question.ParentQuestionID)
	priorQuestions, priorAnswers, _ := h.DB.GetQuestionsBySessionID(ctx, sessionID, 10)
	priorQA = append(priorQA, buildPriorQA(priorQuestions, priorAnswers, question.ID)...)

	sessionCtx := utils.SessionContext{
		Premise:         session.Premise,
		PrimaryDecision: session.PrimaryDecision,
	}
	qaResponse, _, err := utils.GenerateAnswer(ctx, qText, chunks, session.Title, sessionCtx, priorQA)
	if emptyChunkMessage != "" && qaResponse != nil {
		qaResponse.AnswerText = emptyChunkMessage
	}
	if err != nil {
		log.Printf("CreateOrchestrationDraft GenerateAnswer: %v", err)
		qaResponse = &utils.QAResponse{
			AnswerStatus: "error",
			AnswerText:   fmt.Sprintf("Failed to generate answer: %v", err),
			Confidence:   0,
			Citations:    []models.Citation{},
		}
	}
	if qaResponse == nil {
		qaResponse = &utils.QAResponse{
			AnswerStatus: "error",
			AnswerText:   "Failed to generate answer",
			Confidence:   0,
			Citations:    []models.Citation{},
		}
	}

	chunkMap := make(citation.ChunkByID)
	for _, c := range sessionChunks {
		chunkMap[c.ID.String()] = c
	}
	materialLabels := make(citation.MaterialLabels)
	if materials, _ := h.DB.GetActiveMaterialsBySessionID(ctx, sessionID); materials != nil {
		for _, m := range materials {
			name := m.Filename
			if m.Title != nil && *m.Title != "" {
				name = *m.Title
			}
			if name != "" {
				materialLabels[m.ID.String()] = name
			}
		}
	}
	qaResponse.Citations = citation.NormalizeCitations(qaResponse.Citations, chunkMap, materialLabels)

	answer, err := utils.ConvertQAResponseToAnswer(question.ID, qaResponse, orchestration.DraftAnswerModel)
	if err != nil {
		return nil, fmt.Errorf("convert answer: %w", err)
	}
	answer.Confirmed = false
	return answer, nil
}
