// SCRUM-565 (Slice 4a of SCRUM-560): MCP-side helper for the output
// guardrail refusal path on ask_session / ask_session_question. The
// HTTP-side equivalent lives in
// internal/handlers/session_ask_output_guardrail.go.
//
// Per refusal-shape.md § Transport: the MCP return is *not* an MCP-
// protocol error — those signal "the tool itself failed", which it
// didn't (it refused on purpose). The refusal JSON is returned as
// tool-result text content with IsError unset.
//
// Unlike mcpInputGuardrailRefusal (Slice 3), this helper does **not**
// emit a LogLLMCall row — the refusal decision is already logged at
// the call site in internal/utils/qa.go where the LLM rounds happen,
// so this helper is pure transport.
package mcpserver

import (
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/psuthar/talkback/internal/guardrails"
)

// mcpOutputGuardrailRefusal builds the MCP tool-result body for an
// output-guardrail refusal. Returns the *mcp.CallToolResult to plug
// into the surrounding tool handler's return tuple.
func mcpOutputGuardrailRefusal(refusal guardrails.RefusalShape) (*mcp.CallToolResult, error) {
	raw, err := json.Marshal(refusal)
	if err != nil {
		// Marshal failure on a four-field struct is a programmer error;
		// surface a protocol error so it doesn't get swallowed.
		return nil, fmt.Errorf("marshal guardrail refusal: %w", err)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(raw)},
		},
	}, nil
}
