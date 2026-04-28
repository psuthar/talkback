import styles from './OrchestrationRecCard.module.css'

/**
 * OrchestrationRecCard — wraps a single recommendation card in the creator's
 * "AI Suggested Next Actions" panel (SCRUM-191).
 *
 * Visual contract:
 * - Card body is white at all times. Per-type signal lives in a 3px left-border
 *   accent (review_draft_answer = amber, decision_readiness = success-green,
 *   unanswered_question = primary-blue, unknown = neutral).
 * - The status pill renders only for terminal statuses (approved, dismissed,
 *   completed). The default `new` state shows nothing — `new` is the default
 *   and contributes no information.
 *
 * Children render below the summary / suggested-action block (typically the
 * inline outcome form and the action-button row).
 *
 * The per-card uppercase recommendation_type label is preserved here. It will
 * be removed in SCRUM-193 once recommendations are grouped by type and the
 * group header carries the label.
 */

const TERMINAL_STATUSES = new Set(['approved', 'dismissed', 'completed'])
const KNOWN_TYPES = new Set(['review_draft_answer', 'decision_readiness', 'unanswered_question'])

export function OrchestrationRecCard({ rec, children }) {
  if (!rec) return null

  const recType = rec.recommendation_type
  const accentClass = KNOWN_TYPES.has(recType) ? styles[`card_${recType}`] : styles.card_unknown

  const status = rec.status
  const showStatusPill = TERMINAL_STATUSES.has(status)
  const statusPillClass = showStatusPill ? styles[`statusPill_${status}`] || '' : ''

  return (
    <div
      data-testid={`orchestration-rec-${rec.id}`}
      data-rec-type={recType || 'unknown'}
      className={`${styles.card} ${accentClass}`}
    >
      <div className={styles.header}>
        <span className={styles.typeLabel}>
          {String(recType || '').replaceAll('_', ' ')}
        </span>
        {showStatusPill && (
          <span
            data-testid={`orchestration-status-${rec.id}`}
            className={`${styles.statusPill} ${statusPillClass}`}
          >
            {status}
          </span>
        )}
      </div>
      <div className={styles.summary}>{rec.summary}</div>
      {rec.suggested_action && (
        <div className={styles.suggestedAction}>{rec.suggested_action}</div>
      )}
      {children}
    </div>
  )
}
