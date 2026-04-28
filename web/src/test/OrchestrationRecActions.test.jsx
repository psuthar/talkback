import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { OrchestrationRecActions } from '../components/OrchestrationRecActions'

function makeRec(overrides = {}) {
  return { id: 'rec-1', recommendation_type: 'review_draft_answer', ...overrides }
}

function makeHandlers() {
  return {
    onApproveDraft: vi.fn(),
    onDismissDraft: vi.fn(),
    onGenerateDraft: vi.fn(),
    onRecordOutcome: vi.fn(),
    onMarkComplete: vi.fn(),
    onDismiss: vi.fn(),
  }
}

describe('OrchestrationRecActions — review_draft_answer', () => {
  it('renders only Approve draft and Dismiss draft (no generic Mark complete or Dismiss)', () => {
    render(<OrchestrationRecActions rec={makeRec()} {...makeHandlers()} />)
    expect(screen.getByTestId('orchestration-approve-rec-1')).toBeTruthy()
    expect(screen.getByTestId('orchestration-dismiss-draft-rec-1')).toBeTruthy()
    expect(screen.queryByText('Mark complete')).toBeNull()
    expect(screen.queryByText(/^Dismiss$/)).toBeNull()
    expect(screen.queryByText('Not relevant')).toBeNull()
  })

  it('Approve draft invokes onApproveDraft, not onMarkComplete', () => {
    const handlers = makeHandlers()
    render(<OrchestrationRecActions rec={makeRec()} {...handlers} />)
    fireEvent.click(screen.getByTestId('orchestration-approve-rec-1'))
    expect(handlers.onApproveDraft).toHaveBeenCalled()
    expect(handlers.onMarkComplete).not.toHaveBeenCalled()
  })

  it('Dismiss draft invokes onDismissDraft, not onDismiss', () => {
    const handlers = makeHandlers()
    render(<OrchestrationRecActions rec={makeRec()} {...handlers} />)
    fireEvent.click(screen.getByTestId('orchestration-dismiss-draft-rec-1'))
    expect(handlers.onDismissDraft).toHaveBeenCalled()
    expect(handlers.onDismiss).not.toHaveBeenCalled()
  })

  it('disables both buttons when actioning is true', () => {
    render(<OrchestrationRecActions rec={makeRec()} actioning {...makeHandlers()} />)
    expect(screen.getByTestId('orchestration-approve-rec-1').disabled).toBe(true)
    expect(screen.getByTestId('orchestration-dismiss-draft-rec-1').disabled).toBe(true)
  })
})

describe('OrchestrationRecActions — unanswered_question', () => {
  it('renders Generate draft, Mark complete, and Not relevant (renamed from Dismiss)', () => {
    render(
      <OrchestrationRecActions
        rec={makeRec({ recommendation_type: 'unanswered_question' })}
        {...makeHandlers()}
      />
    )
    expect(screen.getByTestId('orchestration-generate-rec-1')).toBeTruthy()
    expect(screen.getByTestId('orchestration-mark-complete-rec-1')).toBeTruthy()
    expect(screen.getByTestId('orchestration-not-relevant-rec-1')).toBeTruthy()
    expect(screen.getByText('Not relevant')).toBeTruthy()
    // Plain "Dismiss" copy must not appear; the secondary action is "Not relevant"
    expect(screen.queryByText(/^Dismiss$/)).toBeNull()
  })

  it('Mark complete invokes onMarkComplete; Not relevant invokes onDismiss', () => {
    const handlers = makeHandlers()
    render(
      <OrchestrationRecActions
        rec={makeRec({ recommendation_type: 'unanswered_question' })}
        {...handlers}
      />
    )
    fireEvent.click(screen.getByTestId('orchestration-mark-complete-rec-1'))
    expect(handlers.onMarkComplete).toHaveBeenCalled()
    fireEvent.click(screen.getByTestId('orchestration-not-relevant-rec-1'))
    expect(handlers.onDismiss).toHaveBeenCalled()
  })
})

describe('OrchestrationRecActions — decision_readiness', () => {
  it('renders Record outcome (only) when inputs are complete and outcome form is closed', () => {
    render(
      <OrchestrationRecActions
        rec={makeRec({ recommendation_type: 'decision_readiness' })}
        decisionReadinessComplete
        {...makeHandlers()}
      />
    )
    expect(screen.getByTestId('orchestration-record-outcome-rec-1')).toBeTruthy()
    // No Mark complete or generic Dismiss — Save outcome inside the form will advance status.
    expect(screen.queryByText('Mark complete')).toBeNull()
    expect(screen.queryByTestId('orchestration-dismiss-rec-1')).toBeNull()
  })

  it('renders nothing when the inline outcome form is open (form supplies its own buttons)', () => {
    const { container } = render(
      <OrchestrationRecActions
        rec={makeRec({ recommendation_type: 'decision_readiness' })}
        decisionReadinessComplete
        outcomeFormOpen
        {...makeHandlers()}
      />
    )
    expect(container.firstChild).toBeNull()
  })

  it('renders only Dismiss when inputs are not complete', () => {
    render(
      <OrchestrationRecActions
        rec={makeRec({ recommendation_type: 'decision_readiness' })}
        decisionReadinessComplete={false}
        {...makeHandlers()}
      />
    )
    expect(screen.getByTestId('orchestration-dismiss-rec-1')).toBeTruthy()
    expect(screen.queryByTestId('orchestration-record-outcome-rec-1')).toBeNull()
    expect(screen.queryByText('Mark complete')).toBeNull()
  })

  it('Record outcome invokes onRecordOutcome, not onMarkComplete', () => {
    const handlers = makeHandlers()
    render(
      <OrchestrationRecActions
        rec={makeRec({ recommendation_type: 'decision_readiness' })}
        decisionReadinessComplete
        {...handlers}
      />
    )
    fireEvent.click(screen.getByTestId('orchestration-record-outcome-rec-1'))
    expect(handlers.onRecordOutcome).toHaveBeenCalled()
    expect(handlers.onMarkComplete).not.toHaveBeenCalled()
  })
})

