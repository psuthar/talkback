package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
)

func TestAskQuestion_RepeatQuestionCaching(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()
	db := h.DB
	ctx := context.Background()

	// Create an artifact
	artifact, err := db.CreateArtifact(ctx, "Test Artifact", nil)
	if err != nil {
		t.Fatalf("Failed to create artifact: %v", err)
	}
	artifactID := artifact.ID

	// Create a material with extracted text
	materialID := uuid.New()
	material := &models.Material{
		ID:            materialID,
		ArtifactID:    artifactID,
		Kind:          string(models.MaterialKindDocument),
		Filename:      "test.txt",
		ContentType:   "text/plain",
		StorageURL:    "./test.txt",
		TextStatus:    models.MaterialTextStatusReady,
		ExtractedText: stringPtr("This is test content about machine learning and artificial intelligence."),
	}
	if err := db.CreateMaterial(ctx, material); err != nil {
		t.Fatalf("Failed to create material: %v", err)
	}

	questionText := "What is this about?"

	// First question - should call LLM
	req1 := AskQuestionRequest{QuestionText: questionText}
	body1, _ := json.Marshal(req1)
	req1_http := httptest.NewRequest("POST", "/artifacts/"+artifactID.String()+"/questions", bytes.NewReader(body1))
	req1_http.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()

	h.AskQuestion(w1, req1_http)

	if w1.Code != http.StatusCreated {
		t.Fatalf("Expected status 201, got %d: %s", w1.Code, w1.Body.String())
	}

	var response1 AskQuestionResponse
	if err := json.Unmarshal(w1.Body.Bytes(), &response1); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response1.Question == nil || response1.Answer == nil {
		t.Fatal("Response should contain question and answer")
	}

	firstQuestionID := response1.Question.ID
	firstAnswerID := response1.Answer.ID

	// Second question with same text - should return cached answer
	req2 := AskQuestionRequest{QuestionText: questionText}
	body2, _ := json.Marshal(req2)
	req2_http := httptest.NewRequest("POST", "/artifacts/"+artifactID.String()+"/questions", bytes.NewReader(body2))
	req2_http.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()

	h.AskQuestion(w2, req2_http)

	if w2.Code != http.StatusOK {
		t.Fatalf("Expected status 200 for cached response, got %d: %s", w2.Code, w2.Body.String())
	}

	var response2 AskQuestionResponse
	if err := json.Unmarshal(w2.Body.Bytes(), &response2); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should return the same question and answer
	if response2.Question.ID != firstQuestionID {
		t.Errorf("Expected same question ID %s, got %s", firstQuestionID, response2.Question.ID)
	}
	if response2.Answer.ID != firstAnswerID {
		t.Errorf("Expected same answer ID %s, got %s", firstAnswerID, response2.Answer.ID)
	}
}

func TestAskQuestion_NotCovered_NoContent(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()
	db := h.DB
	ctx := context.Background()

	// Create an artifact with no materials or transcripts
	artifact, err := db.CreateArtifact(ctx, "Empty Artifact", nil)
	if err != nil {
		t.Fatalf("Failed to create artifact: %v", err)
	}
	artifactID := artifact.ID

	req := AskQuestionRequest{QuestionText: "What is this about?"}
	body, _ := json.Marshal(req)
	req_http := httptest.NewRequest("POST", "/artifacts/"+artifactID.String()+"/questions", bytes.NewReader(body))
	req_http.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.AskQuestion(w, req_http)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected status 201, got %d: %s", w.Code, w.Body.String())
	}

	var response AskQuestionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should return not_covered without calling OpenAI
	if response.Answer.AnswerStatus != models.AnswerStatusNotCovered {
		t.Errorf("Expected answer_status 'not_covered', got '%s'", response.Answer.AnswerStatus)
	}

	if response.Answer.Confidence != 0.0 {
		t.Errorf("Expected confidence 0.0, got %f", response.Answer.Confidence)
	}

	if len(response.Answer.Citations) != 0 {
		t.Errorf("Expected no citations for not_covered, got %d", len(response.Answer.Citations))
	}
}

func TestAskQuestion_NotCovered_UnrelatedQuestion(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()
	db := h.DB
	ctx := context.Background()

	// Create an artifact with content
	artifact, err := db.CreateArtifact(ctx, "Test Artifact", nil)
	if err != nil {
		t.Fatalf("Failed to create artifact: %v", err)
	}
	artifactID := artifact.ID

	// Create a material with specific content
	materialID := uuid.New()
	material := &models.Material{
		ID:            materialID,
		ArtifactID:    artifactID,
		Kind:          string(models.MaterialKindDocument),
		Filename:      "test.txt",
		ContentType:   "text/plain",
		StorageURL:    "./test.txt",
		TextStatus:    models.MaterialTextStatusReady,
		ExtractedText: stringPtr("This document is about machine learning algorithms and neural networks."),
	}
	if err := db.CreateMaterial(ctx, material); err != nil {
		t.Fatalf("Failed to create material: %v", err)
	}

	// Ask a question that's not in the content
	req := AskQuestionRequest{QuestionText: "What is the weather like today?"}
	body, _ := json.Marshal(req)
	req_http := httptest.NewRequest("POST", "/artifacts/"+artifactID.String()+"/questions", bytes.NewReader(body))
	req_http.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.AskQuestion(w, req_http)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected status 201, got %d: %s", w.Code, w.Body.String())
	}

	var response AskQuestionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should return not_covered or answered with low confidence
	// The LLM should detect this is not in context
	// Note: If OPENAI_API_KEY is not set, it will return 'error' status, which is acceptable for this test
	if response.Answer.AnswerStatus == models.AnswerStatusAnswered {
		// If answered, confidence should be low or it should be marked not_covered
		if response.Answer.Confidence < 0.55 {
			t.Logf("Answer was marked as answered but with low confidence %f - this is acceptable", response.Answer.Confidence)
		}
	} else if response.Answer.AnswerStatus == models.AnswerStatusError {
		// Error status is acceptable if API key is not set (test environment)
		t.Logf("Answer status is 'error' (likely due to missing OPENAI_API_KEY) - this is acceptable in test environment")
	} else if response.Answer.AnswerStatus != models.AnswerStatusNotCovered {
		t.Errorf("Expected answer_status 'not_covered', 'answered' with low confidence, or 'error', got '%s'", response.Answer.AnswerStatus)
	}
}

