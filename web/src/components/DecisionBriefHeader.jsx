import { useState } from 'react'
import styles from './DecisionBriefHeader.module.css'

function readinessCopy(readiness) {
  if (!readiness) return null
  const total = Number(readiness.decision_maker_total) || 0
  const voted = Number(readiness.decision_maker_voted) || 0
  if (total === 0) return 'No Decision Makers assigned'
  const base = `${voted}/${total} Decision Makers submitted`
  return readiness.ready_to_close ? `${base} — ready to close` : base
}

// CloseDecisionAction renders an inline closeout CTA when the parent provides
// an onCloseDecision callback and the gate is satisfied. The parent is
// responsible for the actual PATCH and for hiding the action when the caller
// is not authorized (creator/admin) — the server enforces auth as a backstop.
function CloseDecisionAction({ onCloseDecision }) {
  const [open, setOpen] = useState(false)
  const [text, setText] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  if (!onCloseDecision) return null

  if (!open) {
    return (
      <button
        type="button"
        data-testid="close-decision-btn"
        className={styles.closeDecisionBtn}
        onClick={() => {
          setOpen(true)
          setError('')
        }}
      >
        Close decision
      </button>
    )
  }

  const trimmed = text.trim()
  const onSubmit = async (e) => {
    e?.preventDefault?.()
    if (!trimmed) {
      setError('Enter the decision outcome before closing.')
      return
    }
    setSubmitting(true)
    setError('')
    try {
      const res = await onCloseDecision(trimmed)
      if (res && res.ok === false) {
        setError(res.error || 'Failed to close decision.')
        return
      }
      setOpen(false)
      setText('')
    } catch (err) {
      setError(err?.message || 'Failed to close decision.')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <form className={styles.closeDecisionForm} data-testid="close-decision-form" onSubmit={onSubmit}>
      <textarea
        className={styles.closeDecisionInput}
        data-testid="close-decision-input"
        aria-label="Decision outcome"
        placeholder="What was decided?"
        rows={2}
        maxLength={500}
        value={text}
        onChange={(e) => setText(e.target.value)}
        disabled={submitting}
      />
      <div className={styles.closeDecisionActions}>
        <button
          type="submit"
          data-testid="close-decision-confirm"
          className={styles.closeDecisionConfirm}
          disabled={submitting || !trimmed}
        >
          {submitting ? 'Closing…' : 'Record outcome'}
        </button>
        <button
          type="button"
          data-testid="close-decision-cancel"
          className={styles.closeDecisionCancel}
          onClick={() => {
            setOpen(false)
            setText('')
            setError('')
          }}
          disabled={submitting}
        >
          Cancel
        </button>
      </div>
      {error && (
        <p className={styles.closeDecisionError} data-testid="close-decision-error">{error}</p>
      )}
    </form>
  )
}

export function DecisionBriefHeader({ premise, decision, decisionOutcome, readiness, onCloseDecision }) {
  const hasPremise = !!(premise && String(premise).trim())
  const hasDecision = !!(decision && String(decision).trim())
  const hasOutcome = !!(decisionOutcome && String(decisionOutcome).trim())
  // Readiness only renders when there's a decision question to vote on AND the outcome
  // is not yet recorded — once the decision is closed, the outcome row carries the signal.
  const showReadiness = !!readiness && hasDecision && !hasOutcome
  const readinessText = showReadiness ? readinessCopy(readiness) : null
  // The close-decision action only appears when the caller (creator) has provided a callback,
  // a decision question is open, and readiness reports ready_to_close=true.
  const showCloseAction = !!onCloseDecision && hasDecision && !hasOutcome && !!readiness?.ready_to_close
  if (!hasPremise && !hasDecision && !hasOutcome) return null

  return (
    <section className={styles.brief} aria-label="Decision brief" data-testid="decision-brief-header">
      <div className={styles.briefRows}>
        {hasPremise && (
          <div className={styles.briefRow}>
            <span className={styles.briefLabel}>Premise</span>
            <span className={styles.briefText}>{premise}</span>
          </div>
        )}
        {hasDecision && (
          <div className={styles.briefRow}>
            <span className={styles.briefLabel}>Decision</span>
            <span className={styles.briefDecisionText}>{decision}</span>
          </div>
        )}
        {hasOutcome && (
          <div className={styles.briefRow}>
            <span className={styles.briefLabel}>Outcome</span>
            <span className={styles.outcomeBadge} data-testid="decision-outcome-badge">
              Decision: {decisionOutcome}
            </span>
          </div>
        )}
        {showReadiness && readinessText && (
          <div className={styles.briefRow}>
            <span className={styles.briefLabel}>Votes</span>
            <span
              className={readiness.ready_to_close ? styles.readinessTextReady : styles.readinessText}
              data-testid="decision-readiness"
              data-ready={readiness.ready_to_close ? 'true' : 'false'}
            >
              {readinessText}
            </span>
          </div>
        )}
        {showCloseAction && (
          <div className={styles.briefRow}>
            <span className={styles.briefLabel}>Close</span>
            <div className={styles.closeDecisionWrap}>
              <CloseDecisionAction onCloseDecision={onCloseDecision} />
            </div>
          </div>
        )}
      </div>
    </section>
  )
}
