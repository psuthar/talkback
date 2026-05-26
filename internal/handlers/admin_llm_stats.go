package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/guardrails"
)

// AdminLLMStatsPayload is the JSON shape returned by GET /api/admin/llm-stats.
// Per docs/guardrails/log-shape.md "Read patterns" — single endpoint that
// rolls up everything the operator needs from a 7-day window.
type AdminLLMStatsPayload struct {
	DaysWindow       int                                `json:"days_window"`
	Since            string                             `json:"since"`
	TotalCalls       int                                `json:"total_calls"`
	ByDecision       map[string]int                     `json:"by_decision"`
	BySite           map[string]int                     `json:"by_site"`
	TopRefusalCodes  []database.LLMCallRefusalCodeCount `json:"top_refusal_codes"`
	P95LatencyMS     *float64                           `json:"p95_latency_ms"`
	DroppedTelemetry uint64                             `json:"dropped_telemetry_rows"`

	// SCRUM-578 (Slice 1 of SCRUM-577): cost-rollup extension. Token
	// totals are nullable when no rows / all-null tokens (obsworker
	// site doesn't emit token counts). by_model is an empty map when
	// the window has no token-bearing calls — the UI renders the
	// Models table only when this is non-empty.
	TotalInputTokens  *int64         `json:"total_input_tokens"`
	TotalOutputTokens *int64         `json:"total_output_tokens"`
	ByModel           map[string]int `json:"by_model"`
}

// AdminLLMStatsHandler handles GET /api/admin/llm-stats?days=7
// (RequireAuth + RequireAdmin). SCRUM-568 / Slice 5 of SCRUM-560.
//
// Returns the seven-day rollup over the llm_call_log table. `days` is
// clamped to [1, 30] — anything older is out of typical operational
// interest and the indexes are tuned for short windows.
func (h *Handlers) AdminLLMStatsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	ctx := r.Context()

	days := 7
	if raw := r.URL.Query().Get("days"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			if n > 30 {
				n = 30
			}
			days = n
		}
	}
	since := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)

	total, err := h.DB.CountLLMCalls(ctx, since)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "rollup failed: total"})
		return
	}
	byDecision, err := h.DB.CountLLMCallsByDecision(ctx, since)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "rollup failed: by_decision"})
		return
	}
	bySite, err := h.DB.CountLLMCallsBySite(ctx, since)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "rollup failed: by_site"})
		return
	}
	topRefusals, err := h.DB.TopLLMCallRefusalCodes(ctx, since, 3)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "rollup failed: top_refusal_codes"})
		return
	}
	p95, err := h.DB.P95LLMCallLatencyMS(ctx, since)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "rollup failed: p95_latency_ms"})
		return
	}
	// SCRUM-578: cost rollup (token totals + per-model counts).
	inputTokens, outputTokens, err := h.DB.SumLLMCallTokens(ctx, since)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "rollup failed: tokens"})
		return
	}
	byModel, err := h.DB.CountLLMCallsByModel(ctx, since)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "rollup failed: by_model"})
		return
	}

	writeJSON(w, http.StatusOK, AdminLLMStatsPayload{
		DaysWindow:        days,
		Since:             since.Format(time.RFC3339),
		TotalCalls:        total,
		ByDecision:        byDecision,
		BySite:            bySite,
		TopRefusalCodes:   topRefusals,
		P95LatencyMS:      p95,
		DroppedTelemetry:  guardrails.DroppedCount(),
		TotalInputTokens:  inputTokens,
		TotalOutputTokens: outputTokens,
		ByModel:           byModel,
	})
}
