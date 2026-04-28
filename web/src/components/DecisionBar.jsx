import { useCallback, useEffect, useRef, useState } from 'react'
import { StanceCountTooltip } from './StanceCountTooltip'
import styles from './DecisionBar.module.css'

const STANCE_DEFS = [
  { key: 'agree', label: 'Agree' },
  { key: 'disagree', label: 'Disagree' },
  { key: 'need_more_info', label: 'Need More Info' },
]

const LEGACY_STANCE_LABELS = {
  conditional: 'Other (Conditional)',
  abstain: 'Other (Abstain)',
}

const stanceLabel = (s) => {
  const found = STANCE_DEFS.find((d) => d.key === s)
  if (found) return found.label
  if (s && LEGACY_STANCE_LABELS[s]) return LEGACY_STANCE_LABELS[s]
  return s ? `Other (${String(s).replace(/_/g, ' ')})` : 'Other'
}

/**
 * Bar that lets a participant submit / edit their stance and shows live
 * counts of every stance plus a Pending count of non-voters. Each count is a
 * hover/keyboard target that opens a tooltip listing the members in that bucket.
 */
export function DecisionBar({
  primaryDecision,
  decisionOutcome,
  myStance,
  stanceAggregate,
  stanceResponses,
  sessionInvitations,
  stanceRationale,
  setStanceRationale,
  stanceSubmitting,
  stanceFeedback,
  submitStance,
  clearStance,
  placement = 'bottom',
}) {
  const [openStanceKey, setOpenStanceKey] = useState(null)
  const handleStanceToggle = useCallback((nextKey) => {
    setOpenStanceKey(nextKey)
  }, [])

  if (!primaryDecision) return null

  const hasOutcome = !!(decisionOutcome && String(decisionOutcome).trim())
  const responses = Array.isArray(stanceResponses) ? stanceResponses : []
  const invitations = Array.isArray(sessionInvitations) ? sessionInvitations : []

  const byStance = { agree: [], disagree: [], need_more_info: [] }
  for (const r of responses) {
    if (r && byStance[r.stance]) byStance[r.stance].push(r)
  }

  const respondedEmails = new Set(
    responses
      .map((r) => (typeof r?.user_email === 'string' ? r.user_email.trim().toLowerCase() : ''))
      .filter(Boolean)
  )
  const pendingMembers = invitations
    .filter((inv) => {
      const email = typeof inv?.invited_email === 'string' ? inv.invited_email.trim().toLowerCase() : ''
      return email && !respondedEmails.has(email)
    })
    .map((inv) => ({ id: inv.id, user_email: inv.invited_email }))

  const barClass =
    placement === 'top' ? `${styles.bar} ${styles.barTop}` : `${styles.bar} ${styles.barBottom}`

  return (
    <section
      className={barClass}
      aria-label="Decision controls"
      data-testid="decision-bar"
    >
      <div className={styles.combinedRow}>
        <div className={styles.stanceCol}>
          <span className={styles.rowLabel}>Your stance</span>
          <div className={styles.rowBody}>
            {hasOutcome ? (
              <span className={styles.lockedNote} data-testid="decision-bar-locked">
                Outcome recorded — stances are locked.
              </span>
            ) : (
              <StanceDropdown
                myStance={myStance}
                stanceRationale={stanceRationale}
                setStanceRationale={setStanceRationale}
                stanceSubmitting={stanceSubmitting}
                onSubmit={submitStance}
                onClear={clearStance}
              />
            )}
          </div>
        </div>

        <div className={styles.othersCol}>
          <span className={styles.rowLabel}>Others</span>
          <div className={styles.countsRow}>
            {STANCE_DEFS.map((d) => (
              <StanceCountTooltip
                key={d.key}
                stance={d.key}
                label={d.label}
                count={Number(stanceAggregate?.[d.key] ?? 0)}
                members={byStance[d.key]}
                isOpen={openStanceKey === d.key}
                onToggle={handleStanceToggle}
              />
            ))}
            <StanceCountTooltip
              key="pending"
              stance="pending"
              label="Pending"
              count={pendingMembers.length}
              members={pendingMembers}
              isOpen={openStanceKey === 'pending'}
              onToggle={handleStanceToggle}
            />
          </div>
        </div>
      </div>

      {stanceFeedback?.message && (
        <p
          className={`${styles.feedback} ${stanceFeedback.type === 'error' ? styles.feedbackError : styles.feedbackSuccess}`}
          data-testid="decision-bar-feedback"
        >
          {stanceFeedback.message}
        </p>
      )}
    </section>
  )
}

