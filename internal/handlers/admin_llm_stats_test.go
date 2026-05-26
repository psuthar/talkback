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

// SCRUM-578 (Slice 1 of SCRUM-577): cost-rollup extension tests.
// Verifies the new payload fields (TotalInputTokens, TotalOutputTokens,
// ByModel) populate correctly across the three meaningful scenarios:
//   - empty window: both totals nil, by_model empty.
//   - mixed-model window: totals SUM correctly, by_model has per-model
//     counts.
//   - refusal rows (model=""): excluded from by_model; contribute null
//     tokens to totals without breaking the SUM.

func TestAdminLLMStats_GET_CostRollup_EmptyWindow(t *testing.T) {
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

	// Empty window → both totals null, by_model empty map (not nil).
	assert.Nil(t, payload.TotalInputTokens, "no rows → nil totals")
	assert.Nil(t, payload.TotalOutputTokens)
	assert.Empty(t, payload.ByModel, "no rows → empty by_model")
	// The map must serialize as `{}`, not `null`, so the JS consumer
	// can iterate without a nullity guard. Re-encode to confirm.
	raw, err := json.Marshal(payload.ByModel)
	require.NoError(t, err)
	assert.Equal(t, "{}", string(raw), "by_model serializes as object, not null")
}

func TestAdminLLMStats_GET_CostRollup_MixedModels(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	defer truncateLLMCallLogHandler(t, h)
	ctx := context.Background()

	// 4 gpt-4o-mini rows (mixed input/output tokens) + 2 gpt-4o rows.
	// Token totals: input=400 (50*4 + 100*2), output=200 (25*4 + 50*2).
	mk := func(model string, in, out int) guardrails.LLMCallRow {
		return guardrails.LLMCallRow{
			ID:              uuid.New(),
			TS:              time.Now().UTC(),
			Site:            "qa_ask",
			Model:           model,
			PromptHash:      guardrails.HashPrompt("qa_ask", "x"),
			LatencyMS:       100,
			Decision:        "allowed",
			GuardrailsFired: []string{},
			InputTokens:     guardrails.IntPtr(in),
			OutputTokens:    guardrails.IntPtr(out),
		}
	}
	rows := []guardrails.LLMCallRow{
		mk("gpt-4o-mini", 50, 25),
		mk("gpt-4o-mini", 50, 25),
		mk("gpt-4o-mini", 50, 25),
		mk("gpt-4o-mini", 50, 25),
		mk("gpt-4o", 100, 50),
		mk("gpt-4o", 100, 50),
	}
	for _, row := range rows {
		require.NoError(t, h.DB.InsertLLMCallRow(ctx, row))
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm-stats?days=7", nil)
	addAdminSessionCookie(t, h, req)
	w := httptest.NewRecorder()
	h.RequireAuth(h.RequireAdmin(h.AdminLLMStatsHandler))(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var payload AdminLLMStatsPayload
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))

	require.NotNil(t, payload.TotalInputTokens)
	require.NotNil(t, payload.TotalOutputTokens)
	assert.Equal(t, int64(400), *payload.TotalInputTokens, "4*50 + 2*100")
	assert.Equal(t, int64(200), *payload.TotalOutputTokens, "4*25 + 2*50")

	assert.Equal(t, 4, payload.ByModel["gpt-4o-mini"])
	assert.Equal(t, 2, payload.ByModel["gpt-4o"])
}

func TestAdminLLMStats_GET_CostRollup_ExcludesEmptyModel(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	defer truncateLLMCallLogHandler(t, h)
	ctx := context.Background()

	// 2 normal qa_ask rows with tokens + 3 refusal rows (model="",
	// no tokens — the qa.go refusal paths emit these for input_injection
	// / input_off_scope / citation_missing / etc.).
	allowedRow := func() guardrails.LLMCallRow {
		return guardrails.LLMCallRow{
			ID:              uuid.New(),
			TS:              time.Now().UTC(),
			Site:            "qa_ask",
			Model:           "gpt-4o-mini",
			PromptHash:      guardrails.HashPrompt("qa_ask", "x"),
			LatencyMS:       100,
			Decision:        "allowed",
			GuardrailsFired: []string{},
			InputTokens:     guardrails.IntPtr(60),
			OutputTokens:    guardrails.IntPtr(30),
		}
	}
	refusalRow := func(code string) guardrails.LLMCallRow {
		return guardrails.LLMCallRow{
			ID:              uuid.New(),
			TS:              time.Now().UTC(),
			Site:            "qa_ask",
			Model:           "", // refusal — no LLM was invoked
			PromptHash:      guardrails.HashPrompt("qa_ask", "x"),
			LatencyMS:       0,
			Decision:        "refused",
			RefusalCode:     guardrails.StrPtr(code),
			GuardrailsFired: []string{code},
			// InputTokens / OutputTokens deliberately nil.
		}
	}
	rows := []guardrails.LLMCallRow{
		allowedRow(),
		allowedRow(),
		refusalRow("input_injection"),
		refusalRow("input_off_scope"),
		refusalRow("citation_missing"),
	}
	for _, row := range rows {
		require.NoError(t, h.DB.InsertLLMCallRow(ctx, row))
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm-stats?days=7", nil)
	addAdminSessionCookie(t, h, req)
	w := httptest.NewRecorder()
	h.RequireAuth(h.RequireAdmin(h.AdminLLMStatsHandler))(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var payload AdminLLMStatsPayload
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))

	// by_model excludes refusal rows: only gpt-4o-mini should be present.
	assert.Equal(t, 2, payload.ByModel["gpt-4o-mini"])
	_, hasEmpty := payload.ByModel[""]
	assert.False(t, hasEmpty, "by_model must not include empty-string model")
	assert.Len(t, payload.ByModel, 1, "exactly one model in by_model")

	// Totals: only the 2 allowed rows contributed tokens; refusal rows
	// had nil token columns and SUM ignores NULL inputs in PostgreSQL.
	require.NotNil(t, payload.TotalInputTokens)
	require.NotNil(t, payload.TotalOutputTokens)
	assert.Equal(t, int64(120), *payload.TotalInputTokens, "2*60 from allowed rows")
	assert.Equal(t, int64(60), *payload.TotalOutputTokens, "2*30 from allowed rows")

	// And the existing rollup still reflects ALL rows.
	assert.Equal(t, 5, payload.TotalCalls)
	assert.Equal(t, 5, payload.BySite["qa_ask"])
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
