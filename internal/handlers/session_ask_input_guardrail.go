// SCRUM-564 (Slice 3 of SCRUM-560): HTTP-side helper for the input
// guardrail refusal path on POST /api/sessions/:id/ask. The MCP-side
// equivalent lives in internal/mcpserver/session_ask_question.go and
// uses a sibling helper (mcpInputGuardrailRefusal). Both pathways
// agree on the wire shape (docs/guardrails/refusal-shape.md) and the
// llm_call_log row shape (docs/guardrails/log-shape.md).
package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/psuthar/talkback/internal/guardrails"
)

// writeInputGuardrailRefusal emits the structured refusal body on the
// HTTP response and records the decision in llm_call_log. The status
// code is 200 OK on purpose — refusal is a deliberate product response,
// not a request error. Clients branch on the top-level `error` field.
//
// LogLLMCall fires synchronously into the in-memory buffer; the
// background flusher persists it within ~5s. UserID and SessionID are
// picked up from ctx (the handler stamps them earlier via
// guardrails.WithUserID / WithSessionID).
func writeInputGuardrailRefusal(ctx context.Context, w http.ResponseWriter, questionText string, decision guardrails.InputDecision) {
	refusal := decision.Refusal()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(refusal)

	guardrails.LogLLMCall(ctx, guardrails.LLMCallRow{
		Site:               "qa_ask",
		Model:              "", // no LLM was invoked
		PromptHash:         guardrails.HashPrompt("qa_ask", questionText),
		LatencyMS:          0, // pattern-match only; no upstream call
		GuardrailsFired:    []string{decision.Guardrail},
		Decision:           "refused",
		RefusalCode:        guardrails.StrPtr(decision.Code),
		RefusalUserMessage: guardrails.StrPtr(decision.UserMessage),
	})
}