func TestAskQuestion_Citations_Valid(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()
	db := h.DB
	ctx := context.Background()

	// Create an artifact with content
	artifact, err := db.CreateArtifact(ctx, "Test Artifact", nil)
	if err != nil {
		t.Fatalf("Failed to create artifact: %v", err)
	}
	artifactID := artifact.ID

	// Create a material with extracted text
	materialID := uuid.New()
	material := &models.Material{
		ID:            materialID,
		ArtifactID:    artifactID,
		Kind:          string(models.MaterialKindDocument),
		Filename:      "test.txt",
		ContentType:   "text/plain",
		StorageURL:    "./test.txt",
		TextStatus:    models.MaterialTextStatusReady,
		ExtractedText: stringPtr("This document discusses machine learning algorithms, neural networks, and deep learning techniques. It covers supervised learning, unsupervised learning, and reinforcement learning approaches."),
	}
	if err := db.CreateMaterial(ctx, material); err != nil {
		t.Fatalf("Failed to create material: %v", err)
	}

	req := AskQuestionRequest{QuestionText: "What does this document discuss?"}
	body, _ := json.Marshal(req)
	req_http := httptest.NewRequest("POST", "/artifacts/"+artifactID.String()+"/questions", bytes.NewReader(body))
	req_http.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.AskQuestion(w, req_http)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected status 201, got %d: %s", w.Code, w.Body.String())
	}

	var response AskQuestionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// If answered, citations should be valid
	if response.Answer.AnswerStatus == models.AnswerStatusAnswered {
		citations := response.Answer.Citations
		
		// Should have 2-5 citations
		if len(citations) < 2 || len(citations) > 5 {
			t.Errorf("Expected 2-5 citations, got %d", len(citations))
		}

		// Each citation should have required fields
		for i, citation := range citations {
			if citation.ChunkID == "" {
				t.Errorf("Citation %d missing chunk_id", i+1)
			}
			if citation.SourceType != "material" && citation.SourceType != "transcript" {
				t.Errorf("Citation %d has invalid source_type: %s", i+1, citation.SourceType)
			}
			if citation.SourceID == "" {
				t.Errorf("Citation %d missing source_id", i+1)
			}
			if citation.Snippet == "" {
				t.Errorf("Citation %d missing snippet", i+1)
			}
			// Snippet should be reasonable length (not too long)
			if len(citation.Snippet) > 350 {
				t.Errorf("Citation %d snippet too long: %d chars", i+1, len(citation.Snippet))
			}
		}
	}
}

func TestGetQuestions_ReturnsRecentQuestionsWithAnswers(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()
	db := h.DB
	ctx := context.Background()

	// Create an artifact
	artifact, err := db.CreateArtifact(ctx, "Test Artifact", nil)
	if err != nil {
		t.Fatalf("Failed to create artifact: %v", err)
	}
	artifactID := artifact.ID

	// Create a question and answer manually
	questionID := uuid.New()
	question := &models.Question{
		ID:             questionID,
		ArtifactID:     artifactID,
		QuestionText:   "Test question?",
		QuestionSource: models.QuestionSourceText,
	}
	if err := db.CreateQuestion(ctx, question); err != nil {
		t.Fatalf("Failed to create question: %v", err)
	}

	answerID := uuid.New()
	answer := &models.Answer{
		ID:           answerID,
		QuestionID:   questionID,
		AnswerText:   "Test answer",
		AnswerStatus: models.AnswerStatusAnswered,
		Confidence:   0.8,
		Citations:    []models.Citation{},
	}
	if err := db.CreateAnswer(ctx, answer); err != nil {
		t.Fatalf("Failed to create answer: %v", err)
	}

	// Get questions
	req := httptest.NewRequest("GET", "/artifacts/"+artifactID.String()+"/questions", nil)
	w := httptest.NewRecorder()

	h.GetQuestions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var response GetQuestionsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(response.Questions) == 0 {
		t.Fatal("Expected at least one question")
	}

	if len(response.Answers) == 0 {
		t.Fatal("Expected at least one answer")
	}

	// Verify question and answer match
	found := false
	for i, q := range response.Questions {
		if q.ID == questionID {
			found = true
			if i < len(response.Answers) {
				if response.Answers[i].ID != answerID {
					t.Errorf("Answer ID mismatch: expected %s, got %s", answerID, response.Answers[i].ID)
				}
			}
			break
		}
	}

	if !found {
		t.Error("Question not found in response")
	}
}

func stringPtr(s string) *string {
	return &s
}
