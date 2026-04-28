import { describe, it, expect } from 'vitest'
import { shouldShowAppAuthCluster } from '../headerVisibility'

describe('top-right header visibility (SCRUM-198, SCRUM-199)', () => {
  it('hides auth/debug/admin cluster in creator mode with active session', () => {
    const showCluster = shouldShowAppAuthCluster({
      isParticipantMode: false,
      hasValidSession: true,
      sessionUserMode: 'creator',
    })
    expect(showCluster).toBe(false)
  })

  it('still hides auth cluster in participant mode with active session', () => {
    const showCluster = shouldShowAppAuthCluster({
      isParticipantMode: true,
      hasValidSession: true,
      sessionUserMode: 'participant',
    })
    expect(showCluster).toBe(false)
  })

  it('shows auth cluster when no session is active', () => {
    const showCluster = shouldShowAppAuthCluster({
      isParticipantMode: false,
      hasValidSession: false,
      sessionUserMode: null,
    })
    expect(showCluster).toBe(true)
  })
})
