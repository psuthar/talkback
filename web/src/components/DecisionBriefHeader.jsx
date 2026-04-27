import styles from './DecisionBriefHeader.module.css'

export function DecisionBriefHeader({ premise, decision, decisionOutcome }) {
  const hasPremise = !!(premise && String(premise).trim())
  const hasDecision = !!(decision && String(decision).trim())
  const hasOutcome = !!(decisionOutcome && String(decisionOutcome).trim())
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
      </div>
    </section>
  )
}
