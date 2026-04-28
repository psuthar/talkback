import { useId, useState } from 'react'
import styles from './OrchestrationRecGroup.module.css'
import { getGroupLabel } from '../utils/orchestrationGrouping'

/**
 * OrchestrationRecGroup — collapsible group container for one recommendation
 * type in the creator's "AI Suggested Next Actions" panel (SCRUM-193).
 *
 * Header toggle is a button so screen readers announce it as interactive.
 * Pressing Enter or Space activates it (default <button> behavior).
 * `aria-expanded` mirrors the prop; `aria-controls` points at the body region.
 *
 * SCRUM-194 adds an optional inline "Dismiss all (N)" affordance in the
 * header. When `onBulkDismiss` is provided, the header renders a secondary
 * button beside the toggle. Clicking it does not dismiss immediately; it
 * reveals an inline confirmation row ("Dismiss all (N)? — Yes, dismiss all /
 * Cancel"). Confirmation calls onBulkDismiss(), which is responsible for
 * iterating the current rec ids and dispatching the dismiss handler. If the
 * group is empty at click time, onBulkDismiss is a no-op (count gates render).
 *
 * decision_readiness must NOT pass `onBulkDismiss` — those items always need
 * attention; the parent enforces that policy.
 */
export function OrchestrationRecGroup({
  type,
  count,
  expanded,
  onToggle,
  onBulkDismiss,
  children,
}) {
  const bodyId = useId()
  const label = getGroupLabel(type)
  const [confirmingBulkDismiss, setConfirmingBulkDismiss] = useState(false)

  const handleConfirmBulkDismiss = () => {
    setConfirmingBulkDismiss(false)
    onBulkDismiss?.()
  }

  return (
    <section
      data-testid={`orchestration-group-${type}`}
      data-rec-type={type}
      className={styles.group}
    >
      <div className={styles.headerRow}>
        <button
          type="button"
          data-testid={`orchestration-group-toggle-${type}`}
          aria-expanded={expanded}
          aria-controls={bodyId}
          onClick={onToggle}
          className={styles.header}
        >
          <span aria-hidden="true" className={`${styles.chevron} ${expanded ? styles.chevronExpanded : ''}`}>
            ›
          </span>
          <span className={styles.label}>{label}</span>
          <span data-testid={`orchestration-group-count-${type}`} className={styles.count}>
            {count}
          </span>
        </button>

        {onBulkDismiss && count > 0 && (
          confirmingBulkDismiss ? (
            <span className={styles.bulkConfirm} data-testid={`orchestration-bulk-dismiss-confirm-row-${type}`}>
              <span className={styles.bulkConfirmPrompt}>Dismiss all ({count})?</span>
              <button
                type="button"
                data-testid={`orchestration-bulk-dismiss-confirm-${type}`}
                onClick={handleConfirmBulkDismiss}
                className={`${styles.bulkBtn} ${styles.bulkBtnConfirm}`}
              >
                Yes, dismiss all
              </button>
              <button
                type="button"
                data-testid={`orchestration-bulk-dismiss-cancel-${type}`}
                onClick={() => setConfirmingBulkDismiss(false)}
                className={`${styles.bulkBtn} ${styles.bulkBtnCancel}`}
              >
                Cancel
              </button>
            </span>
          ) : (
            <button
              type="button"
              data-testid={`orchestration-bulk-dismiss-${type}`}
              onClick={() => setConfirmingBulkDismiss(true)}
              className={`${styles.bulkBtn} ${styles.bulkBtnTrigger}`}
            >
              Dismiss all ({count})
            </button>
          )
        )}
      </div>

      {expanded && (
        <div id={bodyId} data-testid={`orchestration-group-body-${type}`} className={styles.body}>
          {children}
        </div>
      )}
    </section>
  )
}
