import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { OrchestrationRecCard } from '../components/OrchestrationRecCard'

function makeRec(overrides = {}) {
  return {
    id: 'rec-1',
    recommendation_type: 'review_draft_answer',
    status: 'new',
    summary: 'Review the draft answer',
    suggested_action: 'Approve, dismiss, or rework the generated draft.',
    ...overrides,
  }
}

describe('OrchestrationRecCard — visual chrome (SCRUM-191)', () => {
  it('renders the summary, suggested_action, and children', () => {
    render(
      <OrchestrationRecCard rec={makeRec()}>
        <div data-testid="children-marker" />
      </OrchestrationRecCard>
    )
    expect(screen.getByTestId('orchestration-rec-rec-1')).toBeTruthy()
    expect(screen.getByText('Review the draft answer')).toBeTruthy()
    expect(screen.getByText('Approve, dismiss, or rework the generated draft.')).toBeTruthy()
    // SCRUM-193: per-card uppercase type label removed because the group header
    // now carries it.
    expect(screen.queryByText('review draft answer')).toBeNull()
    // Children render below the chrome
    expect(screen.getByTestId('children-marker')).toBeTruthy()
  })

  it('omits suggested_action when the field is empty', () => {
    render(<OrchestrationRecCard rec={makeRec({ suggested_action: '' })} />)
    expect(screen.queryByText('Approve, dismiss, or rework the generated draft.')).toBeNull()
  })

  it('returns null when rec is missing', () => {
    const { container } = render(<OrchestrationRecCard rec={null} />)
    expect(container.firstChild).toBeNull()
  })
})

describe('OrchestrationRecCard — left-border accent', () => {
  function getCardClassList(rec) {
    render(<OrchestrationRecCard rec={rec} />)
    return screen.getByTestId('orchestration-rec-rec-1').className
  }

  it('uses the review_draft_answer accent class', () => {
    expect(getCardClassList(makeRec({ recommendation_type: 'review_draft_answer' }))).toMatch(/card_review_draft_answer/)
  })

  it('uses the decision_readiness accent class', () => {
    expect(getCardClassList(makeRec({ recommendation_type: 'decision_readiness' }))).toMatch(/card_decision_readiness/)
  })

  it('uses the unanswered_question accent class', () => {
    expect(getCardClassList(makeRec({ recommendation_type: 'unanswered_question' }))).toMatch(/card_unanswered_question/)
  })

  it('falls back to the unknown accent class for other recommendation_types', () => {
    expect(getCardClassList(makeRec({ recommendation_type: 'some_future_type' }))).toMatch(/card_unknown/)
  })

  it('exposes the recommendation_type via data-rec-type for E2E targeting', () => {
    render(<OrchestrationRecCard rec={makeRec({ recommendation_type: 'decision_readiness' })} />)
    expect(screen.getByTestId('orchestration-rec-rec-1').getAttribute('data-rec-type')).toBe('decision_readiness')
  })
})

describe('OrchestrationRecCard — selective status pill', () => {
  it('does NOT render the status pill for the default `new` state', () => {
    render(<OrchestrationRecCard rec={makeRec({ status: 'new' })} />)
    expect(screen.queryByTestId('orchestration-status-rec-1')).toBeNull()
  })

  it('renders the status pill for status=approved', () => {
    render(<OrchestrationRecCard rec={makeRec({ status: 'approved' })} />)
    const pill = screen.getByTestId('orchestration-status-rec-1')
    expect(pill).toBeTruthy()
    expect(pill.textContent).toContain('approved')
    expect(pill.className).toMatch(/statusPill_approved/)
  })

  it('renders the status pill for status=dismissed with the dismissed accent', () => {
    render(<OrchestrationRecCard rec={makeRec({ status: 'dismissed' })} />)
    const pill = screen.getByTestId('orchestration-status-rec-1')
    expect(pill).toBeTruthy()
    expect(pill.className).toMatch(/statusPill_dismissed/)
  })

  it('renders the status pill for status=completed with the completed accent', () => {
    render(<OrchestrationRecCard rec={makeRec({ status: 'completed' })} />)
    const pill = screen.getByTestId('orchestration-status-rec-1')
    expect(pill).toBeTruthy()
    expect(pill.className).toMatch(/statusPill_completed/)
  })

  it('does NOT render the pill for unknown statuses (e.g. server adds a new status string)', () => {
    render(<OrchestrationRecCard rec={makeRec({ status: 'in_review' })} />)
    expect(screen.queryByTestId('orchestration-status-rec-1')).toBeNull()
  })
})

describe('OrchestrationRecCard — preserves data-testid contract', () => {
  it('renders the orchestration-rec-${id} testid', () => {
    render(<OrchestrationRecCard rec={makeRec({ id: 'abc-123' })} />)
    expect(screen.getByTestId('orchestration-rec-abc-123')).toBeTruthy()
  })
})
