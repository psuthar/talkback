export const STORAGE_KEY_MATERIALS_COLLAPSED = 'talkback.participant.materialsCollapsed'

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