function StanceDropdown({
  myStance,
  stanceRationale,
  setStanceRationale,
  stanceSubmitting,
  onSubmit,
  onClear,
}) {
  const [open, setOpen] = useState(false)
  const triggerRef = useRef(null)
  const menuRef = useRef(null)
  const submitted = !!myStance?.stance

  useEffect(() => {
    if (!open) return

    const handlePointerDown = (event) => {
      const t = triggerRef.current
      const m = menuRef.current
      if (t && t.contains(event.target)) return
      if (m && m.contains(event.target)) return
      setOpen(false)
    }
    const handleKeyDown = (event) => {
      if (event.key === 'Escape') {
        event.stopPropagation()
        setOpen(false)
        triggerRef.current?.focus()
      }
    }

    document.addEventListener('mousedown', handlePointerDown)
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('mousedown', handlePointerDown)
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [open])

  useEffect(() => {
    if (!open || !menuRef.current) return
    const items = menuRef.current.querySelectorAll('[role="menuitem"]:not(:disabled)')
    items[0]?.focus()
  }, [open])

  const handleMenuKeyDown = (event) => {
    if (!menuRef.current) return
    const items = Array.from(menuRef.current.querySelectorAll('[role="menuitem"]:not(:disabled)'))
    if (items.length === 0) return
    const currentIdx = items.indexOf(document.activeElement)
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      const next = items[currentIdx < 0 ? 0 : (currentIdx + 1) % items.length]
      next?.focus()
    } else if (event.key === 'ArrowUp') {
      event.preventDefault()
      const prev = items[currentIdx <= 0 ? items.length - 1 : currentIdx - 1]
      prev?.focus()
    } else if (event.key === 'Home') {
      event.preventDefault()
      items[0]?.focus()
    } else if (event.key === 'End') {
      event.preventDefault()
      items[items.length - 1]?.focus()
    }
  }

  const handleSelect = async (stanceKey) => {
    setOpen(false)
    triggerRef.current?.focus()
    await onSubmit?.(stanceKey)
  }

  const handleClear = async () => {
    setOpen(false)
    triggerRef.current?.focus()
    await onClear?.()
  }
  const handleCancelEdit = () => {
    setOpen(false)
    triggerRef.current?.focus()
  }

  const triggerLabel = submitted ? `✓ ${stanceLabel(myStance.stance)}` : 'Choose stance…'
  const triggerAriaLabel = submitted
    ? `Your stance is ${stanceLabel(myStance.stance)}. Open menu to change.`
    : 'Choose your stance'

  const triggerTestId = submitted ? 'stance-edit-btn' : 'stance-trigger-btn'

  return (
    <div className={styles.dropdownBlock}>
      <div className={styles.triggerRow} data-testid={submitted ? 'decision-bar-submitted' : undefined}>
        <button
          ref={triggerRef}
          type="button"
          data-testid={triggerTestId}
          aria-haspopup="menu"
          aria-expanded={open}
          aria-label={triggerAriaLabel}
          onClick={() => setOpen((v) => !v)}
          disabled={stanceSubmitting}
          className={`${styles.trigger} ${submitted ? `${styles.triggerSubmitted} ${styles[`stancePill_${myStance.stance}`] || ''}` : styles.triggerEmpty}`}
        >
          <span className={styles.triggerLabel}>{triggerLabel}</span>
          <span aria-hidden="true" className={styles.triggerChevron}>▾</span>
        </button>
        {!submitted && (
          <span className={styles.triggerHelper}>You haven&rsquo;t submitted a stance yet</span>
        )}
        <div className={styles.rationaleBlock}>
          <textarea
            placeholder="Rationale (optional)"
            value={stanceRationale ?? ''}
            onChange={(e) => setStanceRationale(e.target.value.slice(0, 500))}
            className={styles.rationaleInput}
            data-testid="stance-rationale-input"
            aria-label="Stance rationale"
            rows={1}
          />
          {submitted && stanceRationale !== (myStance?.rationale ?? '') && (
            <button
              type="button"
              data-testid="stance-save-rationale-btn"
              onClick={() => onSubmit?.(myStance.stance)}
              disabled={stanceSubmitting}
              className={styles.saveRationaleBtn}
            >
              {stanceSubmitting ? 'Saving…' : 'Save rationale'}
            </button>
          )}
        </div>
      </div>

      {open && (
        <ul
          ref={menuRef}
          role="menu"
          aria-label="Stance options"
          className={styles.menu}
          data-testid="stance-menu"
          onKeyDown={handleMenuKeyDown}
        >
          {STANCE_DEFS.map((d) => {
            const selected = myStance?.stance === d.key
            return (
              <li role="none" key={d.key}>
                <button
                  role="menuitem"
                  type="button"
                  data-testid={`stance-btn-${d.key}`}
                  data-selected={selected ? 'true' : 'false'}
                  onClick={() => handleSelect(d.key)}
                  disabled={stanceSubmitting}
                  className={`${styles.menuItem} ${styles[`menuItem_${d.key}`] || ''} ${selected ? styles.menuItemSelected : ''}`}
                >
                  <span aria-hidden="true" className={styles.menuItemMark}>{selected ? '✓' : ''}</span>
                  <span>{d.label}</span>
                </button>
              </li>
            )
          })}
          {submitted && (
            <>
              <li role="none">
                <button
                  role="menuitem"
                  type="button"
                  data-testid="stance-clear-btn"
                  onClick={handleClear}
                  disabled={stanceSubmitting}
                  className={styles.menuItemClear}
                >
                  Clear stance
                </button>
              </li>
              <li role="none">
                <button
                  role="menuitem"
                  type="button"
                  data-testid="stance-cancel-edit-btn"
                  onClick={handleCancelEdit}
                  disabled={stanceSubmitting}
                  className={styles.menuItem}
                >
                  Cancel
                </button>
              </li>
            </>
          )}
        </ul>
      )}
    </div>
  )
}
