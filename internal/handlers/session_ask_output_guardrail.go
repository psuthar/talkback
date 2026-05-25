// SCRUM-565 (Slice 4a of SCRUM-560): HTTP-side helper for the output
// guardrail refusal path on POST /api/sessions/:id/ask. The MCP-side
// equivalent lives in internal/mcpserver/session_ask_output_guardrail.go.
// Both pathways agree on the wire shape (docs/guardrails/refusal-shape.md).
//
// Unlike writeInputGuardrailRefusal (Slice 3), this helper does **not**
// emit a LogLLMCall row — the refusal decision is logged at the call
// site in internal/utils/qa.go where the LLM rounds happen, so this
// helper is pure transport.
package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/psuthar/talkback/internal/guardrails"
)

// writeOutputGuardrailRefusal emits the structured refusal body on the
// HTTP response. Status code is 200 OK per refusal-shape.md § Transport
// (refusal is a deliberate product response, not a request error).
func writeOutputGuardrailRefusal(w http.ResponseWriter, refusal guardrails.RefusalShape) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(refusal)
}
