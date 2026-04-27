import { describe, it, expect } from 'vitest'
import {
  STORAGE_KEY_MATERIALS_COLLAPSED,
  getStoredMaterialsCollapsed,
  setStoredMaterialsCollapsed,
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
})
