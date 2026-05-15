import { describe, it, expect } from 'vitest'
import {
  STORAGE_KEY_MATERIALS_COLLAPSED,
  STORAGE_KEY_PARTICIPANT_ONBOARDING_DISMISSED,
  STORAGE_KEY_CONTEXT_EXPANDED,
  STORAGE_KEY_MEMBERS_EXPANDED,
  STORAGE_KEY_MATERIALS_TREE_EXPANDED,
  getStoredMaterialsCollapsed,
  setStoredMaterialsCollapsed,
  isParticipantOnboardingDismissed,
  setParticipantOnboardingDismissed,
  getStoredContextExpanded,
  setStoredContextExpanded,
  getStoredMembersExpanded,
  setStoredMembersExpanded,
  getStoredMaterialsTreeExpanded,
  setStoredMaterialsTreeExpanded,
} from '../utils/participantStorage'

function makeMockStorage(initial = {}) {
  const data = { ...initial }
  return {
    getItem: (k) => (k in data ? data[k] : null),
    setItem: (k, v) => { data[k] = String(v) },
    removeItem: (k) => { delete data[k] },
    _data: data,
  }
}

describe('participantStorage', () => {
  it('returns null when no value is stored (first visit)', () => {
    const storage = makeMockStorage()
    expect(getStoredMaterialsCollapsed('sess-1', storage)).toBeNull()
  })

  it('returns true when "true" is stored', () => {
    const storage = makeMockStorage({ [`${STORAGE_KEY_MATERIALS_COLLAPSED}.sess-1`]: 'true' })
    expect(getStoredMaterialsCollapsed('sess-1', storage)).toBe(true)
  })

  it('returns false when "false" is stored', () => {
    const storage = makeMockStorage({ [`${STORAGE_KEY_MATERIALS_COLLAPSED}.sess-1`]: 'false' })
    expect(getStoredMaterialsCollapsed('sess-1', storage)).toBe(false)
  })

  it('returns null when sessionId is empty', () => {
    const storage = makeMockStorage({ [`${STORAGE_KEY_MATERIALS_COLLAPSED}.`]: 'true' })
    expect(getStoredMaterialsCollapsed('', storage)).toBeNull()
    expect(getStoredMaterialsCollapsed(null, storage)).toBeNull()
    expect(getStoredMaterialsCollapsed(undefined, storage)).toBeNull()
  })

  it('isolates values per session id', () => {
    const storage = makeMockStorage()
    setStoredMaterialsCollapsed('sess-A', true, storage)
    setStoredMaterialsCollapsed('sess-B', false, storage)
    expect(getStoredMaterialsCollapsed('sess-A', storage)).toBe(true)
    expect(getStoredMaterialsCollapsed('sess-B', storage)).toBe(false)
    expect(getStoredMaterialsCollapsed('sess-C', storage)).toBeNull()
  })

  it('stores boolean values as the string "true"/"false"', () => {
    const storage = makeMockStorage()
    setStoredMaterialsCollapsed('sess-1', true, storage)
    expect(storage._data[`${STORAGE_KEY_MATERIALS_COLLAPSED}.sess-1`]).toBe('true')
    setStoredMaterialsCollapsed('sess-1', false, storage)
    expect(storage._data[`${STORAGE_KEY_MATERIALS_COLLAPSED}.sess-1`]).toBe('false')
  })

  it('returns null when storage throws', () => {
    const storage = {
      getItem: () => { throw new Error('blocked') },
      setItem: () => { throw new Error('blocked') },
    }
    expect(getStoredMaterialsCollapsed('sess-1', storage)).toBeNull()
  })

  it('does not persist when sessionId is missing', () => {
    const storage = makeMockStorage()
    setStoredMaterialsCollapsed('', true, storage)
    expect(Object.keys(storage._data)).toHaveLength(0)
  })

  describe('participant onboarding dismissed', () => {
    it('returns false when not yet dismissed', () => {
      const storage = makeMockStorage()
      expect(isParticipantOnboardingDismissed('sess-1', storage)).toBe(false)
    })

    it('returns true when dismissed flag is stored', () => {
      const storage = makeMockStorage({
        [`${STORAGE_KEY_PARTICIPANT_ONBOARDING_DISMISSED}.sess-1`]: 'true',
      })
      expect(isParticipantOnboardingDismissed('sess-1', storage)).toBe(true)
    })

    it('isolates dismissal per session id', () => {
      const storage = makeMockStorage()
      setParticipantOnboardingDismissed('sess-A', storage)
      expect(isParticipantOnboardingDismissed('sess-A', storage)).toBe(true)
      expect(isParticipantOnboardingDismissed('sess-B', storage)).toBe(false)
    })

    it('does not persist when sessionId is missing', () => {
      const storage = makeMockStorage()
      setParticipantOnboardingDismissed('', storage)
      expect(Object.keys(storage._data)).toHaveLength(0)
    })
  })

  describe.each([
    {
      label: 'context expanded',
      key: STORAGE_KEY_CONTEXT_EXPANDED,
      get: getStoredContextExpanded,
      set: setStoredContextExpanded,
    },
    {
      label: 'members expanded',
      key: STORAGE_KEY_MEMBERS_EXPANDED,
      get: getStoredMembersExpanded,
      set: setStoredMembersExpanded,
    },
    {
      label: 'materials tree expanded',
      key: STORAGE_KEY_MATERIALS_TREE_EXPANDED,
      get: getStoredMaterialsTreeExpanded,
      set: setStoredMaterialsTreeExpanded,
    },
  ])('$label', ({ key, get, set }) => {
    it('returns null on first visit (no stored value)', () => {
      const storage = makeMockStorage()
      expect(get('sess-1', storage)).toBeNull()
    })

    it('round-trips true and false per session id', () => {
      const storage = makeMockStorage()
      set('sess-A', true, storage)
      set('sess-B', false, storage)
      expect(get('sess-A', storage)).toBe(true)
      expect(get('sess-B', storage)).toBe(false)
      expect(get('sess-C', storage)).toBeNull()
    })

    it('persists values as the string "true"/"false"', () => {
      const storage = makeMockStorage()
      set('sess-1', true, storage)
      expect(storage._data[`${key}.sess-1`]).toBe('true')
      set('sess-1', false, storage)
      expect(storage._data[`${key}.sess-1`]).toBe('false')
    })

    it('returns null when sessionId is empty', () => {
      const storage = makeMockStorage({ [`${key}.`]: 'true' })
      expect(get('', storage)).toBeNull()
      expect(get(null, storage)).toBeNull()
      expect(get(undefined, storage)).toBeNull()
    })

    it('does not persist when sessionId is missing', () => {
      const storage = makeMockStorage()
      set('', true, storage)
      expect(Object.keys(storage._data)).toHaveLength(0)
    })

    it('returns null when storage throws', () => {
      const storage = {
        getItem: () => { throw new Error('blocked') },
        setItem: () => { throw new Error('blocked') },
      }
      expect(get('sess-1', storage)).toBeNull()
    })

    it('uses an isolated storage key (no cross-helper bleed)', () => {
      const storage = makeMockStorage()
      set('sess-1', true, storage)
      const keys = Object.keys(storage._data)
      expect(keys).toEqual([`${key}.sess-1`])
    })
  })
})
