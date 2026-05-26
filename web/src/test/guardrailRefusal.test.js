// SCRUM-581: tests for the guardrail-refusal client-side helper. Contract source:
// docs/guardrails/refusal-shape.md. The helper must recognize the discriminator
// (`error === "guardrail_blocked"`), return the verbatim `user_message`, and refuse
// to misidentify success-shape or malformed bodies as refusals.

import { describe, it, expect } from 'vitest'
import { extractGuardrailRefusal } from '../utils/guardrailRefusal'

describe('extractGuardrailRefusal', () => {
  it('detects input_injection refusal and returns the contract user_message', () => {
    const result = extractGuardrailRefusal({
      error: 'guardrail_blocked',
      guardrail: 'input_injection',
      code: 'input_injection',
      user_message: 'Question detected to have unsafe content — not processed.',
    })
    expect(result).toEqual({
      isRefusal: true,
      message: 'Question detected to have unsafe content — not processed.',
      guardrail: 'input_injection',
      code: 'input_injection',
    })
  })

  it('detects grounding_failed refusal (different slice, same shape)', () => {
    const result = extractGuardrailRefusal({
      error: 'guardrail_blocked',
      guardrail: 'grounding_failed',
      code: 'grounding_failed',
      user_message: 'The answer could not be verified against session content.',
    })
    expect(result.isRefusal).toBe(true)
    expect(result.message).toBe('The answer could not be verified against session content.')
    expect(result.guardrail).toBe('grounding_failed')
  })

  it('returns isRefusal=false for a normal question/answer success body', () => {
    const result = extractGuardrailRefusal({
      question: { id: 'q1', question_text: 'What is X?', created_at: '2026-05-26T12:00:00Z' },
      answer: { id: 'a1', answer_text: 'X is Y.', citations: [] },
    })
    expect(result.isRefusal).toBe(false)
    expect(result.message).toBeUndefined()
  })

  it('returns isRefusal=false for null / undefined / non-object input', () => {
    expect(extractGuardrailRefusal(null).isRefusal).toBe(false)
    expect(extractGuardrailRefusal(undefined).isRefusal).toBe(false)
    expect(extractGuardrailRefusal('error string').isRefusal).toBe(false)
    expect(extractGuardrailRefusal(42).isRefusal).toBe(false)
  })

  it('returns isRefusal=false when error is some other string', () => {
    const result = extractGuardrailRefusal({ error: 'rate_limited', message: 'Slow down.' })
    expect(result.isRefusal).toBe(false)
  })

  it('falls back to a generic message when the server omits user_message', () => {
    // Contract says user_message is required and non-empty, but the client must not
    // crash if a server bug omits it.
    const result = extractGuardrailRefusal({
      error: 'guardrail_blocked',
      guardrail: 'input_injection',
      code: 'input_injection',
    })
    expect(result.isRefusal).toBe(true)
    expect(result.message).toBe('Question was blocked by a safety guardrail.')
  })

  it('falls back when user_message is an empty string', () => {
    const result = extractGuardrailRefusal({
      error: 'guardrail_blocked',
      guardrail: 'input_injection',
      code: 'input_injection',
      user_message: '',
    })
    expect(result.isRefusal).toBe(true)
    expect(result.message).toBe('Question was blocked by a safety guardrail.')
  })
})
