import { useEffect, useRef, useState } from 'react'
import styles from './DecisionBar.module.css'

/**
 * StanceCountTooltip — click-only popover that lists the members in a stance bucket.
 *
 * Controlled or uncontrolled:
 * - Controlled: pass `isOpen` (boolean) and `onToggle(stance)` so a parent (typically
 *   DecisionBar) can enforce a single-open invariant across multiple tooltips.
 * - Uncontrolled: omit both; the component manages its own open state.
 *
 * Hover / focus no longer open the tooltip — this is required for touch users and
 * keyboard users. Escape and outside-click close it. The chip carries a small
 * chevron (▾) when there are members so the affordance is visible without hover.
 */
export function StanceCountTooltip({
  stance,
  label,
  count,
  members,
  isOpen,
  onToggle,
}) {
  const controlled = typeof isOpen === 'boolean'
  const [internalOpen, setInternalOpen] = useState(false)
  const open = controlled ? isOpen : internalOpen

  const wrapRef = useRef(null)
  const buttonRef = useRef(null)

  const setOpen = (next) => {
    if (controlled) {
      onToggle?.(next ? stance : null)
    } else {
      setInternalOpen(next)
      onToggle?.(next ? stance : null)
    }
  }

  useEffect(() => {
    if (!open) return

    const handlePointerDown = (event) => {
      if (wrapRef.current && wrapRef.current.contains(event.target)) return
      setOpen(false)
    }
    const handleKeyDown = (event) => {
      if (event.key === 'Escape') {
        event.stopPropagation()
        setOpen(false)
        buttonRef.current?.focus()
      }
    }
    document.addEventListener('mousedown', handlePointerDown)
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('mousedown', handlePointerDown)
      document.removeEventListener('keydown', handleKeyDown)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const safeMembers = Array.isArray(members) ? members : []
  const hasMembers = safeMembers.length > 0

  const handleClick = () => {
    setOpen(!open)
  }

  return (
    <span ref={wrapRef} className={styles.countWrap}>
      <button
        ref={buttonRef}
        type="button"
        onClick={handleClick}
        className={`${styles.countChip} ${styles[`stance_${stance}`] || ''} ${open ? styles.countPinned : ''}`}
        data-testid={`stance-count-${stance}`}
        data-pinned={open ? 'true' : 'false'}
        aria-haspopup="true"
        aria-expanded={open}
        aria-label={`${label}: ${count}${hasMembers ? ', tap to view members' : ''}`}
      >
        <span className={styles.countLabel}>{label}</span>
        <span className={styles.countNum}>{count}</span>
        {hasMembers && (
          <span aria-hidden="true" className={styles.countChevron}>▾</span>
        )}
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
          {!hasMembers ? (
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
