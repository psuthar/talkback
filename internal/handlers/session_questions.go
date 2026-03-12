package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/auth"
	"github.com/psuthar/talkback/internal/citation"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/utils"
)

// transcribeVoiceResponse is the JSON response for voice transcription endpoints.
type transcribeVoiceResponse struct {
	TranscribedText string   `json:"transcribed_text"`
	Confidence      *float32 `json:"confidence,omitempty"`
}

func stringPtr(s string) *string {
	return &s
}

func saveMultipartToTempFile(file multipart.File, header *multipart.FileHeader) (string, func(), error) {
	ext := ""
	if header != nil && header.Filename != "" {
		ext = filepath.Ext(header.Filename)
	}
	if ext == "" {
		ext = ".webm"
	}
	dir := filepath.Join("tmp", "voice")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", nil, err
	}
	dstPath := filepath.Join(dir, fmt.Sprintf("voice_%d%s", time.Now().UnixNano(), ext))
	out, err := os.Create(dstPath)
	if err != nil {
		return "", nil, err
	}
	defer out.Close()
	if _, err := out.ReadFrom(file); err != nil {
		_ = os.Remove(dstPath)
		return "", nil, err
	}
	if info, err := os.Stat(dstPath); err != nil {
		_ = os.Remove(dstPath)
		return "", nil, err
	} else if info.Size() == 0 {
		_ = os.Remove(dstPath)
		return "", nil, fmt.Errorf("uploaded audio file is empty")
	}
	absPath, err := filepath.Abs(dstPath)
	if err != nil {
		_ = os.Remove(dstPath)
		return "", nil, err
	}
	cleanup := func() {
		_ = os.Remove(absPath)
	}
	return absPath, cleanup, nil
}

