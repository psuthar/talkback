import { describe, it, expect } from 'vitest'
import {
  NARROW_VIEWPORT_PX,
  sessionMaterialsCount,
  shouldForceCollapseSidebarOnLoad,
  resolveInitialExpanded,
} from '../utils/sessionSidebar'

describe('sessionMaterialsCount', () => {
  it('returns 0 for null/undefined session', () => {
    expect(sessionMaterialsCount(null)).toBe(0)
    expect(sessionMaterialsCount(undefined)).toBe(0)
  })

  it('returns 0 when all three lists are missing', () => {
    expect(sessionMaterialsCount({})).toBe(0)
  })

  it('sums video_sources + materials + links', () => {
    const session = {
      video_sources: [{ id: 1 }, { id: 2 }],
      materials: [{ id: 'a' }, { id: 'b' }, { id: 'c' }],
      links: [{ id: 10 }],
    }
    expect(sessionMaterialsCount(session)).toBe(6)
  })

  it('treats missing lists as 0 and present lists normally', () => {
    expect(sessionMaterialsCount({ materials: [{ id: 'a' }] })).toBe(1)
    expect(sessionMaterialsCount({ video_sources: [], links: [{ id: 1 }] })).toBe(1)
  })

  it('ignores non-array values defensively', () => {
    const session = {
      video_sources: 'not an array',
      materials: null,
      links: undefined,
    }
    expect(sessionMaterialsCount(session)).toBe(0)
  })
})

describe('shouldForceCollapseSidebarOnLoad', () => {
  it('returns false when no window is available', () => {
    expect(shouldForceCollapseSidebarOnLoad(null)).toBe(false)
  })

  it('returns false when innerWidth is not numeric', () => {
    expect(shouldForceCollapseSidebarOnLoad({})).toBe(false)
    expect(shouldForceCollapseSidebarOnLoad({ innerWidth: '1200' })).toBe(false)
  })

  it('returns true below NARROW_VIEWPORT_PX', () => {
    expect(shouldForceCollapseSidebarOnLoad({ innerWidth: 800 })).toBe(true)
    expect(shouldForceCollapseSidebarOnLoad({ innerWidth: NARROW_VIEWPORT_PX - 1 })).toBe(true)
  })

  it('returns false at or above NARROW_VIEWPORT_PX', () => {
    expect(shouldForceCollapseSidebarOnLoad({ innerWidth: NARROW_VIEWPORT_PX })).toBe(false)
    expect(shouldForceCollapseSidebarOnLoad({ innerWidth: 1440 })).toBe(false)
  })
})

describe('resolveInitialExpanded', () => {
  const wide = { innerWidth: 1440 }
  const narrow = { innerWidth: 800 }

  it('uses defaultExpanded when no preference is stored (wide viewport)', () => {
    expect(resolveInitialExpanded({
      stored: null, defaultExpanded: true, honorNarrowOverride: true, win: wide,
    })).toBe(true)
    expect(resolveInitialExpanded({
      stored: null, defaultExpanded: false, honorNarrowOverride: true, win: wide,
    })).toBe(false)
  })

  it('uses the stored preference when one exists (wide viewport)', () => {
    expect(resolveInitialExpanded({
      stored: true, defaultExpanded: false, honorNarrowOverride: true, win: wide,
    })).toBe(true)
    expect(resolveInitialExpanded({
      stored: false, defaultExpanded: true, honorNarrowOverride: true, win: wide,
    })).toBe(false)
  })

  it('forces collapsed at narrow viewport when honorNarrowOverride is true, even with stored=true', () => {
    expect(resolveInitialExpanded({
      stored: true, defaultExpanded: true, honorNarrowOverride: true, win: narrow,
    })).toBe(false)
  })

  it('ignores narrow viewport when honorNarrowOverride is false', () => {
    expect(resolveInitialExpanded({
      stored: true, defaultExpanded: false, honorNarrowOverride: false, win: narrow,
    })).toBe(true)
    expect(resolveInitialExpanded({
      stored: null, defaultExpanded: true, honorNarrowOverride: false, win: narrow,
    })).toBe(true)
  })

  it('treats undefined stored as no preference', () => {
    expect(resolveInitialExpanded({
      stored: undefined, defaultExpanded: true, honorNarrowOverride: false, win: wide,
    })).toBe(true)
  })

  it('models the three sidebar sections together', () => {
    // Wide viewport, no stored preferences — defaults take effect
    expect(resolveInitialExpanded({ stored: null, defaultExpanded: false, honorNarrowOverride: true, win: wide })).toBe(false) // Context
    expect(resolveInitialExpanded({ stored: null, defaultExpanded: false, honorNarrowOverride: true, win: wide })).toBe(false) // Members
    expect(resolveInitialExpanded({ stored: null, defaultExpanded: true, honorNarrowOverride: false, win: wide })).toBe(true) // Materials tree

    // Narrow viewport, all three previously expanded — Context/Members collapse, Materials stays open
    expect(resolveInitialExpanded({ stored: true, defaultExpanded: false, honorNarrowOverride: true, win: narrow })).toBe(false) // Context
    expect(resolveInitialExpanded({ stored: true, defaultExpanded: false, honorNarrowOverride: true, win: narrow })).toBe(false) // Members
    expect(resolveInitialExpanded({ stored: true, defaultExpanded: true, honorNarrowOverride: false, win: narrow })).toBe(true) // Materials tree
  })
})
