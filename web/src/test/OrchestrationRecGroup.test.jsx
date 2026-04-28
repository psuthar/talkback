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
