import styles from './OrchestrationRecActions.module.css'

/**
 * OrchestrationRecActions — renders the action-button row for a single
 * recommendation card in the creator's "AI Suggested Next Actions" panel.
 *
 * Action set per recommendation_type (SCRUM-190):
 * - review_draft_answer: Approve draft (primary) + Dismiss draft (advances status to dismissed)
 * - unanswered_question: Generate draft (primary) + Mark complete + Not relevant
 * - decision_readiness with inputs_complete (and outcome form not yet open):
 *     Record outcome (primary, opens inline form). The Save outcome inside the
 *     form already advances status to a terminal state — no Mark complete here.
 * - decision_readiness without inputs_complete: Dismiss only (nothing else is actionable)
 * - any unknown recommendation_type: Mark complete + Dismiss (safe fallback)
 *
 * Visual hierarchy (SCRUM-192):
 * - The primary action per type renders as a filled-accent button matching the
 *   left-border accent (amber for review, green for decision, blue for unanswered).
 * - Secondary actions render as ghost / ghost-danger so the primary click target
 *   is obvious at a glance.
 */
export function OrchestrationRecActions({
  rec,
  actioning = false,
  outcomeFormOpen = false,
  decisionReadinessComplete = false,
  onApproveDraft,
  onDismissDraft,
  onGenerateDraft,
  onRecordOutcome,
  onMarkComplete,
  onDismiss,
}) {
  if (!rec) return null
  if (outcomeFormOpen) return null

  const recId = rec.id
  const recommendationType = rec.recommendation_type
  const isDraftReview = recommendationType === 'review_draft_answer'
  const isUnanswered = recommendationType === 'unanswered_question'
  const isDecisionReadiness = recommendationType === 'decision_readiness'
  const isUnknownType = !isDraftReview && !isUnanswered && !isDecisionReadiness

  const primaryClass = (type) => `${styles.btn} ${styles.btnPrimary} ${styles[`btnPrimary_${type}`] || ''}`
  const ghost = `${styles.btn} ${styles.btnGhost}`
  const ghostDanger = `${styles.btn} ${styles.btnGhostDanger}`

  return (
    <div className={styles.actions}>
      {isDraftReview && (
        <>
          <button
            data-testid={`orchestration-approve-${recId}`}
            type="button"
            disabled={actioning}
            onClick={onApproveDraft}
            className={primaryClass('review_draft_answer')}
          >
            {actioning ? '…' : 'Approve draft'}
          </button>
          <button
            data-testid={`orchestration-dismiss-draft-${recId}`}
            type="button"
            disabled={actioning}
            onClick={onDismissDraft}
            className={ghostDanger}
          >
            Dismiss draft
          </button>
        </>
      )}

      {isUnanswered && (
        <>
          <button
            data-testid={`orchestration-generate-${recId}`}
            type="button"
            disabled={actioning}
            onClick={onGenerateDraft}
            className={primaryClass('unanswered_question')}
          >
            Generate draft
          </button>
          <button
            data-testid={`orchestration-mark-complete-${recId}`}
            type="button"
            disabled={actioning}
            onClick={onMarkComplete}
            className={ghost}
          >
            Mark complete
          </button>
          <button
            data-testid={`orchestration-not-relevant-${recId}`}
            type="button"
            disabled={actioning}
            onClick={onDismiss}
            className={ghost}
          >
            Not relevant
          </button>
        </>
      )}

      {isDecisionReadiness && decisionReadinessComplete && (
        <button
          data-testid={`orchestration-record-outcome-${recId}`}
          type="button"
          disabled={actioning}
          onClick={onRecordOutcome}
          className={primaryClass('decision_readiness')}
        >
          Record outcome
        </button>
      )}

      {isDecisionReadiness && !decisionReadinessComplete && (
        <button
          data-testid={`orchestration-dismiss-${recId}`}
          type="button"
          disabled={actioning}
          onClick={onDismiss}
          className={ghost}
        >
          Dismiss
        </button>
      )}

      {isUnknownType && (
        <>
          <button type="button" disabled={actioning} onClick={onMarkComplete} className={ghost}>
            Mark complete
          </button>
          <button type="button" disabled={actioning} onClick={onDismiss} className={ghost}>
            Dismiss
          </button>
        </>
      )}
    </div>
  )
}
