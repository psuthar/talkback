package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/utils"
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
	Session         *models.Session    `json:"session"`
	Artifacts       []*models.Artifact `json:"artifacts"`
	Materials       []*models.Material `json:"materials"`
	VideoSources    []*models.VideoSource `json:"video_sources"`
	RecentQuestions []*models.Question `json:"recent_questions"`
	RecentAnswers   []*models.Answer   `json:"recent_answers"`
	Mode            string             `json:"mode"` // "creator" or "participant"
}

type JoinParticipantRequest struct {
	ParticipantRef string `json:"participant_ref"`
}

type CreateEventRequest struct {
	ParticipantRef   *string                `json:"participant_ref,omitempty"`
	EventType        string                 `json:"event_type"`
	VideoTimeSeconds *int                   `json:"video_time_seconds,omitempty"`
	Payload          map[string]interface{} `json:"payload,omitempty"`
}

type transcribeVoiceResponse struct {
	TranscribedText string   `json:"transcribed_text"`
	Confidence      *float32 `json:"confidence,omitempty"`
}

// CreateSession creates a new session (no longer requires artifact)
func (h *Handlers) CreateSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request body
	var req CreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if req.Title == "" {
		http.Error(w, "title is required", http.StatusBadRequest)
		return
	}

	// Create session
	session := &models.Session{
		ID:         uuid.New(),
		Title:      req.Title,
		CreatedBy:  req.CreatedBy,
		Status:     models.SessionStatusOpen,
	}

	if err := h.DB.CreateSession(r.Context(), session); err != nil {
		log.Printf("Error creating session: %v", err)
		http.Error(w, fmt.Sprintf("Failed to create session: %v", err), http.StatusInternalServerError)
		return
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

	// Get materials and video sources from session
	allMaterials, err := h.DB.GetMaterialsBySessionID(r.Context(), sessionID)
	if err != nil {
		log.Printf("Warning: Failed to get materials: %v", err)
		allMaterials = []*models.Material{}
	}
	
	allVideoSources, err := h.DB.GetVideoSourcesBySessionID(r.Context(), sessionID)
	if err != nil {
		log.Printf("Warning: Failed to get video sources: %v", err)
		allVideoSources = []*models.VideoSource{}
	}

	// Get recent questions (limit 20)
	questions, answers, err := h.DB.GetQuestionsBySessionID(r.Context(), sessionID, 20)
	if err != nil {
		log.Printf("Warning: Failed to get questions: %v", err)
		questions = []*models.Question{}
		answers = []*models.Answer{}
	}

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

	response := GetSessionResponse{
		Session:         session,
		Artifacts:       artifacts,
		Materials:       allMaterials,
		VideoSources:    allVideoSources,
		RecentQuestions: questions,
		RecentAnswers:   answers,
		Mode:            mode,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// JoinSessionParticipant creates or updates a session participant
func (h *Handlers) JoinSessionParticipant(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID from URL path
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 3 || pathParts[0] != "sessions" || pathParts[2] != "participants" {
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

	// Parse request body
	var req JoinParticipantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if req.ParticipantRef == "" {
		http.Error(w, "participant_ref is required", http.StatusBadRequest)
		return
	}

	// Upsert participant
	participant, err := h.DB.UpsertSessionParticipant(r.Context(), sessionID, req.ParticipantRef)
	if err != nil {
		log.Printf("Error upserting participant: %v", err)
		http.Error(w, fmt.Sprintf("Failed to join session: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(participant)
}

// CreateSessionEvent creates a new session event
func (h *Handlers) CreateSessionEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID from URL path
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 3 || pathParts[0] != "sessions" || pathParts[2] != "events" {
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

	// Parse request body
	var req CreateEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	// Validate event type
	eventType := models.SessionEventType(req.EventType)
	validTypes := []models.SessionEventType{
		models.SessionEventTypeJoin,
		models.SessionEventTypeLeave,
		models.SessionEventTypePlay,
		models.SessionEventTypePause,
		models.SessionEventTypeSeek,
		models.SessionEventTypeQuestion,
	}
	valid := false
	for _, vt := range validTypes {
		if eventType == vt {
			valid = true
			break
		}
	}
	if !valid {
		http.Error(w, fmt.Sprintf("Invalid event_type: %s", req.EventType), http.StatusBadRequest)
		return
	}

	// Create event
	event := &models.SessionEvent{
		ID:              uuid.New(),
		SessionID:       sessionID,
		ParticipantRef:  req.ParticipantRef,
		EventType:       eventType,
		VideoTimeSeconds: req.VideoTimeSeconds,
		Payload:         req.Payload,
	}
	if event.Payload == nil {
		event.Payload = make(map[string]interface{})
	}

	if err := h.DB.CreateSessionEvent(r.Context(), event); err != nil {
		log.Printf("Error creating session event: %v", err)
		http.Error(w, fmt.Sprintf("Failed to create event: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(event)
}

// AskSessionQuestion asks a question in a session context
func (h *Handlers) AskSessionQuestion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID from URL path
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

	// Verify session exists
	_, err = h.DB.GetSession(r.Context(), sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Session not found: %v", err), http.StatusNotFound)
		return
	}

	// Get artifacts for this session
	artifacts, err := h.DB.GetArtifactsBySessionID(r.Context(), sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get artifacts: %v", err), http.StatusInternalServerError)
		return
	}
	
	if len(artifacts) == 0 {
		http.Error(w, "No artifacts found for this session", http.StatusNotFound)
		return
	}
	
	// Use first artifact for question (or aggregate across all artifacts)
	artifact := artifacts[0]

	// Parse request body
	type AskSessionQuestionRequest struct {
		QuestionText    string `json:"question_text"`
		VideoTimeSeconds *int  `json:"video_time_seconds,omitempty"`
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

	// Check for existing question with same text in this session (repeat-question caching)
	existingQuestion, existingAnswer, err := h.DB.FindExistingQuestionByText(r.Context(), sessionID, req.QuestionText)
	if err != nil {
		log.Printf("Error checking for existing session question: %v", err)
		// Continue with new question creation on error
	} else if existingQuestion != nil && existingAnswer != nil {
		// Return cached answer
		log.Printf("Returning cached answer for duplicate session question: %s", req.QuestionText)
		response := AskQuestionResponse{
			Question: existingQuestion,
			Answer:   existingAnswer,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK) // 200 OK for cached response
		json.NewEncoder(w).Encode(response)
		return
	}

	// Create new question with session_id
	question := &models.Question{
		ID:              uuid.New(),
		ArtifactID:      artifact.ID,
		SessionID:       sessionID, // Questions belong to sessions (required)
		QuestionText:    req.QuestionText,
		QuestionSource:  models.QuestionSourceText,
		VideoTimeSeconds: req.VideoTimeSeconds, // Include timestamp if provided
	}

	if err := h.DB.CreateQuestion(r.Context(), question); err != nil {
		log.Printf("Error creating question: %v", err)
		http.Error(w, fmt.Sprintf("Failed to create question: %v", err), http.StatusInternalServerError)
		return
	}

	// Retrieve materials and video sources from session
	allMaterials, err := h.DB.GetMaterialsBySessionID(r.Context(), sessionID)
	if err != nil {
		log.Printf("Warning: Failed to get materials: %v", err)
		allMaterials = []*models.Material{}
	}
	
	allVideoSources, err := h.DB.GetVideoSourcesBySessionID(r.Context(), sessionID)
	if err != nil {
		log.Printf("Warning: Failed to get video sources: %v", err)
		allVideoSources = []*models.VideoSource{}
	}
	
	// Use first video source for RAG (or aggregate)
	var videoSource *models.VideoSource
	if len(allVideoSources) > 0 {
		videoSource = allVideoSources[0]
	}

	// Perform retrieval
	chunks := utils.RetrieveChunks(req.QuestionText, allMaterials, videoSource, 5)

	// Get prior questions and answers from this session for context accumulation
	priorQuestions, priorAnswers, err := h.DB.GetQuestionsBySessionID(r.Context(), sessionID, 10)
	if err != nil {
		log.Printf("Warning: Failed to get prior questions for context: %v", err)
		priorQuestions = []*models.Question{}
		priorAnswers = []*models.Answer{}
	}

	// Build prior Q&A pairs (excluding the current question we just created)
	priorQA := make([]utils.PriorQAPair, 0, len(priorQuestions))
	answerMap := make(map[uuid.UUID]*models.Answer)
	for _, answer := range priorAnswers {
		answerMap[answer.QuestionID] = answer
	}
	
	for _, priorQuestion := range priorQuestions {
		// Skip the current question (it won't have an answer yet, but be safe)
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

	// Generate answer using LLM with prior Q&A context
	qaResponse, _, err := utils.GenerateAnswer(r.Context(), req.QuestionText, chunks, artifact.Title, priorQA)
	if err != nil {
		log.Printf("Error generating answer: %v", err)
		// Still create an error answer
		qaResponse = &utils.QAResponse{
			AnswerStatus: "error",
			AnswerText:   fmt.Sprintf("Failed to generate answer: %v", err),
			Confidence:   0,
			Citations:    []models.Citation{},
		}
	}

	// Convert to Answer model
	answer, err := utils.ConvertQAResponseToAnswer(question.ID, qaResponse, "gpt-4o-mini")
	if err != nil {
		log.Printf("Error converting answer: %v", err)
		http.Error(w, fmt.Sprintf("Failed to process answer: %v", err), http.StatusInternalServerError)
		return
	}

	// Save answer
	if err := h.DB.CreateAnswer(r.Context(), answer); err != nil {
		log.Printf("Error creating answer: %v", err)
		http.Error(w, fmt.Sprintf("Failed to save answer: %v", err), http.StatusInternalServerError)
		return
	}

	// Broadcast question created event via WebSocket
	if h.Hub != nil {
		h.Hub.BroadcastQuestionCreated(sessionID, question)
		// Also broadcast answer created since it's created immediately
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
// It does not persist audio or create a question. The client must confirm/edit and then call
// POST /sessions/{id}/questions with the resulting text.
func (h *Handlers) TranscribeSessionQuestionVoice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID from URL path: /sessions/{id}/questions/voice
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

	// Verify session exists
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

	transcriber := utils.NewWhisperCLITranscriberFromEnv()
	if !transcriber.CanTranscribe() {
		http.Error(w, "Speech-to-text is not available (whisper CLI not found). Configure WHISPER_CLI and install openai-whisper.", http.StatusServiceUnavailable)
		return
	}

	text, confidence, err := transcriber.TranscribeAudio(r.Context(), tempFilePath)
	if err != nil {
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

// CreateSessionAnswer allows creators to answer a question in a session
func (h *Handlers) CreateSessionAnswer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID and question ID from URL path: /sessions/{id}/questions/{question_id}/answers
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

	// Verify session exists
	_, err = h.DB.GetSession(r.Context(), sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Session not found: %v", err), http.StatusNotFound)
		return
	}

	// Verify question exists and belongs to this session
	question, err := h.DB.GetQuestionByID(r.Context(), questionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Question not found: %v", err), http.StatusNotFound)
		return
	}
	if question.SessionID != sessionID {
		http.Error(w, "Question does not belong to this session", http.StatusBadRequest)
		return
	}

	// Parse request body
	type CreateAnswerRequest struct {
		AnswerText string `json:"answer_text"`
		Status     string `json:"status,omitempty"` // "answered", "not_covered", "error"
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

	// Determine answer status
	answerStatus := models.AnswerStatusAnswered
	if req.Status != "" {
		answerStatus = models.AnswerStatus(req.Status)
		// Validate status
		if answerStatus != models.AnswerStatusAnswered && 
		   answerStatus != models.AnswerStatusNotCovered && 
		   answerStatus != models.AnswerStatusError {
			answerStatus = models.AnswerStatusAnswered
		}
	}

	// Check if answer already exists - delete it to replace with new one
	existingAnswer, err := h.DB.GetAnswerByQuestionID(r.Context(), questionID)
	if err == nil && existingAnswer != nil {
		// Delete existing answer to replace it
		_ = h.DB.DeleteAnswer(r.Context(), existingAnswer.ID)
	}

	// Create new answer
	answer := &models.Answer{
		ID:           uuid.New(),
		QuestionID:   questionID,
		AnswerText:   req.AnswerText,
		AnswerStatus: answerStatus,
		Confidence:   1.0, // Manual answers have full confidence
		Citations:    []models.Citation{},
		Model:        stringPtr("manual"),
	}

	if err := h.DB.CreateAnswer(r.Context(), answer); err != nil {
		log.Printf("Error creating answer: %v", err)
		http.Error(w, fmt.Sprintf("Failed to create answer: %v", err), http.StatusInternalServerError)
		return
	}

	// Broadcast answer created/updated event via WebSocket
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

// UpdateAnswerConfirmed updates the confirmed status of an answer
func (h *Handlers) UpdateAnswerConfirmed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch && r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID and answer ID from URL path: /sessions/{id}/answers/{answer_id}/confirm
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

	// Verify session exists
	_, err = h.DB.GetSession(r.Context(), sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Session not found: %v", err), http.StatusNotFound)
		return
	}

	// Get the answer to verify it exists
	answer, err := h.DB.GetAnswerByID(r.Context(), answerID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Answer not found: %v", err), http.StatusNotFound)
		return
	}
	if answer == nil {
		http.Error(w, "Answer not found", http.StatusNotFound)
		return
	}

	// Verify the question belongs to this session
	question, err := h.DB.GetQuestionByID(r.Context(), answer.QuestionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Question not found: %v", err), http.StatusNotFound)
		return
	}
	if question.SessionID != sessionID {
		http.Error(w, "Answer does not belong to this session", http.StatusBadRequest)
		return
	}

	// Only allow confirmation for answers with status "answered"
	if answer.AnswerStatus != models.AnswerStatusAnswered {
		http.Error(w, "Only answers with status 'answered' can be confirmed", http.StatusBadRequest)
		return
	}

	// Parse request body
	type UpdateConfirmedRequest struct {
		Confirmed bool `json:"confirmed"`
	}
	
	var req UpdateConfirmedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	// Update confirmed status
	if err := h.DB.UpdateAnswerConfirmed(r.Context(), answerID, req.Confirmed); err != nil {
		log.Printf("Error updating answer confirmed status: %v", err)
		http.Error(w, fmt.Sprintf("Failed to update confirmed status: %v", err), http.StatusInternalServerError)
		return
	}

	// Get updated answer
	updatedAnswer, err := h.DB.GetAnswerByID(r.Context(), answerID)
	if err != nil || updatedAnswer == nil {
		log.Printf("Warning: Failed to get updated answer: %v", err)
		// Still return success, but use the original answer
		updatedAnswer = answer
		updatedAnswer.Confirmed = req.Confirmed
	}

	// Broadcast via WebSocket
	if h.Hub != nil {
		h.Hub.BroadcastAnswerUpdated(sessionID, updatedAnswer)
	}

	// Return updated answer
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(updatedAnswer)
}

// TranscribeSessionAnswerVoice accepts a short audio recording for an answer and returns a transcription.
func (h *Handlers) TranscribeSessionAnswerVoice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID and question ID from URL path: /sessions/{id}/questions/{question_id}/answers/voice
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

	// Verify session exists
	_, err = h.DB.GetSession(r.Context(), sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Session not found: %v", err), http.StatusNotFound)
		return
	}

	// Verify question exists and belongs to this session
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

	transcriber := utils.NewWhisperCLITranscriberFromEnv()
	if !transcriber.CanTranscribe() {
		http.Error(w, "Speech-to-text is not available (whisper CLI not found). Configure WHISPER_CLI and install openai-whisper.", http.StatusServiceUnavailable)
		return
	}

	text, confidence, err := transcriber.TranscribeAudio(r.Context(), tempFilePath)
	if err != nil {
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

	// Use a stable temp location under the repo to avoid OS temp cleaners/AV races on Windows.
	// This file is still short-lived and deleted via cleanup.
	dir := filepath.Join("data", "tmp", "voice")
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

	// Verify it exists and has some content.
	if info, err := os.Stat(dstPath); err != nil {
		_ = os.Remove(dstPath)
		return "", nil, err
	} else if info.Size() == 0 {
		_ = os.Remove(dstPath)
		return "", nil, fmt.Errorf("uploaded audio file is empty")
	}

	// Use absolute path so downstream tools (whisper) are not sensitive to process working directory.
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

// GetSessionQuestions retrieves questions for a session
func (h *Handlers) GetSessionQuestions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID from URL path
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

	// Verify session exists
	_, err = h.DB.GetSession(r.Context(), sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Session not found: %v", err), http.StatusNotFound)
		return
	}

	// Get questions with answers
	questions, answers, err := h.DB.GetQuestionsBySessionID(r.Context(), sessionID, 20)
	if err != nil {
		log.Printf("Error getting questions: %v", err)
		http.Error(w, fmt.Sprintf("Failed to get questions: %v", err), http.StatusInternalServerError)
		return
	}

	response := GetQuestionsResponse{
		Questions: questions,
		Answers:   answers,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// UpdateSessionStatus updates the status of a session (e.g., close it)
func (h *Handlers) UpdateSessionStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch && r.Method != http.MethodPut {
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

	// Verify session exists
	_, err = h.DB.GetSession(r.Context(), sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Session not found: %v", err), http.StatusNotFound)
		return
	}

	// Parse request body
	type UpdateSessionStatusRequest struct {
		Status string `json:"status"` // "open" or "closed"
	}

	var req UpdateSessionStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	// Validate status
	status := models.SessionStatus(req.Status)
	if status != models.SessionStatusOpen && status != models.SessionStatusClosed {
		http.Error(w, fmt.Sprintf("Invalid status: %s. Must be 'open' or 'closed'", req.Status), http.StatusBadRequest)
		return
	}

	// Update session status
	err = h.DB.UpdateSessionStatus(r.Context(), sessionID, status)
	if err != nil {
		log.Printf("Error updating session status: %v", err)
		http.Error(w, fmt.Sprintf("Failed to update session status: %v", err), http.StatusInternalServerError)
		return
	}

	// Get updated session
	session, err := h.DB.GetSession(r.Context(), sessionID)
	if err != nil {
		log.Printf("Error getting updated session: %v", err)
		http.Error(w, fmt.Sprintf("Failed to get updated session: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(session)
}

// GetSessionTimeline retrieves the timeline for a session (ordered questions and answers)
// This returns questions and answers in chronological order (oldest first) for timeline replay
func (h *Handlers) GetSessionTimeline(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID from URL path (e.g., /sessions/{id}/timeline)
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

	// Verify session exists
	_, err = h.DB.GetSession(r.Context(), sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Session not found: %v", err), http.StatusNotFound)
		return
	}

	// Get limit from query parameter (default 100)
	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsedLimit, err := fmt.Sscanf(limitStr, "%d", &limit); err != nil || parsedLimit != 1 || limit <= 0 {
			limit = 100
		}
	}

	// Get timeline (ordered questions and answers)
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

	// Build timeline entries (questions and answers paired)
	timeline := make([]TimelineEntry, 0, len(questions))
	
	// Create a map of question ID to answer for quick lookup
	answerMap := make(map[uuid.UUID]*models.Answer)
	for _, answer := range answers {
		answerMap[answer.QuestionID] = answer
	}

	// Create timeline entries in chronological order
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
		SessionID string         `json:"session_id"`
		Timeline  []TimelineEntry `json:"timeline"`
		Count     int            `json:"count"`
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