// AskSessionQuestion asks a question in a session context
func (h *Handlers) AskSessionQuestion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 3 || pathParts[0] != "sessions" || pathParts[2] != "questions" {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(pathParts[1])
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	_, err = h.DB.GetSession(r.Context(), sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Session not found: %v", err), http.StatusNotFound)
		return
	}
	artifacts, err := h.DB.GetArtifactsBySessionID(r.Context(), sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get artifacts: %v", err), http.StatusInternalServerError)
		return
	}
	if len(artifacts) == 0 {
		http.Error(w, "No artifacts found for this session", http.StatusNotFound)
		return
	}
	artifact := artifacts[0]
	type AskSessionQuestionRequest struct {
		QuestionText     string `json:"question_text"`
		VideoTimeSeconds *int   `json:"video_time_seconds,omitempty"`
	}
	var req AskSessionQuestionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	if req.QuestionText == "" {
		http.Error(w, "question_text is required", http.StatusBadRequest)
		return
	}
	existingQuestion, existingAnswer, err := h.DB.FindExistingQuestionByText(r.Context(), sessionID, req.QuestionText, nil)
	if err != nil {
		log.Printf("Error checking for existing session question: %v", err)
	} else if existingQuestion != nil && existingAnswer != nil {
		log.Printf("Returning cached answer for duplicate session question: %s", req.QuestionText)
		response := AskQuestionResponse{
			Question: existingQuestion,
			Answer:   existingAnswer,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
		return
	}
	count, err := h.DB.CountQuestionsBySessionID(r.Context(), sessionID)
	if err != nil {
		log.Printf("Error counting session questions: %v", err)
		http.Error(w, "Failed to check question limit", http.StatusInternalServerError)
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
	question := &models.Question{
		ID:               uuid.New(),
		ArtifactID:       artifact.ID,
		SessionID:        sessionID,
		QuestionText:     req.QuestionText,
		QuestionSource:   models.QuestionSourceText,
		VideoTimeSeconds: req.VideoTimeSeconds,
	}
	if err := h.DB.CreateQuestion(r.Context(), question); err != nil {
		log.Printf("Error creating question: %v", err)
		http.Error(w, fmt.Sprintf("Failed to create question: %v", err), http.StatusInternalServerError)
		return
	}
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
	primaryVideo, _ := resolveEffectivePrimaryAndAdditional(allVideoSources)
	videoSource := primaryVideo
	verifiedLinks, _ := h.DB.GetVerifiedSessionLinksBySessionID(r.Context(), sessionID)
	chunks := utils.RetrieveChunks(req.QuestionText, allMaterials, videoSource, verifiedLinks, 5)
	priorQuestions, priorAnswers, err := h.DB.GetQuestionsBySessionID(r.Context(), sessionID, 10)
	if err != nil {
		log.Printf("Warning: Failed to get prior questions for context: %v", err)
		priorQuestions = []*models.Question{}
		priorAnswers = []*models.Answer{}
	}
	priorQA := make([]utils.PriorQAPair, 0, len(priorQuestions))
	answerMap := make(map[uuid.UUID]*models.Answer)
	for _, answer := range priorAnswers {
		answerMap[answer.QuestionID] = answer
	}
	for _, priorQuestion := range priorQuestions {
		if priorQuestion.ID == question.ID {
			continue
		}
		if priorAnswer, exists := answerMap[priorQuestion.ID]; exists && priorAnswer != nil {
			priorQA = append(priorQA, utils.PriorQAPair{
				Question: priorQuestion.QuestionText,
				Answer:   priorAnswer.AnswerText,
			})
		}
	}
	qaResponse, _, err := utils.GenerateAnswer(r.Context(), req.QuestionText, chunks, artifact.Title, utils.SessionContext{}, priorQA)
	if err != nil {
		log.Printf("Error generating answer: %v", err)
		qaResponse = &utils.QAResponse{
			AnswerStatus: "error",
			AnswerText:   fmt.Sprintf("Failed to generate answer: %v", err),
			Confidence:   0,
			Citations:    []models.Citation{},
		}
	}
	answer, err := utils.ConvertQAResponseToAnswer(question.ID, qaResponse, "gpt-4o-mini")
	if err != nil {
		log.Printf("Error converting answer: %v", err)
		http.Error(w, fmt.Sprintf("Failed to process answer: %v", err), http.StatusInternalServerError)
		return
	}
	if err := h.DB.CreateAnswer(r.Context(), answer); err != nil {
		log.Printf("Error creating answer: %v", err)
		http.Error(w, fmt.Sprintf("Failed to save answer: %v", err), http.StatusInternalServerError)
		return
	}
	if h.Hub != nil {
		h.Hub.BroadcastQuestionCreated(sessionID, question)
		h.Hub.BroadcastAnswerCreated(sessionID, answer)
	}
	response := AskQuestionResponse{
		Question: question,
		Answer:   answer,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}

// TranscribeSessionQuestionVoice accepts a short audio recording and returns a transcription.
func (h *Handlers) TranscribeSessionQuestionVoice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 4 || pathParts[0] != "sessions" || pathParts[2] != "questions" || pathParts[3] != "voice" {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(pathParts[1])
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	_, err = h.DB.GetSession(r.Context(), sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Session not found: %v", err), http.StatusNotFound)
		return
	}
	maxBytes := utils.VoiceMaxUploadBytesFromEnv()
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	if err := r.ParseMultipartForm(maxBytes); err != nil {
		http.Error(w, fmt.Sprintf("Invalid multipart form: %v", err), http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file is required", http.StatusBadRequest)
		return
	}
	defer file.Close()
	tempFilePath, cleanup, err := saveMultipartToTempFile(file, header)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to store upload: %v", err), http.StatusInternalServerError)
		return
	}
	defer cleanup()
	transcriber := utils.NewSpeechToTextFromEnv()
	if !transcriber.CanTranscribe() {
		http.Error(w, "Speech-to-text is not available. Configure OPENAI_API_KEY (Whisper API) or WHISPER_CLI (local).", http.StatusServiceUnavailable)
		return
	}
	text, confidence, err := transcriber.TranscribeAudio(r.Context(), tempFilePath)
	if err != nil {
		if errors.Is(err, utils.ErrAudioTooLong) {
			http.Error(w, fmt.Sprintf("Audio exceeds maximum duration (STT_MAX_AUDIO_SECONDS). %v", err), http.StatusRequestEntityTooLarge)
			return
		}
		if errors.Is(err, utils.ErrDailyCapExceeded) {
			http.Error(w, "Daily speech-to-text limit reached. Try again tomorrow.", http.StatusTooManyRequests)
			return
		}
		http.Error(w, fmt.Sprintf("Transcription failed: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(transcribeVoiceResponse{
		TranscribedText: text,
		Confidence:      confidence,
	})
}

// CreateSessionAnswer allows admin or session creator to provide an answer to a question (RequireAuth).
func (h *Handlers) CreateSessionAnswer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 5 || pathParts[0] != "sessions" || pathParts[2] != "questions" || pathParts[4] != "answers" {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(pathParts[1])
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	questionID, err := uuid.Parse(pathParts[3])
	if err != nil {
		http.Error(w, "Invalid question ID", http.StatusBadRequest)
		return
	}
	session, err := h.DB.GetSession(r.Context(), sessionID)
	if err != nil || session == nil {
		http.Error(w, fmt.Sprintf("Session not found: %v", err), http.StatusNotFound)
		return
	}
	isCreator := session.CreatedBy != nil && *session.CreatedBy == user.Email
	isAdmin := user.GlobalRole == models.GlobalRoleAdmin
	if !isCreator && !isAdmin {
		http.Error(w, "Only the session creator or an admin can add an answer", http.StatusForbidden)
		return
	}
	question, err := h.DB.GetQuestionByID(r.Context(), questionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Question not found: %v", err), http.StatusNotFound)
		return
	}
	if question.SessionID != sessionID {
		http.Error(w, "Question does not belong to this session", http.StatusBadRequest)
		return
	}
	type CreateAnswerRequest struct {
		AnswerText string `json:"answer_text"`
		Status     string `json:"status,omitempty"`
	}
	var req CreateAnswerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	if req.AnswerText == "" {
		http.Error(w, "answer_text is required", http.StatusBadRequest)
		return
	}
	answerStatus := models.AnswerStatusAnswered
	if req.Status != "" {
		answerStatus = models.AnswerStatus(req.Status)
		if answerStatus != models.AnswerStatusAnswered &&
			answerStatus != models.AnswerStatusNotCovered &&
			answerStatus != models.AnswerStatusError {
			answerStatus = models.AnswerStatusAnswered
		}
	}
	existingAnswer, err := h.DB.GetAnswerByQuestionID(r.Context(), questionID)
	if err == nil && existingAnswer != nil {
		_ = h.DB.DeleteAnswer(r.Context(), existingAnswer.ID)
	}
	answer := &models.Answer{
		ID:           uuid.New(),
		QuestionID:   questionID,
		AnswerText:   req.AnswerText,
		AnswerStatus: answerStatus,
		Confidence:   1.0,
		Citations:    []models.Citation{},
		Model:        stringPtr("manual"),
		AnsweredBy:   stringPtr(user.Email),
	}
	if err := h.DB.CreateAnswer(r.Context(), answer); err != nil {
		log.Printf("Error creating answer: %v", err)
		http.Error(w, fmt.Sprintf("Failed to create answer: %v", err), http.StatusInternalServerError)
		return
	}
	if h.Hub != nil {
		if existingAnswer != nil {
			h.Hub.BroadcastAnswerUpdated(sessionID, answer)
		} else {
			h.Hub.BroadcastAnswerCreated(sessionID, answer)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(answer)
}

// UpdateAnswerConfirmed updates the confirmed status of an answer (RequireAuth; admin or session creator only).
func (h *Handlers) UpdateAnswerConfirmed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch && r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 5 || pathParts[0] != "sessions" || pathParts[2] != "answers" || pathParts[4] != "confirm" {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(pathParts[1])
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	answerID, err := uuid.Parse(pathParts[3])
	if err != nil {
		http.Error(w, "Invalid answer ID", http.StatusBadRequest)
		return
	}
	session, err := h.DB.GetSession(r.Context(), sessionID)
	if err != nil || session == nil {
		http.Error(w, fmt.Sprintf("Session not found: %v", err), http.StatusNotFound)
		return
	}
	isCreator := session.CreatedBy != nil && *session.CreatedBy == user.Email
	isAdmin := user.GlobalRole == models.GlobalRoleAdmin
	if !isCreator && !isAdmin {
		http.Error(w, "Only the session creator or an admin can confirm an answer", http.StatusForbidden)
		return
	}
	answer, err := h.DB.GetAnswerByID(r.Context(), answerID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Answer not found: %v", err), http.StatusNotFound)
		return
	}
	if answer == nil {
		http.Error(w, "Answer not found", http.StatusNotFound)
		return
	}
	question, err := h.DB.GetQuestionByID(r.Context(), answer.QuestionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Question not found: %v", err), http.StatusNotFound)
		return
	}
	if question.SessionID != sessionID {
		http.Error(w, "Answer does not belong to this session", http.StatusBadRequest)
		return
	}
	if answer.AnswerStatus != models.AnswerStatusAnswered {
		http.Error(w, "Only answers with status 'answered' can be confirmed", http.StatusBadRequest)
		return
	}
	type UpdateConfirmedRequest struct {
		Confirmed bool `json:"confirmed"`
	}
	var req UpdateConfirmedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	if err := h.DB.UpdateAnswerConfirmed(r.Context(), answerID, req.Confirmed); err != nil {
		log.Printf("Error updating answer confirmed status: %v", err)
		http.Error(w, fmt.Sprintf("Failed to update confirmed status: %v", err), http.StatusInternalServerError)
		return
	}
	updatedAnswer, err := h.DB.GetAnswerByID(r.Context(), answerID)
	if err != nil || updatedAnswer == nil {
		log.Printf("Warning: Failed to get updated answer: %v", err)
		updatedAnswer = answer
		updatedAnswer.Confirmed = req.Confirmed
	}
	if h.Hub != nil {
		h.Hub.BroadcastAnswerUpdated(sessionID, updatedAnswer)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(updatedAnswer)
}

// TranscribeSessionAnswerVoice accepts a short audio recording for an answer and returns a transcription (RequireAuth; admin or creator).
func (h *Handlers) TranscribeSessionAnswerVoice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 6 || pathParts[0] != "sessions" || pathParts[2] != "questions" || pathParts[4] != "answers" || pathParts[5] != "voice" {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(pathParts[1])
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	questionID, err := uuid.Parse(pathParts[3])
	if err != nil {
		http.Error(w, "Invalid question ID", http.StatusBadRequest)
		return
	}
	session, err := h.DB.GetSession(r.Context(), sessionID)
	if err != nil || session == nil {
		http.Error(w, fmt.Sprintf("Session not found: %v", err), http.StatusNotFound)
		return
	}
	isCreator := session.CreatedBy != nil && *session.CreatedBy == user.Email
	isAdmin := user.GlobalRole == models.GlobalRoleAdmin
	if !isCreator && !isAdmin {
		http.Error(w, "Only the session creator or an admin can provide an answer", http.StatusForbidden)
		return
	}
	question, err := h.DB.GetQuestionByID(r.Context(), questionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Question not found: %v", err), http.StatusNotFound)
		return
	}
	if question.SessionID != sessionID {
		http.Error(w, "Question does not belong to this session", http.StatusBadRequest)
		return
	}
	maxBytes := utils.VoiceMaxUploadBytesFromEnv()
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	if err := r.ParseMultipartForm(maxBytes); err != nil {
		http.Error(w, fmt.Sprintf("Invalid multipart form: %v", err), http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file is required", http.StatusBadRequest)
		return
	}
	defer file.Close()
	tempFilePath, cleanup, err := saveMultipartToTempFile(file, header)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to store upload: %v", err), http.StatusInternalServerError)
		return
	}
	defer cleanup()
	transcriber := utils.NewSpeechToTextFromEnv()
	if !transcriber.CanTranscribe() {
		http.Error(w, "Speech-to-text is not available. Configure OPENAI_API_KEY (Whisper API) or WHISPER_CLI (local).", http.StatusServiceUnavailable)
		return
	}
	text, confidence, err := transcriber.TranscribeAudio(r.Context(), tempFilePath)
	if err != nil {
		if errors.Is(err, utils.ErrAudioTooLong) {
			http.Error(w, fmt.Sprintf("Audio exceeds maximum duration (STT_MAX_AUDIO_SECONDS). %v", err), http.StatusRequestEntityTooLarge)
			return
		}
		if errors.Is(err, utils.ErrDailyCapExceeded) {
			http.Error(w, "Daily speech-to-text limit reached. Try again tomorrow.", http.StatusTooManyRequests)
			return
		}
		http.Error(w, fmt.Sprintf("Transcription failed: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(transcribeVoiceResponse{
		TranscribedText: text,
		Confidence:      confidence,
	})
}

// GetSessionQuestions retrieves questions for a session
func (h *Handlers) GetSessionQuestions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 3 || pathParts[0] != "sessions" || pathParts[2] != "questions" {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(pathParts[1])
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	_, err = h.DB.GetSession(r.Context(), sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Session not found: %v", err), http.StatusNotFound)
		return
	}
	questions, answers, err := h.DB.GetQuestionsBySessionID(r.Context(), sessionID, 20)
	if err != nil {
		log.Printf("Error getting questions: %v", err)
		http.Error(w, fmt.Sprintf("Failed to get questions: %v", err), http.StatusInternalServerError)
		return
	}
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
	links, _ := h.DB.GetSessionLinksBySessionID(r.Context(), sessionID)
	materials, _ := h.DB.GetActiveMaterialsBySessionID(r.Context(), sessionID)
	videoSources, _ := h.DB.GetVideoSourcesBySessionID(r.Context(), sessionID)
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
			t := citation.ResolveCitationTarget(*c, videoSources, materials, links, chunkURLByChunkID)
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
	response := GetQuestionsResponse{
		Questions: questions,
		Answers:   answers,
	}
	if participantRef := strings.TrimSpace(r.URL.Query().Get("participant_ref")); participantRef != "" {
		unread, err := h.DB.GetUnreadQuestionIDs(r.Context(), sessionID, participantRef)
		if err != nil {
			log.Printf("Error getting unread question IDs: %v", err)
		} else {
			response.UnreadQuestionIDs = make([]string, 0, len(unread))
			for _, id := range unread {
				response.UnreadQuestionIDs = append(response.UnreadQuestionIDs, id.String())
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// MarkQuestionViewed records that the participant viewed (expanded) a question. POST body: { "participant_ref": "..." }.
func (h *Handlers) MarkQuestionViewed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 5 || pathParts[0] != "sessions" || pathParts[2] != "questions" || pathParts[4] != "view" {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(pathParts[1])
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	questionID, err := uuid.Parse(pathParts[3])
	if err != nil {
		http.Error(w, "Invalid question ID", http.StatusBadRequest)
		return
	}
	if _, err := h.DB.GetSession(r.Context(), sessionID); err != nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}
	var body struct {
		ParticipantRef string `json:"participant_ref"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.ParticipantRef) == "" {
		http.Error(w, "participant_ref required in body", http.StatusBadRequest)
		return
	}
	participantRef := strings.TrimSpace(body.ParticipantRef)
	if err := h.DB.MarkQuestionViewed(r.Context(), sessionID, participantRef, questionID); err != nil {
		log.Printf("Error marking question viewed: %v", err)
		http.Error(w, "Failed to record view", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetSessionTimeline retrieves the timeline for a session (ordered questions and answers)
func (h *Handlers) GetSessionTimeline(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 3 || pathParts[0] != "sessions" || pathParts[2] != "timeline" {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(pathParts[1])
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	_, err = h.DB.GetSession(r.Context(), sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Session not found: %v", err), http.StatusNotFound)
		return
	}
	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsedLimit, err := fmt.Sscanf(limitStr, "%d", &limit); err != nil || parsedLimit != 1 || limit <= 0 {
			limit = 100
		}
	}
	questions, answers, err := h.DB.GetSessionTimeline(r.Context(), sessionID, limit)
	if err != nil {
		log.Printf("Error getting session timeline: %v", err)
		http.Error(w, fmt.Sprintf("Failed to get timeline: %v", err), http.StatusInternalServerError)
		return
	}
	type TimelineEntry struct {
		Question      *models.Question `json:"question"`
		Answer        *models.Answer   `json:"answer,omitempty"`
		Timestamp     string           `json:"timestamp"`
		VideoTimeSecs *int             `json:"video_time_seconds,omitempty"`
	}
	timeline := make([]TimelineEntry, 0, len(questions))
	answerMap := make(map[uuid.UUID]*models.Answer)
	for _, answer := range answers {
		answerMap[answer.QuestionID] = answer
	}
	for _, question := range questions {
		entry := TimelineEntry{
			Question:      question,
			Answer:        answerMap[question.ID],
			Timestamp:     question.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			VideoTimeSecs: question.VideoTimeSeconds,
		}
		timeline = append(timeline, entry)
	}
	type GetTimelineResponse struct {
		SessionID string          `json:"session_id"`
		Timeline  []TimelineEntry `json:"timeline"`
		Count     int             `json:"count"`
	}
	response := GetTimelineResponse{
		SessionID: sessionID.String(),
		Timeline:  timeline,
		Count:     len(timeline),
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}
