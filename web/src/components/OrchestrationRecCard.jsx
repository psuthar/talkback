import { useId, useRef, useState } from 'react'
import styles from './OrchestrationRecCard.module.css'

/**
 * OrchestrationRecCard — wraps a single recommendation card in the creator's
 * "AI Suggested Next Actions" panel.
 *
 * Visual contract:
 * - Card body is white at all times. Per-type signal lives in a 3px left-border
 *   accent (review_draft_answer = amber, decision_readiness = success-green,
 *   unanswered_question = primary-blue, unknown = neutral).
 * - The status pill renders only for terminal statuses (approved, dismissed,
 *   completed). The default `new` state shows nothing — `new` is the default
 *   and contributes no information.
 *
 * Progressive disclosure (SCRUM-196):
 * - When `expandable` is true (default), the card collapses to a summary-only
 *   trigger row. Clicking the trigger or pressing Enter/Space expands it to
 *   reveal `rec.suggested_action`, the inline outcome form (when the parent
 *   passes one as a child), and the action-button row. Collapsing returns
 *   focus to the trigger.
 * - When `expandable` is false, the card renders fully expanded with no
 *   chevron. The parent uses this for the single-item decision_readiness case
 *   (rendered ungrouped — the inline outcome textarea must stay first-class).
 * - `defaultExpanded` controls the initial state when expandable; default false.
 */

const TERMINAL_STATUSES = new Set(['approved', 'dismissed', 'completed'])
const KNOWN_TYPES = new Set(['review_draft_answer', 'decision_readiness', 'unanswered_question'])

export function OrchestrationRecCard({
  rec,
  expandable = true,
  defaultExpanded = false,
  children,
}) {
  const [expanded, setExpanded] = useState(defaultExpanded)
  const triggerRef = useRef(null)
  const bodyId = useId()

  if (!rec) return null

  const recType = rec.recommendation_type
  const accentClass = KNOWN_TYPES.has(recType) ? styles[`card_${recType}`] : styles.card_unknown

  const status = rec.status
  const showStatusPill = TERMINAL_STATUSES.has(status)
  const statusPillClass = showStatusPill ? styles[`statusPill_${status}`] || '' : ''

  const isExpanded = expandable ? expanded : true

  const handleToggle = () => {
    if (!expandable) return
    setExpanded((prev) => {
      const next = !prev
      if (!next) {
        // Collapsing — return focus to the trigger after the re-render.
        setTimeout(() => triggerRef.current?.focus(), 0)
      }
      return next
    })
  }

  return (
    <div
      data-testid={`orchestration-rec-${rec.id}`}
      data-rec-type={recType || 'unknown'}
      className={`${styles.card} ${accentClass}`}
    >
      {expandable ? (
        <button
          ref={triggerRef}
          type="button"
          data-testid={`orchestration-rec-toggle-${rec.id}`}
          aria-expanded={isExpanded}
          aria-controls={bodyId}
          onClick={handleToggle}
          className={styles.triggerRow}
        >
          <span aria-hidden="true" className={`${styles.chevron} ${isExpanded ? styles.chevronExpanded : ''}`}>
            ›
          </span>
          <span className={styles.summary}>{rec.summary}</span>
          {showStatusPill && (
            <span
              data-testid={`orchestration-status-${rec.id}`}
              className={`${styles.statusPill} ${statusPillClass}`}
            >
              {status}
            </span>
          )}
        </button>
      ) : (
        <div className={styles.staticHeader}>
          <span className={styles.summary}>{rec.summary}</span>
          {showStatusPill && (
            <span
              data-testid={`orchestration-status-${rec.id}`}
              className={`${styles.statusPill} ${statusPillClass}`}
            >
              {status}
            </span>
          )}
        </div>
      )}

      {isExpanded && (
        <div id={bodyId} data-testid={`orchestration-rec-body-${rec.id}`} className={styles.body}>
          {rec.suggested_action && (
            <div className={styles.suggestedAction}>{rec.suggested_action}</div>
          )}
          {children}
        </div>
      )}
    </div>
  )
}
