/**
 * Pure helpers for grouping recommendations by recommendation_type in the
 * creator's "AI Suggested Next Actions" panel (SCRUM-193). Lives outside the
 * component so the grouping rules can be unit-tested without React.
 */

// Priority order. The first entry in this array that has at least one
// recommendation in the current set is considered the "primary" group and is
// expanded by default. Lower-priority groups default to collapsed.
export const PRIORITY_ORDER = [
  'decision_readiness',
  'review_draft_answer',
  'unanswered_question',
]

// Human-readable headers per recommendation_type. Unknown types fall back to a
// snake_case → space-case transform.
const GROUP_LABELS = {
  decision_readiness: 'Decision readiness',
  review_draft_answer: 'Draft answers awaiting review',
  unanswered_question: 'Unanswered questions',
}

export function groupRecommendations(recs) {
  const groups = {}
  if (!Array.isArray(recs)) return groups
  for (const rec of recs) {
    if (!rec) continue
    const type = rec.recommendation_type || 'unknown'
    if (!groups[type]) groups[type] = []
    groups[type].push(rec)
  }
  return groups
}

/**
 * Return group types in render order. Known types appear in PRIORITY_ORDER;
 * unknown types appear after, in alphabetical order so the output is stable.
 * Only types with at least one recommendation are returned.
 */
export function getOrderedGroupTypes(groups) {
  const result = []
  if (!groups) return result
  const seen = new Set()
  for (const type of PRIORITY_ORDER) {
    if (groups[type] && groups[type].length > 0) {
      result.push(type)
      seen.add(type)
    }
  }
  const unknownTypes = Object.keys(groups)
    .filter((type) => !seen.has(type) && groups[type].length > 0)
    .sort()
  return [...result, ...unknownTypes]
}

export function getGroupLabel(type) {
  if (GROUP_LABELS[type]) return GROUP_LABELS[type]
  return String(type || 'unknown').replaceAll('_', ' ')
}

/**
 * Decide whether a single decision_readiness recommendation should render
 * outside any group wrapper. The inline outcome textarea is the panel's most
 * important interaction and must stay first-class — never hidden behind a
 * collapsed group header.
 */
export function shouldRenderUngrouped(type, recs) {
  return type === 'decision_readiness' && Array.isArray(recs) && recs.length === 1
}

/**
 * Default-expanded type given the current ordered group list. Returns the
 * first (highest-priority) entry, or null if there are no groups.
 */
export function getDefaultExpandedType(orderedTypes) {
  if (!Array.isArray(orderedTypes) || orderedTypes.length === 0) return null
  return orderedTypes[0]
}
