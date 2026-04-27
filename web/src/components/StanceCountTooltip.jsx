import { useEffect, useRef, useState } from 'react'
import styles from './DecisionBar.module.css'

const HOVER_IN_MS = 150
const HOVER_OUT_MS = 300

export function StanceCountTooltip({
  stance,
  label,
  count,
  members,
  hoverInMs = HOVER_IN_MS,
  hoverOutMs = HOVER_OUT_MS,
}) {
  const [open, setOpen] = useState(false)
  const [pinned, setPinned] = useState(false)
  const enterTimer = useRef(null)
  const leaveTimer = useRef(null)

  const clearTimers = () => {
    if (enterTimer.current) { clearTimeout(enterTimer.current); enterTimer.current = null }
    if (leaveTimer.current) { clearTimeout(leaveTimer.current); leaveTimer.current = null }
  }

  useEffect(() => () => clearTimers(), [])

  const handleEnter = () => {
    if (pinned) return
    if (leaveTimer.current) { clearTimeout(leaveTimer.current); leaveTimer.current = null }
    if (open) return
    enterTimer.current = setTimeout(() => { setOpen(true) }, hoverInMs)
  }

  const handleLeave = () => {
    if (pinned) return
    if (enterTimer.current) { clearTimeout(enterTimer.current); enterTimer.current = null }
    if (!open) return
    leaveTimer.current = setTimeout(() => { setOpen(false) }, hoverOutMs)
  }

  const handleClick = () => {
    clearTimers()
    if (pinned) {
      setPinned(false)
      setOpen(false)
    } else {
      setPinned(true)
      setOpen(true)
    }
  }

  const safeMembers = Array.isArray(members) ? members : []

  return (
    <span
      className={styles.countWrap}
      onMouseEnter={handleEnter}
      onMouseLeave={handleLeave}
      onFocus={handleEnter}
      onBlur={handleLeave}
    >
      <button
        type="button"
        onClick={handleClick}
        className={`${styles.countChip} ${styles[`stance_${stance}`] || ''} ${pinned ? styles.countPinned : ''}`}
        data-testid={`stance-count-${stance}`}
        data-pinned={pinned ? 'true' : 'false'}
        aria-haspopup="true"
        aria-expanded={open}
        aria-label={`${label}: ${count}${safeMembers.length > 0 ? ', click to view members' : ''}`}
      >
        <span className={styles.countLabel}>{label}</span>
        <span className={styles.countNum}>{count}</span>
      </button>
      {open && (
        <div
          className={styles.tooltip}
          role="tooltip"
          data-testid={`stance-tooltip-${stance}`}
        >
          <div className={styles.tooltipArrow} aria-hidden />
          <div className={styles.tooltipHeader}>
            {label} ({count})
          </div>
          {safeMembers.length === 0 ? (
            <div className={styles.tooltipEmpty}>—</div>
          ) : (
            <ul className={styles.tooltipList}>
              {safeMembers.map((m, i) => (
                <li key={(m.id ?? m.user_email ?? '') + ':' + i} className={styles.tooltipItem}>
                  {m.user_email || 'Unknown'}
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </span>
  )
}
