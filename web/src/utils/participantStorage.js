export const STORAGE_KEY_MATERIALS_COLLAPSED = 'talkback.participant.materialsCollapsed'

/** When set to "true", first-join onboarding for participant mode has been dismissed for this session id. */
export const STORAGE_KEY_PARTICIPANT_ONBOARDING_DISMISSED = 'talkback.participant.onboardingDismissed'

/**
 * Read the stored materials-collapsed preference for a session, if any.
 * Returns true/false when an explicit value is stored, or null when there is
 * no stored preference (caller should treat as "first visit" and apply default).
 */
export function getStoredMaterialsCollapsed(sessionId, storage = null) {
  if (!sessionId) return null
  const ls = storage ?? (typeof localStorage !== 'undefined' ? localStorage : null)
  if (!ls) return null
  try {
    const v = ls.getItem(`${STORAGE_KEY_MATERIALS_COLLAPSED}.${sessionId}`)
    if (v === null || v === undefined) return null
    return v === 'true'
  } catch {
    return null
  }
}

export function setStoredMaterialsCollapsed(sessionId, value, storage = null) {
  if (!sessionId) return
  const ls = storage ?? (typeof localStorage !== 'undefined' ? localStorage : null)
  if (!ls) return
  try {
    ls.setItem(`${STORAGE_KEY_MATERIALS_COLLAPSED}.${sessionId}`, String(!!value))
  } catch {
    // ignore
  }
}

/**
 * Whether the participant first-join onboarding dialog was dismissed for this session.
 */
export function isParticipantOnboardingDismissed(sessionId, storage = null) {
  if (!sessionId) return false
  const ls = storage ?? (typeof localStorage !== 'undefined' ? localStorage : null)
  if (!ls) return false
  try {
    return ls.getItem(`${STORAGE_KEY_PARTICIPANT_ONBOARDING_DISMISSED}.${sessionId}`) === 'true'
  } catch {
    return false
  }
}

export function setParticipantOnboardingDismissed(sessionId, storage = null) {
  if (!sessionId) return
  const ls = storage ?? (typeof localStorage !== 'undefined' ? localStorage : null)
  if (!ls) return
  try {
    ls.setItem(`${STORAGE_KEY_PARTICIPANT_ONBOARDING_DISMISSED}.${sessionId}`, 'true')
  } catch {
    // ignore
  }
}
