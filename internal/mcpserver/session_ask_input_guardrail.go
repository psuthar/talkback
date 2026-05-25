// SCRUM-564 (Slice 3 of SCRUM-560): MCP-side helper for the input
// guardrail refusal path on ask_session / ask_session_question. The
// HTTP-side equivalent lives in
// internal/handlers/session_ask_input_guardrail.go and uses a sibling
// helper (writeInputGuardrailRefusal). Both pathways agree on the wire
// shape (docs/guardrails/refusal-shape.md) and the llm_call_log row
// shape (docs/guardrails/log-shape.md).
//
// Per refusal-shape.md § Transport: the MCP return is *not* an
// MCP-protocol error — those signal "the tool itself failed", which it
// didn't (it refused on purpose). Instead the refusal JSON is returned
// as tool-result text content.
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/psuthar/talkback/internal/guardrails"
)

// mcpInputGuardrailRefusal builds the MCP tool-result body for an
// input-guardrail refusal and records the decision in llm_call_log.
// Returns the same three-tuple shape as the surrounding tool handler so
// the call site can return its result directly.
//
// LogLLMCall fires synchronously into the in-memory buffer; the
// background flusher persists it within ~5s. UserID and SessionID are
// picked up from ctx (the tool handler stamps them earlier via
// guardrails.WithUserID / WithSessionID).
func mcpInputGuardrailRefusal(ctx context.Context, questionText string, decision guardrails.InputDecision) (*mcp.CallToolResult, askSessionQuestionOutput, error) {
	refusal := decision.Refusal()
	raw, err := json.Marshal(refusal)
	if err != nil {
		// Marshal failure on a four-field struct is a programmer error;
		// surface a protocol error so it doesn't get swallowed.
		return nil, askSessionQuestionOutput{}, fmt.Errorf("marshal guardrail refusal: %w", err)
	}

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

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(raw)},
		},
	}, askSessionQuestionOutput{}, nil
}
