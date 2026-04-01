import { describe, it, expect } from 'vitest'
import { getOrchestrationEmptyStateMessage, getOrchestrationLoadFeedback } from '../modes/CreatorMode'

describe('orchestration panel messaging helpers', () => {
  it('returns onboarding empty-state copy for new sessions with no inputs', () => {
    const msg = getOrchestrationEmptyStateMessage({ materials: [], links: [], video_sources: [] }, [])
    expect(msg).toContain('Add transcript or materials')
  })

  it('returns default empty-state copy when session already has content', () => {
    const msg = getOrchestrationEmptyStateMessage({ materials: [{ id: 'm1' }], links: [], video_sources: [] }, [])
    expect(msg).toBe('No recommendations right now.')
  })

  it('maps 404 recommendation load to info guidance', () => {
    const fb = getOrchestrationLoadFeedback(404, {})
    expect(fb.type).toBe('info')
    expect(fb.message).toContain('initial content')
  })

  it('maps non-404 failures to error text', () => {
    const fb = getOrchestrationLoadFeedback(500, { message: 'Server down' })
    expect(fb.type).toBe('error')
    expect(fb.message).toBe('Server down')
  })
})
