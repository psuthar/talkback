/**
 * Unit tests for exported helpers in CreatorMode.jsx (SCRUM-79 / PR gate: orchestration domain evidence).
 * Keeps coverage on pure logic without mounting the full creator layout.
 */
import { describe, it, expect } from 'vitest'
import { getOrchestrationEmptyStateMessage, getOrchestrationLoadFeedback } from './CreatorMode.jsx'

describe('getOrchestrationEmptyStateMessage', () => {
  it('returns onboarding hint when session has no content and no questions', () => {
    const session = { materials: [], links: [], video_sources: [] }
    const msg = getOrchestrationEmptyStateMessage(session, [])
    expect(msg).toMatch(/Add transcript or materials/i)
  })

  it('returns generic empty copy when some content exists but recommendations are empty', () => {
    const session = { materials: [{ id: '1' }], links: [], video_sources: [] }
    const msg = getOrchestrationEmptyStateMessage(session, [])
    expect(msg).toBe('No recommendations right now.')
  })
})

describe('getOrchestrationLoadFeedback', () => {
  it('returns info for 404', () => {
    const fb = getOrchestrationLoadFeedback(404, {})
    expect(fb.type).toBe('info')
    expect(fb.message).toMatch(/initial content/i)
  })

  it('returns error with server message when present', () => {
    const fb = getOrchestrationLoadFeedback(500, { error: 'boom' })
    expect(fb.type).toBe('error')
    expect(fb.message).toBe('boom')
  })
})
