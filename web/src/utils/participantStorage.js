export const STORAGE_KEY_MATERIALS_COLLAPSED = 'talkback.participant.materialsCollapsed'

/** When set to "true", first-join onboarding for participant mode has been dismissed for this session id. */
export const STORAGE_KEY_PARTICIPANT_ONBOARDING_DISMISSED = 'talkback.participant.onboardingDismissed'

/** Per-section expanded preferences for the session sidebar (creator + participant). */
export const STORAGE_KEY_CONTEXT_EXPANDED = 'talkback.session.contextExpanded'
export const STORAGE_KEY_MEMBERS_EXPANDED = 'talkback.session.membersExpanded'
export const STORAGE_KEY_MATERIALS_TREE_EXPANDED = 'talkback.session.materialsTreeExpanded'

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

function readBooleanPreference(storageKey, sessionId, storage) {
  if (!sessionId) return null
  const ls = storage ?? (typeof localStorage !== 'undefined' ? localStorage : null)
  if (!ls) return null
  try {
    const v = ls.getItem(`${storageKey}.${sessionId}`)
    if (v === null || v === undefined) return null
    return v === 'true'
  } catch {
    return null
  }
}

function writeBooleanPreference(storageKey, sessionId, value, storage) {
  if (!sessionId) return
  const ls = storage ?? (typeof localStorage !== 'undefined' ? localStorage : null)
  if (!ls) return
  try {
    ls.setItem(`${storageKey}.${sessionId}`, String(!!value))
  } catch {
    // ignore
  }
}

/**
 * Read the stored expanded preference for the sidebar Context block.
 * Returns true/false when stored, or null when no preference exists.
 * Callers apply their own default (the block defaults to collapsed).
 */
export function getStoredContextExpanded(sessionId, storage = null) {
  return readBooleanPreference(STORAGE_KEY_CONTEXT_EXPANDED, sessionId, storage)
}

export function setStoredContextExpanded(sessionId, value, storage = null) {
  writeBooleanPreference(STORAGE_KEY_CONTEXT_EXPANDED, sessionId, value, storage)
}

/**
 * Read the stored expanded preference for the sidebar Members block.
 * Returns true/false when stored, or null when no preference exists.
 * Callers apply their own default (the block defaults to collapsed).
 */
export function getStoredMembersExpanded(sessionId, storage = null) {
  return readBooleanPreference(STORAGE_KEY_MEMBERS_EXPANDED, sessionId, storage)
}

export function setStoredMembersExpanded(sessionId, value, storage = null) {
  writeBooleanPreference(STORAGE_KEY_MEMBERS_EXPANDED, sessionId, value, storage)
}

/**
 * Read the stored expanded preference for the Materials tree sub-collapse.
 * Distinct from STORAGE_KEY_MATERIALS_COLLAPSED, which controls the
 * column-level (48px) collapse for the entire materials panel.
 * Returns true/false when stored, or null when no preference exists.
 * Callers apply their own default (the tree defaults to expanded).
 */
export function getStoredMaterialsTreeExpanded(sessionId, storage = null) {
  return readBooleanPreference(STORAGE_KEY_MATERIALS_TREE_EXPANDED, sessionId, storage)
}

export function setStoredMaterialsTreeExpanded(sessionId, value, storage = null) {
  writeBooleanPreference(STORAGE_KEY_MATERIALS_TREE_EXPANDED, sessionId, value, storage)
}
