import { describe, it, expect } from 'vitest'
import {
  PRIORITY_ORDER,
  groupRecommendations,
  getOrderedGroupTypes,
  getGroupLabel,
  shouldRenderUngrouped,
  getDefaultExpandedType,
} from '../utils/orchestrationGrouping'

const rec = (id, type) => ({ id, recommendation_type: type })

describe('groupRecommendations', () => {
  it('groups recs by recommendation_type, preserving order within each group', () => {
    const recs = [
      rec('a', 'review_draft_answer'),
      rec('b', 'unanswered_question'),
      rec('c', 'review_draft_answer'),
      rec('d', 'decision_readiness'),
    ]
    const groups = groupRecommendations(recs)
    expect(groups.review_draft_answer.map((r) => r.id)).toEqual(['a', 'c'])
    expect(groups.unanswered_question.map((r) => r.id)).toEqual(['b'])
    expect(groups.decision_readiness.map((r) => r.id)).toEqual(['d'])
  })

  it('uses "unknown" as the bucket for missing recommendation_type', () => {
    const groups = groupRecommendations([rec('x', null), rec('y', undefined)])
    expect(groups.unknown.map((r) => r.id)).toEqual(['x', 'y'])
  })

  it('handles non-array input gracefully', () => {
    expect(groupRecommendations(null)).toEqual({})
    expect(groupRecommendations(undefined)).toEqual({})
  })

  it('skips falsy recs without crashing', () => {
    const groups = groupRecommendations([null, rec('a', 'review_draft_answer'), undefined])
    expect(Object.values(groups).flat().map((r) => r.id)).toEqual(['a'])
  })
})

describe('getOrderedGroupTypes — priority order is decision_readiness > review_draft_answer > unanswered_question', () => {
  it('returns known types in priority order, regardless of insertion order', () => {
    const groups = {
      unanswered_question: [rec('u', 'unanswered_question')],
      review_draft_answer: [rec('r', 'review_draft_answer')],
      decision_readiness: [rec('d', 'decision_readiness')],
    }
    expect(getOrderedGroupTypes(groups)).toEqual([
      'decision_readiness',
      'review_draft_answer',
      'unanswered_question',
    ])
  })

  it('omits types with no recommendations', () => {
    const groups = {
      review_draft_answer: [rec('r', 'review_draft_answer')],
      decision_readiness: [],
      unanswered_question: [rec('u', 'unanswered_question')],
    }
    expect(getOrderedGroupTypes(groups)).toEqual(['review_draft_answer', 'unanswered_question'])
  })

  it('appends unknown types after the priority list, sorted alphabetically for stability', () => {
    const groups = {
      unknown_b: [rec('1', 'unknown_b')],
      unknown_a: [rec('2', 'unknown_a')],
      review_draft_answer: [rec('3', 'review_draft_answer')],
    }
    expect(getOrderedGroupTypes(groups)).toEqual(['review_draft_answer', 'unknown_a', 'unknown_b'])
  })

  it('returns [] when groups is empty or null', () => {
    expect(getOrderedGroupTypes({})).toEqual([])
    expect(getOrderedGroupTypes(null)).toEqual([])
  })
})

describe('getGroupLabel', () => {
  it('returns the human label for known types', () => {
    expect(getGroupLabel('decision_readiness')).toBe('Decision readiness')
    expect(getGroupLabel('review_draft_answer')).toBe('Draft answers awaiting review')
    expect(getGroupLabel('unanswered_question')).toBe('Unanswered questions')
  })

  it('falls back to a snake_case → space transform for unknown types', () => {
    expect(getGroupLabel('some_new_type')).toBe('some new type')
  })

  it('returns the literal "unknown" for empty input', () => {
    expect(getGroupLabel('')).toBe('unknown')
    expect(getGroupLabel(null)).toBe('unknown')
  })
})

describe('shouldRenderUngrouped — keeps single decision_readiness first-class', () => {
  it('is true for exactly one decision_readiness recommendation', () => {
    expect(shouldRenderUngrouped('decision_readiness', [rec('d', 'decision_readiness')])).toBe(true)
  })

  it('is false for two or more decision_readiness recs (use a group then)', () => {
    expect(
      shouldRenderUngrouped('decision_readiness', [
        rec('d1', 'decision_readiness'),
        rec('d2', 'decision_readiness'),
      ])
    ).toBe(false)
  })

  it('is false for any other type, even with one rec (no special-case for them)', () => {
    expect(shouldRenderUngrouped('review_draft_answer', [rec('r', 'review_draft_answer')])).toBe(false)
    expect(shouldRenderUngrouped('unanswered_question', [rec('u', 'unanswered_question')])).toBe(false)
  })

  it('is false for an empty group', () => {
    expect(shouldRenderUngrouped('decision_readiness', [])).toBe(false)
  })
})

describe('getDefaultExpandedType', () => {
  it('returns the first ordered type', () => {
    expect(getDefaultExpandedType(['decision_readiness', 'review_draft_answer'])).toBe('decision_readiness')
  })

  it('returns null when no types are present', () => {
    expect(getDefaultExpandedType([])).toBe(null)
    expect(getDefaultExpandedType(null)).toBe(null)
  })
})

describe('PRIORITY_ORDER constant', () => {
  it('exposes the documented priority for downstream consumers', () => {
    expect(PRIORITY_ORDER).toEqual([
      'decision_readiness',
      'review_draft_answer',
      'unanswered_question',
    ])
  })
})
