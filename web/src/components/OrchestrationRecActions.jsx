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
 * The "Approve draft" and "Dismiss draft" handlers themselves call
 * `updateRecommendationStatus(..., 'approved' | 'dismissed')` internally, so the
 * row clears as a side effect of the primary action — no separate Mark complete
 * is needed on those cards.
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

  const baseBtnStyle = { margin: 0, padding: '4px 8px', fontSize: '12px' }
  const dismissStyle = { ...baseBtnStyle, backgroundColor: '#e0e0e0', color: '#333' }
  const dismissDraftStyle = { ...baseBtnStyle, backgroundColor: '#ef9a9a' }
  const recordOutcomeStyle = {
    ...baseBtnStyle,
    backgroundColor: 'var(--color-success-bg-hover)',
    color: '#1b5e20',
  }

  return (
    <div style={{ display: 'flex', gap: '6px', flexWrap: 'wrap' }}>
      {isDraftReview && (
        <>
          <button
            data-testid={`orchestration-approve-${recId}`}
            type="button"
            disabled={actioning}
            onClick={onApproveDraft}
            style={baseBtnStyle}
          >
            {actioning ? '…' : 'Approve draft'}
          </button>
          <button
            data-testid={`orchestration-dismiss-draft-${recId}`}
            type="button"
            disabled={actioning}
            onClick={onDismissDraft}
            style={dismissDraftStyle}
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
            style={baseBtnStyle}
          >
            Generate draft
          </button>
          <button
            data-testid={`orchestration-mark-complete-${recId}`}
            type="button"
            disabled={actioning}
            onClick={onMarkComplete}
            style={baseBtnStyle}
          >
            Mark complete
          </button>
          <button
            data-testid={`orchestration-not-relevant-${recId}`}
            type="button"
            disabled={actioning}
            onClick={onDismiss}
            style={dismissStyle}
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
          style={recordOutcomeStyle}
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
          style={dismissStyle}
        >
          Dismiss
        </button>
      )}

      {isUnknownType && (
        <>
          <button type="button" disabled={actioning} onClick={onMarkComplete} style={baseBtnStyle}>
            Mark complete
          </button>
          <button type="button" disabled={actioning} onClick={onDismiss} style={dismissStyle}>
            Dismiss
          </button>
        </>
      )}
    </div>
  )
}
