// SCRUM-581: client-side helper for the guardrail-refusal contract documented in
// docs/guardrails/refusal-shape.md.
//
// The /api/sessions/:id/ask endpoint returns HTTP 200 with the refusal body when an
// input or output guardrail blocks the call. The body discriminator is the literal
// string `error === "guardrail_blocked"`. When that discriminator is set the body has
// no `question` / `answer` fields, so callers must branch on this helper before
// reading downstream success-shape fields.

const REFUSAL_DISCRIMINATOR = 'guardrail_blocked'
const FALLBACK_USER_MESSAGE = 'Question was blocked by a safety guardrail.'

// extractGuardrailRefusal returns { isRefusal, message } for a parsed ask-endpoint
// response body. When isRefusal is true, message is the verbatim user_message from
// the contract (falling back to a generic string only if the server omitted it,
// which would be a contract violation — see refusal-shape.md "All four fields are
// required and non-empty").
export function extractGuardrailRefusal(data) {
  if (!data || typeof data !== 'object') {
    return { isRefusal: false }
  }
  if (data.error !== REFUSAL_DISCRIMINATOR) {
    return { isRefusal: false }
  }
  const userMessage = typeof data.user_message === 'string' && data.user_message.length > 0
    ? data.user_message
    : FALLBACK_USER_MESSAGE
  return {
    isRefusal: true,
    message: userMessage,
    guardrail: typeof data.guardrail === 'string' ? data.guardrail : null,
    code: typeof data.code === 'string' ? data.code : null,
  }
}
