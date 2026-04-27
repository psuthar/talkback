import { useState } from 'react'
import { StanceCountTooltip } from './StanceCountTooltip'
import styles from './DecisionBar.module.css'

const STANCE_DEFS = [
  { key: 'agree', label: 'Agree' },
  { key: 'disagree', label: 'Disagree' },
  { key: 'conditional', label: 'Conditional' },
  { key: 'abstain', label: 'Abstain' },
  { key: 'need_more_info', label: 'Need More Info' },
]

const stanceLabel = (s) => {
  const found = STANCE_DEFS.find((d) => d.key === s)
  return found ? found.label : (s || '').replace(/_/g, ' ')
}

/**
 * Always-visible bottom bar that lets a participant submit / edit their
 * stance and shows live counts of every stance plus a Pending count of
 * non-voters. Each count is a hover/keyboard target that opens a tooltip
 * listing the members in that bucket.
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
}) {
  const [editing, setEditing] = useState(false)

  if (!primaryDecision) return null

  const hasOutcome = !!(decisionOutcome && String(decisionOutcome).trim())
  const showStanceForm = !myStance?.stance || editing
  const responses = Array.isArray(stanceResponses) ? stanceResponses : []
  const invitations = Array.isArray(sessionInvitations) ? sessionInvitations : []

  // Group responses per stance for tooltip member lists
  const byStance = { agree: [], disagree: [], conditional: [], abstain: [], need_more_info: [] }
  for (const r of responses) {
    if (r && byStance[r.stance]) byStance[r.stance].push(r)
  }

  // Pending = invited members whose email is not in stanceResponses
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

  const handleSubmit = async (stanceKey) => {
    await submitStance?.(stanceKey)
    setEditing(false)
  }

  const handleClear = async () => {
    await clearStance?.()
    setEditing(false)
  }

  return (
    <section
      className={styles.bar}
      aria-label="Decision controls"
      data-testid="decision-bar"
    >
      <div className={styles.row}>
        <span className={styles.rowLabel}>Your stance</span>
        <div className={styles.rowBody}>
          {hasOutcome ? (
            <span className={styles.lockedNote} data-testid="decision-bar-locked">
              Outcome recorded — stances are locked.
            </span>
          ) : showStanceForm ? (
            <StanceForm
              myStance={myStance}
              stanceRationale={stanceRationale}
              setStanceRationale={setStanceRationale}
              stanceSubmitting={stanceSubmitting}
              onSubmit={handleSubmit}
              onClear={handleClear}
              onCancelEdit={() => setEditing(false)}
              isEditing={editing}
            />
          ) : (
            <SubmittedView
              myStance={myStance}
              onEdit={() => setEditing(true)}
            />
          )}
        </div>
      </div>

      <div className={styles.row}>
        <span className={styles.rowLabel}>Others</span>
        <div className={styles.countsRow}>
          {STANCE_DEFS.map((d) => (
            <StanceCountTooltip
              key={d.key}
              stance={d.key}
              label={d.label}
              count={Number(stanceAggregate?.[d.key] ?? 0)}
              members={byStance[d.key]}
            />
          ))}
          <StanceCountTooltip
            key="pending"
            stance="pending"
            label="Pending"
            count={pendingMembers.length}
            members={pendingMembers}
          />
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

function StanceForm({
  myStance,
  stanceRationale,
  setStanceRationale,
  stanceSubmitting,
  onSubmit,
  onClear,
  onCancelEdit,
  isEditing,
}) {
  return (
    <div className={styles.formBlock}>
      <div className={styles.stanceBtnsGroup}>
        {STANCE_DEFS.map((d) => {
          const selected = myStance?.stance === d.key
          return (
            <button
              key={d.key}
              type="button"
              data-testid={`stance-btn-${d.key}`}
              data-selected={selected ? 'true' : 'false'}
              onClick={() => onSubmit(d.key)}
              disabled={stanceSubmitting}
              className={`${styles.stanceBtn} ${styles[`stanceBtn_${d.key}`] || ''} ${selected ? styles.stanceBtnSelected : ''}`}
            >
              {d.label}
            </button>
          )
        })}
        {(myStance?.stance || (stanceRationale && stanceRationale.trim())) && (
          <button
            type="button"
            data-testid="stance-clear-btn"
            onClick={onClear}
            disabled={stanceSubmitting}
            className={styles.clearBtn}
          >
            Clear
          </button>
        )}
        {isEditing && (
          <button
            type="button"
            data-testid="stance-cancel-edit-btn"
            onClick={onCancelEdit}
            disabled={stanceSubmitting}
            className={styles.cancelBtn}
          >
            Cancel
          </button>
        )}
      </div>
      <input
        type="text"
        placeholder="Rationale (optional)"
        value={stanceRationale ?? ''}
        onChange={(e) => setStanceRationale(e.target.value.slice(0, 500))}
        className={styles.rationaleInput}
        data-testid="stance-rationale-input"
      />
      {myStance?.stance && stanceRationale !== (myStance?.rationale ?? '') && (
        <button
          type="button"
          data-testid="stance-save-rationale-btn"
          onClick={() => onSubmit(myStance.stance)}
          disabled={stanceSubmitting}
          className={styles.saveRationaleBtn}
        >
          {stanceSubmitting ? 'Saving…' : 'Save rationale'}
        </button>
      )}
    </div>
  )
}

function SubmittedView({ myStance, onEdit }) {
  return (
    <div className={styles.submittedBlock} data-testid="decision-bar-submitted">
      <span className={`${styles.submittedStance} ${styles[`stancePill_${myStance.stance}`] || ''}`}>
        ✓ {stanceLabel(myStance.stance)}
      </span>
      {myStance?.rationale && String(myStance.rationale).trim() && (
        <span className={styles.submittedRationale}>“{String(myStance.rationale).trim()}”</span>
      )}
      <button
        type="button"
        data-testid="stance-edit-btn"
        onClick={onEdit}
        className={styles.editBtn}
      >
        Edit ▾
      </button>
    </div>
  )
}
