package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSmoke_AskAnswer_NoContent_ReturnsDeterministicNotCovered covers the deterministic path of
// Flow 5: when an artifact has no indexed content, AskQuestion returns not_covered without
// calling any LLM. This test is fully deterministic even without an OpenAI key.
func TestSmoke_AskAnswer_NoContent_ReturnsDeterministicNotCovered(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	session := createTestSessionForHandlers(t, h.DB, "Q&A Smoke No Content")
	artifact, err := h.DB.CreateArtifact(context.Background(), session.ID, "Empty Artifact", nil)
	require.NoError(t, err)

	body, _ := json.Marshal(AskQuestionRequest{QuestionText: "What is the Meridian proposal?"})
	req := httptest.NewRequest(http.MethodPost, "/artifacts/"+artifact.ID.String()+"/questions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.AskQuestion(w, req)

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var resp AskQuestionResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))

	require.NotNil(t, resp.Question)
	assert.Equal(t, "What is the Meridian proposal?", resp.Question.QuestionText)
	assert.Equal(t, session.ID, resp.Question.SessionID)

	require.NotNil(t, resp.Answer)
	// With no content, the response MUST be not_covered — deterministic without LLM call.
	assert.Equal(t, models.AnswerStatusNotCovered, resp.Answer.AnswerStatus)
	assert.Equal(t, float32(0.0), resp.Answer.Confidence)
	assert.Empty(t, resp.Answer.Citations)
}

// TestSmoke_AskAnswer_WithContent_ValidatesResponseStructure covers Flow 5 with seeded material.
// When an OpenAI key is present, expects "answered"; without one, "error" is acceptable.
// Never asserts exact answer text — only structural validity and optional keyword presence.
func TestSmoke_AskAnswer_WithContent_ValidatesResponseStructure(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	ctx := context.Background()
	session := createTestSessionForHandlers(t, h.DB, "Q&A Smoke With Content")
	artifact, err := h.DB.CreateArtifact(ctx, session.ID, "Meridian Artifact", nil)
	require.NoError(t, err)

	// Seed material with distinctive fixture text (same keywords as smokeDocText).
	material := &models.Material{
		ID:            uuid.New(),
		ArtifactID:    artifact.ID,
		SessionID:     session.ID,
		Kind:          string(models.MaterialKindDocument),
		Filename:      "smoke-report.txt",
		ContentType:   "text/plain",
		StorageURL:    "./smoke-report.txt",
		TextStatus:    models.MaterialTextStatusReady,
		ExtractedText: stringPtr(smokeDocText),
	}
	require.NoError(t, h.DB.CreateMaterial(ctx, material))

	body, _ := json.Marshal(AskQuestionRequest{QuestionText: "What was the APAC expansion budget?"})
	req := httptest.NewRequest(http.MethodPost, "/artifacts/"+artifact.ID.String()+"/questions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.AskQuestion(w, req)

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var resp AskQuestionResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))

	require.NotNil(t, resp.Answer)

	// Structural invariants — must hold regardless of LLM availability.
	assert.Contains(t,
		[]models.AnswerStatus{models.AnswerStatusAnswered, models.AnswerStatusNotCovered, models.AnswerStatusError},
		resp.Answer.AnswerStatus,
	)
	assert.GreaterOrEqual(t, resp.Answer.Confidence, float32(0.0))
	assert.LessOrEqual(t, resp.Answer.Confidence, float32(1.0))

	// When answered: keyword must be traceable to fixture; citation source types must be valid.
	if resp.Answer.AnswerStatus == models.AnswerStatusAnswered {
		assert.NotEmpty(t, resp.Answer.AnswerText)
		lower := strings.ToLower(resp.Answer.AnswerText)
		assert.True(t,
			strings.Contains(lower, "apac") || strings.Contains(lower, "expansion") || strings.Contains(lower, "meridian"),
			"expected answer to reference seeded fixture keywords (got: %q)", resp.Answer.AnswerText,
		)
		for _, c := range resp.Answer.Citations {
			assert.Contains(t, []string{"material", "transcript", "link"}, c.SourceType,
				"citation SourceType must be one of material|transcript|link")
			assert.NotEmpty(t, c.ChunkID, "citation must have a ChunkID")
		}
	}
}

