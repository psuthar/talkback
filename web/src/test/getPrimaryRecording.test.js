import { describe, it, expect } from 'vitest'
import {
  getPrimaryRecording,
  getRecordings,
  hasAnyRecording,
} from '../utils/session'

// SCRUM-405: accessor regression tests. The pre-SCRUM-405 behavior — for
// any single-recording session — was `session.primary_video ??
// session.video_sources[0] ?? null`; this suite locks that in for the
// existing one-recording case and pins the new deterministic fallback
// (oldest by `created_at`) for multi-recording sessions.

describe('getPrimaryRecording', () => {
  it('returns null for null / undefined / empty session', () => {
    expect(getPrimaryRecording(null)).toBeNull()
    expect(getPrimaryRecording(undefined)).toBeNull()
    expect(getPrimaryRecording({})).toBeNull()
  })

  it('returns null when video_sources is empty and primary_video is absent', () => {
    expect(getPrimaryRecording({ video_sources: [] })).toBeNull()
  })

  it('prefers primary_video over video_sources[0]', () => {
    const primary = { id: 'primary' }
    const first = { id: 'first' }
    const session = { primary_video: primary, video_sources: [first] }
    expect(getPrimaryRecording(session)).toBe(primary)
  })

  it('falls back to video_sources[0] when primary_video is missing — single-recording parity with the legacy expression', () => {
    const only = { id: 'only' }
    expect(getPrimaryRecording({ video_sources: [only] })).toBe(only)
  })

  it('falls back to oldest recording by created_at when primary_video is missing and there are multiple', () => {
    const a = { id: 'a', created_at: '2026-01-02T00:00:00Z' }
    const b = { id: 'b', created_at: '2026-01-01T00:00:00Z' }
    const c = { id: 'c', created_at: '2026-01-03T00:00:00Z' }
    expect(getPrimaryRecording({ video_sources: [a, b, c] })).toBe(b)
  })

  it('handles mixed created_at + missing timestamps deterministically (timestamped wins, then array order)', () => {
    const noTs = { id: 'no-ts' }
    const ts = { id: 'ts', created_at: '2026-01-01T00:00:00Z' }
    // The timestamped row sorts ahead of the untimestamped one.
    expect(getPrimaryRecording({ video_sources: [noTs, ts] })).toBe(ts)
  })
})

describe('getRecordings', () => {
  it('returns [] for null / undefined / empty', () => {
    expect(getRecordings(null)).toEqual([])
    expect(getRecordings(undefined)).toEqual([])
    expect(getRecordings({})).toEqual([])
    expect(getRecordings({ video_sources: [] })).toEqual([])
  })

  it('returns the single-recording array unchanged', () => {
    const only = { id: 'only' }
    expect(getRecordings({ video_sources: [only] })).toEqual([only])
  })

  it('sorts multi-recording lists by created_at ascending (oldest first)', () => {
    const a = { id: 'a', created_at: '2026-01-02T00:00:00Z' }
    const b = { id: 'b', created_at: '2026-01-01T00:00:00Z' }
    const c = { id: 'c', created_at: '2026-01-03T00:00:00Z' }
    expect(getRecordings({ video_sources: [a, b, c] }).map((r) => r.id)).toEqual([
      'b',
      'a',
      'c',
    ])
  })

  it('does not mutate the input array', () => {
    const a = { id: 'a', created_at: '2026-01-02T00:00:00Z' }
    const b = { id: 'b', created_at: '2026-01-01T00:00:00Z' }
    const input = [a, b]
    getRecordings({ video_sources: input })
    expect(input).toEqual([a, b])
  })
})

describe('hasAnyRecording', () => {
  it('is false when session has no recordings and no primary_video', () => {
    expect(hasAnyRecording(null)).toBe(false)
    expect(hasAnyRecording({})).toBe(false)
    expect(hasAnyRecording({ video_sources: [] })).toBe(false)
  })

  it('is true when primary_video is set', () => {
    expect(hasAnyRecording({ primary_video: { id: 'x' } })).toBe(true)
  })

  it('is true when video_sources is non-empty', () => {
    expect(hasAnyRecording({ video_sources: [{ id: 'x' }] })).toBe(true)
  })
})
