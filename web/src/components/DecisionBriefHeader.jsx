import styles from './DecisionBriefHeader.module.css'

export function DecisionBriefHeader({
  premise,
  decision,
  decisionOutcome,
  videoStarted = false,
  qaCount = 0,
  stanceSubmitted = false,
}) {
  const hasPremise = !!(premise && String(premise).trim())
  const hasDecision = !!(decision && String(decision).trim())
  const hasOutcome = !!(decisionOutcome && String(decisionOutcome).trim())
  if (!hasPremise && !hasDecision && !hasOutcome) return null

  const chips = [
    { label: 'Review video', done: !!videoStarted },
    { label: 'Ask questions', done: (qaCount || 0) > 0 },
    { label: 'Decide', done: !!stanceSubmitted },
  ]

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
        <div className={styles.briefRow}>
          <span className={styles.briefLabel}>Your task</span>
          {hasOutcome ? (
            <span className={styles.outcomeBadge} data-testid="decision-outcome-badge">
              Decision: {decisionOutcome}
            </span>
          ) : (
            <ol className={styles.chipList} aria-label="Participant progress">
              {chips.map((chip, idx) => (
                <li
                  key={chip.label}
                  className={`${styles.chip} ${chip.done ? styles.chipDone : styles.chipPending}`}
                  data-testid={`decision-chip-${idx + 1}`}
                  data-done={chip.done ? 'true' : 'false'}
                >
                  <span className={styles.chipNum} aria-hidden>{idx + 1}</span>
                  <span className={styles.chipLabel}>{chip.label}</span>
                  {chip.done && <span className={styles.chipCheck} aria-hidden>✓</span>}
                </li>
              ))}
            </ol>
          )}
        </div>
      </div>
    </section>
  )
}