// TestSmoke_AskAnswer_CachedRepeatQuestion verifies that asking the same question twice returns
// the same question ID (deduplication / caching) and a 200 on the second call.
func TestSmoke_AskAnswer_CachedRepeatQuestion(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	session := createTestSessionForHandlers(t, h.DB, "Q&A Smoke Cache")
	artifact, err := h.DB.CreateArtifact(context.Background(), session.ID, "Cache Artifact", nil)
	require.NoError(t, err)

	ask := func() (int, AskQuestionResponse) {
		body, _ := json.Marshal(AskQuestionRequest{QuestionText: "What is the Meridian proposal?"})
		req := httptest.NewRequest(http.MethodPost, "/artifacts/"+artifact.ID.String()+"/questions", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.AskQuestion(w, req)
		var resp AskQuestionResponse
		_ = json.NewDecoder(w.Body).Decode(&resp)
		return w.Code, resp
	}

	code1, resp1 := ask()
	require.Equal(t, http.StatusCreated, code1)
	require.NotNil(t, resp1.Question)

	code2, resp2 := ask()
	// Second call returns 200 (cached) with same question ID.
	require.Equal(t, http.StatusOK, code2)
	require.NotNil(t, resp2.Question)
	assert.Equal(t, resp1.Question.ID, resp2.Question.ID, "repeat question should return the same question ID")
}

// TestSmoke_AskAnswer_GetQuestionsListsPersistedQA verifies that questions and answers are
// retrievable via GetQuestions after being directly inserted through the DB layer.
// This tests persistence and retrieval without depending on LLM availability.
func TestSmoke_AskAnswer_GetQuestionsListsPersistedQA(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	ctx := context.Background()
	session := createTestSessionForHandlers(t, h.DB, "Q&A Smoke Persist")
	artifact, err := h.DB.CreateArtifact(ctx, session.ID, "Persist Artifact", nil)
	require.NoError(t, err)

	// Insert question and answer directly — bypasses LLM entirely.
	q := &models.Question{
		ID:             uuid.New(),
		ArtifactID:     artifact.ID,
		SessionID:      session.ID,
		QuestionText:   "What is the churn target?",
		QuestionSource: models.QuestionSourceText,
	}
	require.NoError(t, h.DB.CreateQuestion(ctx, q))

	a := &models.Answer{
		ID:           uuid.New(),
		QuestionID:   q.ID,
		AnswerText:   "Churn target is below 6% per the Meridian report.",
		AnswerStatus: models.AnswerStatusAnswered,
		Confidence:   0.91,
		Citations:    []models.Citation{{SourceType: "material", ChunkID: "chunk-smoke-001"}},
	}
	require.NoError(t, h.DB.CreateAnswer(ctx, a))

	// Retrieve via handler.
	req := httptest.NewRequest(http.MethodGet, "/artifacts/"+artifact.ID.String()+"/questions", nil)
	w := httptest.NewRecorder()
	h.GetQuestions(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp GetQuestionsResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))

	require.NotEmpty(t, resp.Questions)
	require.NotEmpty(t, resp.Answers)

	// Structural assertions — never assert exact text.
	ans := resp.Answers[0]
	assert.Contains(t,
		[]models.AnswerStatus{models.AnswerStatusAnswered, models.AnswerStatusNotCovered, models.AnswerStatusError},
		ans.AnswerStatus,
	)
	assert.GreaterOrEqual(t, ans.Confidence, float32(0.0))
	assert.LessOrEqual(t, ans.Confidence, float32(1.0))
	assert.NotEmpty(t, ans.AnswerText)

	for _, c := range ans.Citations {
		assert.Contains(t, []string{"material", "transcript", "link"}, c.SourceType)
	}
}
