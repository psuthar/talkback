import { describe, it, expect } from 'vitest'
import { shouldShowAppAuthCluster, shouldShowCreatorParticipantCta } from '../headerVisibility'

describe('creator top-right header visibility (SCRUM-198)', () => {
  it('shows view-as-participant CTA in creator mode with active session', () => {
    const visible = shouldShowCreatorParticipantCta({
      hasValidSession: true,
      participantUrl: '/app/sessions/abc?mode=view',
      sessionUserMode: 'creator',
    })
    expect(visible).toBe(true)
  })

  it('hides auth/debug/admin cluster in creator mode when CTA is shown', () => {
    const showCluster = shouldShowAppAuthCluster({
      isParticipantMode: false,
      hasValidSession: true,
      showCreatorParticipantCta: true,
    })
    expect(showCluster).toBe(false)
  })

  it('still hides auth cluster in participant mode with active session', () => {
    const showCluster = shouldShowAppAuthCluster({
      isParticipantMode: true,
      hasValidSession: true,
      showCreatorParticipantCta: false,
    })
    expect(showCluster).toBe(false)
  })
})