describe('OrchestrationRecActions — unknown recommendation_type fallback', () => {
  it('renders generic Mark complete + Dismiss when type is unknown', () => {
    const handlers = makeHandlers()
    render(
      <OrchestrationRecActions
        rec={makeRec({ recommendation_type: 'something_new_we_dont_know_about' })}
        {...handlers}
      />
    )
    expect(screen.getByText('Mark complete')).toBeTruthy()
    expect(screen.getByText('Dismiss')).toBeTruthy()
    fireEvent.click(screen.getByText('Mark complete'))
    expect(handlers.onMarkComplete).toHaveBeenCalled()
    fireEvent.click(screen.getByText('Dismiss'))
    expect(handlers.onDismiss).toHaveBeenCalled()
  })
})

describe('OrchestrationRecActions — guards', () => {
  it('returns null when rec is missing', () => {
    const { container } = render(<OrchestrationRecActions rec={null} {...makeHandlers()} />)
    expect(container.firstChild).toBeNull()
  })
})

describe('OrchestrationRecActions — primary vs ghost styling (SCRUM-192)', () => {
  it('Approve draft is primary (review_draft_answer accent); Dismiss draft is ghost-danger', () => {
    render(<OrchestrationRecActions rec={makeRec()} {...makeHandlers()} />)
    const approve = screen.getByTestId('orchestration-approve-rec-1')
    expect(approve.className).toMatch(/btnPrimary/)
    expect(approve.className).toMatch(/btnPrimary_review_draft_answer/)
    const dismissDraft = screen.getByTestId('orchestration-dismiss-draft-rec-1')
    expect(dismissDraft.className).toMatch(/btnGhostDanger/)
    expect(dismissDraft.className).not.toMatch(/btnPrimary/)
  })

  it('Generate draft is primary (unanswered_question accent); Mark complete and Not relevant are ghost', () => {
    render(
      <OrchestrationRecActions
        rec={makeRec({ recommendation_type: 'unanswered_question' })}
        {...makeHandlers()}
      />
    )
    const generate = screen.getByTestId('orchestration-generate-rec-1')
    expect(generate.className).toMatch(/btnPrimary_unanswered_question/)
    const markComplete = screen.getByTestId('orchestration-mark-complete-rec-1')
    expect(markComplete.className).toMatch(/btnGhost/)
    expect(markComplete.className).not.toMatch(/btnPrimary/)
    const notRelevant = screen.getByTestId('orchestration-not-relevant-rec-1')
    expect(notRelevant.className).toMatch(/btnGhost/)
    expect(notRelevant.className).not.toMatch(/btnPrimary/)
  })

  it('Record outcome is primary (decision_readiness accent)', () => {
    render(
      <OrchestrationRecActions
        rec={makeRec({ recommendation_type: 'decision_readiness' })}
        decisionReadinessComplete
        {...makeHandlers()}
      />
    )
    const recordOutcome = screen.getByTestId('orchestration-record-outcome-rec-1')
    expect(recordOutcome.className).toMatch(/btnPrimary_decision_readiness/)
  })

  it('the decision_readiness Dismiss (when inputs incomplete) is ghost, not danger', () => {
    render(
      <OrchestrationRecActions
        rec={makeRec({ recommendation_type: 'decision_readiness' })}
        decisionReadinessComplete={false}
        {...makeHandlers()}
      />
    )
    const dismiss = screen.getByTestId('orchestration-dismiss-rec-1')
    expect(dismiss.className).toMatch(/btnGhost/)
    expect(dismiss.className).not.toMatch(/btnGhostDanger/)
    expect(dismiss.className).not.toMatch(/btnPrimary/)
  })

  it('disabled primary button is still primary-shaped (CSS handles the dimmed look)', () => {
    render(<OrchestrationRecActions rec={makeRec()} actioning {...makeHandlers()} />)
    const approve = screen.getByTestId('orchestration-approve-rec-1')
    expect(approve.disabled).toBe(true)
    // Class is still applied; .btn:disabled in the module dims it via opacity.
    expect(approve.className).toMatch(/btnPrimary_review_draft_answer/)
  })
})
