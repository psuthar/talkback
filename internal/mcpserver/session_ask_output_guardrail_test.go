// SCRUM-565 (Slice 4a of SCRUM-560): MCP-side propagation test for
// the output guardrail refusal. The full retry-refuse path lives in
// internal/utils/qa.go and is covered end-to-end with a mocked OpenAI
// in internal/utils/qa_citation_integration_test.go. Here we verify
// that mcpOutputGuardrailRefusal produces the exact shape per
// docs/guardrails/refusal-shape.md so MCP clients see the same JSON
// as HTTP clients.
package mcpserver

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/psuthar/talkback/internal/guardrails"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMcpOutputGuardrailRefusal_EmitsRefusalShape(t *testing.T) {
	t.Parallel()
	refusal := guardrails.RefusalShape{
		Error:       "guardrail_blocked",
		Guardrail:   "citation_missing",
		Code:        "citation_missing",
		UserMessage: guardrails.UserMessageCitationMissing,
	}
	res, err := mcpOutputGuardrailRefusal(refusal)
	require.NoError(t, err)
	require.NotNil(t, res)
	// Per refusal-shape.md § Transport: refusal is a deliberate product
	// response, NOT a protocol error.
	assert.False(t, res.IsError,
		"MCP refusal must not set IsError per refusal-shape.md § Transport")
	require.Len(t, res.Content, 1, "expect exactly one tool-result content block")

	tc, ok := res.Content[0].(*mcp.TextContent)
	require.True(t, ok, "want TextContent, got %T", res.Content[0])

	var body guardrails.RefusalShape
	require.NoError(t, json.Unmarshal([]byte(tc.Text), &body))
	assert.Equal(t, "guardrail_blocked", body.Error)
	assert.Equal(t, "citation_missing", body.Guardrail)
	assert.Equal(t, "citation_missing", body.Code)
	assert.Equal(t, guardrails.UserMessageCitationMissing, body.UserMessage,
		"user_message is contract-locked verbatim in refusal-shape.md")
}

func TestMcpOutputGuardrailRefusal_GroundingFailedShape(t *testing.T) {
	// SCRUM-566 (Slice 4b) will populate grounding_failed via the same
	// helper. Smoke-test the future slug now so the contract doesn't
	// silently regress when 4b lands.
	t.Parallel()
	refusal := guardrails.RefusalShape{
		Error:       "guardrail_blocked",
		Guardrail:   "grounding_failed",
		Code:        "grounding_failed",
		UserMessage: guardrails.UserMessageGroundingFailed,
	}
	res, err := mcpOutputGuardrailRefusal(refusal)
	require.NoError(t, err)
	tc, ok := res.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	var body guardrails.RefusalShape
	require.NoError(t, json.Unmarshal([]byte(tc.Text), &body))
	assert.Equal(t, "grounding_failed", body.Guardrail)
}
