import { useId } from 'react'
import styles from './OrchestrationRecGroup.module.css'
import { getGroupLabel } from '../utils/orchestrationGrouping'

/**
 * OrchestrationRecGroup — collapsible group container for one recommendation
 * type in the creator's "AI Suggested Next Actions" panel (SCRUM-193).
 *
 * Header is a button so screen readers announce it as interactive. Pressing
 * Enter or Space activates it (default <button> behavior). aria-expanded
 * mirrors the prop; aria-controls points at the body region for assistive
 * tech.
 *
 * The single decision_readiness ungrouped case is handled by the parent — this
 * component is only mounted when grouping is in effect.
 */
export function OrchestrationRecGroup({
  type,
  count,
  expanded,
  onToggle,
  children,
}) {
  const bodyId = useId()
  const label = getGroupLabel(type)

  return (
    <section
      data-testid={`orchestration-group-${type}`}
      data-rec-type={type}
      className={styles.group}
    >
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
      {expanded && (
        <div id={bodyId} data-testid={`orchestration-group-body-${type}`} className={styles.body}>
          {children}
        </div>
      )}
    </section>
  )
}
