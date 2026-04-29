import styles from './DecisionBriefHeader.module.css'

function readinessCopy(readiness) {
  if (!readiness) return null
  const total = Number(readiness.decision_maker_total) || 0
  const voted = Number(readiness.decision_maker_voted) || 0
  if (total === 0) return 'No Decision Makers assigned'
  const base = `${voted}/${total} Decision Makers submitted`
  return readiness.ready_to_close ? `${base} — ready to close` : base
}

export function DecisionBriefHeader({ premise, decision, decisionOutcome, readiness }) {
  const hasPremise = !!(premise && String(premise).trim())
  const hasDecision = !!(decision && String(decision).trim())
  const hasOutcome = !!(decisionOutcome && String(decisionOutcome).trim())
  // Readiness only renders when there's a decision question to vote on AND the outcome
  // is not yet recorded — once the decision is closed, the outcome row carries the signal.
  const showReadiness = !!readiness && hasDecision && !hasOutcome
  const readinessText = showReadiness ? readinessCopy(readiness) : null
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
      </div>
    </section>
  )
}
