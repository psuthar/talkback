import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { OrchestrationRecGroup } from '../components/OrchestrationRecGroup'

function renderGroup(overrides = {}) {
  const props = {
    type: 'review_draft_answer',
    count: 3,
    expanded: false,
    onToggle: vi.fn(),
    ...overrides,
  }
  return render(
    <OrchestrationRecGroup {...props}>
      <div data-testid="group-child" />
    </OrchestrationRecGroup>
  )
}

describe('OrchestrationRecGroup — header and counts (SCRUM-193)', () => {
  it('renders the human label, the count, and the toggle testid', () => {
    renderGroup()
    expect(screen.getByTestId('orchestration-group-review_draft_answer')).toBeTruthy()
    expect(screen.getByTestId('orchestration-group-toggle-review_draft_answer')).toBeTruthy()
    expect(screen.getByTestId('orchestration-group-count-review_draft_answer').textContent).toBe('3')
    // Human label, not snake_case
    expect(screen.getByText('Draft answers awaiting review')).toBeTruthy()
  })

  it('uses friendly labels for all three known types', () => {
    renderGroup({ type: 'decision_readiness' })
    expect(screen.getByText('Decision readiness')).toBeTruthy()
    renderGroup({ type: 'unanswered_question' })
    expect(screen.getByText('Unanswered questions')).toBeTruthy()
  })

  it('falls back to snake-case → space for unknown types', () => {
    renderGroup({ type: 'some_future_type' })
    expect(screen.getByText('some future type')).toBeTruthy()
  })
})

describe('OrchestrationRecGroup — collapse / expand', () => {
  it('does not render the body when collapsed', () => {
    renderGroup({ expanded: false })
    expect(screen.queryByTestId('group-child')).toBeNull()
    expect(screen.queryByTestId('orchestration-group-body-review_draft_answer')).toBeNull()
  })

  it('renders the body and children when expanded', () => {
    renderGroup({ expanded: true })
    expect(screen.getByTestId('orchestration-group-body-review_draft_answer')).toBeTruthy()
    expect(screen.getByTestId('group-child')).toBeTruthy()
  })

  it('aria-expanded mirrors the expanded prop', () => {
    const { rerender } = render(
      <OrchestrationRecGroup type="review_draft_answer" count={1} expanded={false} onToggle={() => {}}>
        <div />
      </OrchestrationRecGroup>
    )
    expect(screen.getByTestId('orchestration-group-toggle-review_draft_answer').getAttribute('aria-expanded')).toBe('false')
    rerender(
      <OrchestrationRecGroup type="review_draft_answer" count={1} expanded onToggle={() => {}}>
        <div />
      </OrchestrationRecGroup>
    )
    expect(screen.getByTestId('orchestration-group-toggle-review_draft_answer').getAttribute('aria-expanded')).toBe('true')
  })

  it('aria-controls links the toggle to the body region', () => {
    renderGroup({ expanded: true })
    const toggle = screen.getByTestId('orchestration-group-toggle-review_draft_answer')
    const body = screen.getByTestId('orchestration-group-body-review_draft_answer')
    expect(toggle.getAttribute('aria-controls')).toBe(body.id)
  })

  it('clicking the toggle invokes onToggle', () => {
    const onToggle = vi.fn()
    renderGroup({ onToggle })
    fireEvent.click(screen.getByTestId('orchestration-group-toggle-review_draft_answer'))
    expect(onToggle).toHaveBeenCalled()
  })

  it('the toggle is a real <button>, so Enter and Space activate it natively', () => {
    renderGroup()
    const toggle = screen.getByTestId('orchestration-group-toggle-review_draft_answer')
    expect(toggle.tagName).toBe('BUTTON')
    expect(toggle.getAttribute('type')).toBe('button')
  })
})

describe('OrchestrationRecGroup — bulk dismiss (SCRUM-194)', () => {
  it('does not render the bulk dismiss button when onBulkDismiss is omitted', () => {
    renderGroup()
    expect(screen.queryByTestId('orchestration-bulk-dismiss-review_draft_answer')).toBeNull()
  })

  it('does not render the bulk dismiss button when count is 0 even if onBulkDismiss is provided', () => {
    renderGroup({ count: 0, onBulkDismiss: vi.fn() })
    expect(screen.queryByTestId('orchestration-bulk-dismiss-review_draft_answer')).toBeNull()
  })

  it('renders "Dismiss all (N)" trigger when onBulkDismiss is provided and count > 0', () => {
    renderGroup({ onBulkDismiss: vi.fn() })
    const trigger = screen.getByTestId('orchestration-bulk-dismiss-review_draft_answer')
    expect(trigger.textContent).toContain('Dismiss all (3)')
  })

  it('clicking the trigger reveals an inline confirmation row, not an immediate dismiss', () => {
    const onBulkDismiss = vi.fn()
    renderGroup({ onBulkDismiss })
    fireEvent.click(screen.getByTestId('orchestration-bulk-dismiss-review_draft_answer'))
    expect(onBulkDismiss).not.toHaveBeenCalled()
    expect(screen.getByTestId('orchestration-bulk-dismiss-confirm-row-review_draft_answer')).toBeTruthy()
    expect(screen.getByText('Dismiss all (3)?')).toBeTruthy()
    expect(screen.getByTestId('orchestration-bulk-dismiss-confirm-review_draft_answer')).toBeTruthy()
    expect(screen.getByTestId('orchestration-bulk-dismiss-cancel-review_draft_answer')).toBeTruthy()
    // Original trigger is replaced while confirming
    expect(screen.queryByTestId('orchestration-bulk-dismiss-review_draft_answer')).toBeNull()
  })

  it('confirming invokes onBulkDismiss exactly once and exits confirmation mode', () => {
    const onBulkDismiss = vi.fn()
    renderGroup({ onBulkDismiss })
    fireEvent.click(screen.getByTestId('orchestration-bulk-dismiss-review_draft_answer'))
    fireEvent.click(screen.getByTestId('orchestration-bulk-dismiss-confirm-review_draft_answer'))
    expect(onBulkDismiss).toHaveBeenCalledTimes(1)
    // After confirm, the trigger is back (count is still 3 in the prop)
    expect(screen.getByTestId('orchestration-bulk-dismiss-review_draft_answer')).toBeTruthy()
    expect(screen.queryByTestId('orchestration-bulk-dismiss-confirm-row-review_draft_answer')).toBeNull()
  })

  it('cancelling exits confirmation mode without invoking onBulkDismiss', () => {
    const onBulkDismiss = vi.fn()
    renderGroup({ onBulkDismiss })
    fireEvent.click(screen.getByTestId('orchestration-bulk-dismiss-review_draft_answer'))
    fireEvent.click(screen.getByTestId('orchestration-bulk-dismiss-cancel-review_draft_answer'))
    expect(onBulkDismiss).not.toHaveBeenCalled()
    expect(screen.queryByTestId('orchestration-bulk-dismiss-confirm-row-review_draft_answer')).toBeNull()
    expect(screen.getByTestId('orchestration-bulk-dismiss-review_draft_answer')).toBeTruthy()
  })

  it('absent on decision_readiness when parent does not pass onBulkDismiss (policy enforcement)', () => {
    // The component itself doesn't enforce the per-type policy — the parent does.
    // This test documents the contract: when omitted, no bulk button renders.
    renderGroup({ type: 'decision_readiness' })
    expect(screen.queryByTestId('orchestration-bulk-dismiss-decision_readiness')).toBeNull()
  })
})
