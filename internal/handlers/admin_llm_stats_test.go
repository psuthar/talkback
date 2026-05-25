package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/guardrails"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SCRUM-568: admin /api/admin/llm-stats handler — auth gating + rollup shape.

func truncateLLMCallLogHandler(t *testing.T, h *Handlers) {
	t.Helper()
	_, err := h.DB.Pool.Exec(context.Background(), "TRUNCATE TABLE llm_call_log")
	require.NoError(t, err)
}

func TestAdminLLMStats_GET_Unauthorized(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm-stats", nil)
	w := httptest.NewRecorder()
	h.RequireAuth(h.RequireAdmin(h.AdminLLMStatsHandler))(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAdminLLMStats_GET_ForbiddenWhenNotAdmin(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm-stats", nil)
	addUserSessionCookie(t, h, req, "user@example.com")
	w := httptest.NewRecorder()
	h.RequireAuth(h.RequireAdmin(h.AdminLLMStatsHandler))(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestAdminLLMStats_GET_MethodNotAllowed(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	defer truncateLLMCallLogHandler(t, h)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm-stats", nil)
	addAdminSessionCookie(t, h, req)
	w := httptest.NewRecorder()
	h.RequireAuth(h.RequireAdmin(h.AdminLLMStatsHandler))(w, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestAdminLLMStats_GET_EmptyWindow(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	defer truncateLLMCallLogHandler(t, h)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm-stats?days=7", nil)
	addAdminSessionCookie(t, h, req)
	w := httptest.NewRecorder()
	h.RequireAuth(h.RequireAdmin(h.AdminLLMStatsHandler))(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var payload AdminLLMStatsPayload
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	assert.Equal(t, 7, payload.DaysWindow)
	assert.Equal(t, 0, payload.TotalCalls)
	assert.Empty(t, payload.ByDecision)
	assert.Empty(t, payload.BySite)
	assert.Empty(t, payload.TopRefusalCodes)
	assert.Nil(t, payload.P95LatencyMS)
	assert.NotEmpty(t, payload.Since)
}

func TestAdminLLMStats_GET_RollupShapeWithMixedData(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	defer truncateLLMCallLogHandler(t, h)
	ctx := context.Background()

	// 3 allowed qa_ask, 2 refused qa_ask with input_injection, 1 refused
	// qa_ask with citation_missing, 1 allowed action_items.
	mk := func(site, decision string, refusal *string, latencyMS int) guardrails.LLMCallRow {
		row := guardrails.LLMCallRow{
			ID:         uuid.New(),
			TS:         time.Now().UTC(),
			Site:       site,
			Model:      "gpt-4o-mini",
			PromptHash: guardrails.HashPrompt(site, "x"),
			LatencyMS:  latencyMS,
			Decision:   decision,
		}
		if refusal != nil {
			row.RefusalCode = refusal
			row.GuardrailsFired = []string{*refusal}
		} else {
			row.GuardrailsFired = []string{}
		}
		return row
	}
	rows := []guardrails.LLMCallRow{
		mk("qa_ask", "allowed", nil, 100),
		mk("qa_ask", "allowed", nil, 110),
		mk("qa_ask", "allowed", nil, 120),
		mk("qa_ask", "refused", guardrails.StrPtr("input_injection"), 50),
		mk("qa_ask", "refused", guardrails.StrPtr("input_injection"), 55),
		mk("qa_ask", "refused", guardrails.StrPtr("citation_missing"), 90),
		mk("action_items", "allowed", nil, 200),
	}
	for _, row := range rows {
		require.NoError(t, h.DB.InsertLLMCallRow(ctx, row))
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm-stats", nil)
	addAdminSessionCookie(t, h, req)
	w := httptest.NewRecorder()
	h.RequireAuth(h.RequireAdmin(h.AdminLLMStatsHandler))(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var payload AdminLLMStatsPayload
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))

	assert.Equal(t, 7, payload.DaysWindow, "default days=7")
	assert.Equal(t, len(rows), payload.TotalCalls)
	assert.Equal(t, 4, payload.ByDecision["allowed"])
	assert.Equal(t, 3, payload.ByDecision["refused"])
	assert.Equal(t, 6, payload.BySite["qa_ask"])
	assert.Equal(t, 1, payload.BySite["action_items"])

	require.GreaterOrEqual(t, len(payload.TopRefusalCodes), 2)
	assert.Equal(t, "input_injection", payload.TopRefusalCodes[0].Code)
	assert.Equal(t, 2, payload.TopRefusalCodes[0].Count)
	assert.Equal(t, "citation_missing", payload.TopRefusalCodes[1].Code)
	assert.Equal(t, 1, payload.TopRefusalCodes[1].Count)

	require.NotNil(t, payload.P95LatencyMS)
	assert.Greater(t, *payload.P95LatencyMS, 0.0)
}

func TestAdminLLMStats_GET_DaysClampedTo30(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	defer truncateLLMCallLogHandler(t, h)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm-stats?days=999", nil)
	addAdminSessionCookie(t, h, req)
	w := httptest.NewRecorder()
	h.RequireAuth(h.RequireAdmin(h.AdminLLMStatsHandler))(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var payload AdminLLMStatsPayload
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	assert.Equal(t, 30, payload.DaysWindow, "days clamped to 30 (>30 input)")
}
