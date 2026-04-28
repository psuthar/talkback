import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { DecisionBar } from '../components/DecisionBar'

function makeProps(overrides = {}) {
  return {
    primaryDecision: 'Should we extend an offer?',
    decisionOutcome: null,
    myStance: null,
    stanceAggregate: { agree: 0, disagree: 0, conditional: 0, abstain: 0, need_more_info: 0 },
    stanceResponses: [],
    sessionInvitations: [],
    stanceRationale: '',
    setStanceRationale: vi.fn(),
    stanceSubmitting: false,
    stanceFeedback: { type: '', message: '' },
    submitStance: vi.fn(),
    clearStance: vi.fn(),
    ...overrides,
  }
}

describe('DecisionBar', () => {
  it('returns null when there is no primary decision', () => {
    const { container } = render(<DecisionBar {...makeProps({ primaryDecision: '' })} />)
    expect(container.firstChild).toBeNull()
  })

  it('State A: shows a "Choose stance…" trigger and rationale input when no stance is submitted', () => {
    render(<DecisionBar {...makeProps()} />)
    const trigger = screen.getByTestId('stance-trigger-btn')
    expect(trigger).toBeTruthy()
    expect(trigger.textContent).toMatch(/choose stance/i)
    expect(trigger.getAttribute('aria-haspopup')).toBe('menu')
    expect(trigger.getAttribute('aria-expanded')).toBe('false')
    expect(screen.getByTestId('stance-rationale-input')).toBeTruthy()
    // Menu items not visible until trigger is clicked
    expect(screen.queryByTestId('stance-btn-agree')).toBeNull()
    expect(screen.queryByTestId('decision-bar-submitted')).toBeNull()
  })

  it('opening the trigger reveals only the three supported stance menu items', () => {
    render(<DecisionBar {...makeProps()} />)
    fireEvent.click(screen.getByTestId('stance-trigger-btn'))
    expect(screen.getByTestId('stance-btn-agree')).toBeTruthy()
    expect(screen.getByTestId('stance-btn-disagree')).toBeTruthy()
    expect(screen.getByTestId('stance-btn-need_more_info')).toBeTruthy()
    expect(screen.queryByTestId('stance-btn-conditional')).toBeNull()
    expect(screen.queryByTestId('stance-btn-abstain')).toBeNull()
    expect(screen.getByTestId('stance-trigger-btn').getAttribute('aria-expanded')).toBe('true')
  })

  it('clicking a stance menu item calls submitStance with that key', () => {
    const submitStance = vi.fn()
    render(<DecisionBar {...makeProps({ submitStance })} />)
    fireEvent.click(screen.getByTestId('stance-trigger-btn'))
    fireEvent.click(screen.getByTestId('stance-btn-agree'))
    expect(submitStance).toHaveBeenCalledWith('agree')
  })

  it('selecting a stance closes the menu', () => {
    render(<DecisionBar {...makeProps()} />)
    fireEvent.click(screen.getByTestId('stance-trigger-btn'))
    fireEvent.click(screen.getByTestId('stance-btn-agree'))
    expect(screen.queryByTestId('stance-btn-agree')).toBeNull()
  })

  it('Escape closes the menu', () => {
    render(<DecisionBar {...makeProps()} />)
    fireEvent.click(screen.getByTestId('stance-trigger-btn'))
    expect(screen.getByTestId('stance-btn-agree')).toBeTruthy()
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByTestId('stance-btn-agree')).toBeNull()
  })

  it('State B: trigger shows the submitted stance with a check and a stance-edit-btn id', () => {
    render(<DecisionBar {...makeProps({
      myStance: { stance: 'agree', rationale: 'Strong fit' },
      stanceAggregate: { agree: 1, disagree: 0, conditional: 0, abstain: 0, need_more_info: 0 },
    })} />)
    const trigger = screen.getByTestId('stance-edit-btn')
    expect(trigger.textContent).toContain('✓')
    expect(trigger.textContent).toContain('Agree')
    // Rationale input remains available below the trigger
    expect(screen.getByTestId('stance-rationale-input')).toBeTruthy()
    // Menu starts closed
    expect(screen.queryByTestId('stance-btn-agree')).toBeNull()
    expect(screen.getByTestId('decision-bar-submitted')).toBeTruthy()
  })

  it('clicking the submitted trigger reopens the menu and exposes Clear', () => {
    render(<DecisionBar {...makeProps({
      myStance: { stance: 'disagree', rationale: 'Concerned' },
    })} />)
    fireEvent.click(screen.getByTestId('stance-edit-btn'))
    expect(screen.getByTestId('stance-btn-agree')).toBeTruthy()
    expect(screen.getByTestId('stance-clear-btn')).toBeTruthy()
  })

  it('clicking Clear in the menu calls clearStance', () => {
    const clearStance = vi.fn()
    render(<DecisionBar {...makeProps({
      myStance: { stance: 'disagree', rationale: 'Concerned' },
      clearStance,
    })} />)
    fireEvent.click(screen.getByTestId('stance-edit-btn'))
    fireEvent.click(screen.getByTestId('stance-clear-btn'))
    expect(clearStance).toHaveBeenCalled()
  })

  it('cancel edit menu item closes menu without clearing stance', () => {
    const clearStance = vi.fn()
    render(<DecisionBar {...makeProps({
      myStance: { stance: 'disagree', rationale: 'Concerned' },
      clearStance,
    })} />)
    fireEvent.click(screen.getByTestId('stance-edit-btn'))
    fireEvent.click(screen.getByTestId('stance-cancel-edit-btn'))
    expect(screen.queryByTestId('stance-menu')).toBeNull()
    expect(clearStance).not.toHaveBeenCalled()
  })

  it('Save rationale only appears when rationale differs from saved and submits explicitly', () => {
    const submitStance = vi.fn()
    const props = makeProps({
      myStance: { stance: 'agree', rationale: 'old' },
      stanceRationale: 'old',
      submitStance,
    })
    const { rerender } = render(<DecisionBar {...props} />)
    expect(screen.queryByTestId('stance-save-rationale-btn')).toBeNull()
    rerender(<DecisionBar {...props} stanceRationale="new text" />)
    const saveBtn = screen.getByTestId('stance-save-rationale-btn')
    expect(saveBtn).toBeTruthy()
    fireEvent.click(saveBtn)
    expect(submitStance).toHaveBeenCalledWith('agree')
  })

  it('uses a multiline rationale textarea and preserves line breaks in edit state', () => {
    const setStanceRationale = vi.fn()
    render(<DecisionBar {...makeProps({
      myStance: { stance: 'agree', rationale: '' },
      stanceRationale: '',
      setStanceRationale,
    })} />)
    const rationaleInput = screen.getByTestId('stance-rationale-input')
    expect(rationaleInput.tagName).toBe('TEXTAREA')
    fireEvent.change(rationaleInput, { target: { value: 'line one\nline two' } })
    expect(setStanceRationale).toHaveBeenCalledWith('line one\nline two')
  })

  it('OTHERS row renders three stance counts plus a Pending count', () => {
    render(<DecisionBar {...makeProps({
      stanceAggregate: { agree: 6, disagree: 2, conditional: 1, abstain: 0, need_more_info: 1 },
      stanceResponses: [
        { id: 'r1', user_email: 'a@x.com', stance: 'agree' },
        { id: 'r2', user_email: 'b@x.com', stance: 'disagree' },
      ],
      sessionInvitations: [
        { id: 'i1', invited_email: 'a@x.com', status: 'accepted' },
        { id: 'i2', invited_email: 'b@x.com', status: 'accepted' },
        { id: 'i3', invited_email: 'c@x.com', status: 'pending' },
        { id: 'i4', invited_email: 'd@x.com', status: 'pending' },
      ],
    })} />)
    expect(screen.getByTestId('stance-count-agree').textContent).toContain('6')
    expect(screen.getByTestId('stance-count-disagree').textContent).toContain('2')
    expect(screen.getByTestId('stance-count-need_more_info').textContent).toContain('1')
    expect(screen.getByTestId('stance-count-pending').textContent).toContain('2')
    expect(screen.queryByTestId('stance-count-conditional')).toBeNull()
    expect(screen.queryByTestId('stance-count-abstain')).toBeNull()
  })

  it('Pending tooltip lists members who have not yet voted', () => {
    render(<DecisionBar {...makeProps({
      stanceResponses: [
        { id: 'r1', user_email: 'a@x.com', stance: 'agree' },
      ],
      sessionInvitations: [
        { id: 'i1', invited_email: 'a@x.com', status: 'accepted' },
        { id: 'i2', invited_email: 'b@x.com', status: 'pending' },
      ],
    })} />)
    fireEvent.click(screen.getByTestId('stance-count-pending'))
    const tooltip = screen.getByTestId('stance-tooltip-pending')
    expect(tooltip.textContent).toContain('b@x.com')
    expect(tooltip.textContent).not.toContain('a@x.com')
  })

  it('counts update live when stanceAggregate prop changes', () => {
    const props = makeProps({ stanceAggregate: { agree: 0 } })
    const { rerender } = render(<DecisionBar {...props} />)
    expect(screen.getByTestId('stance-count-agree').textContent).toContain('0')
    rerender(<DecisionBar {...props} stanceAggregate={{ agree: 7 }} />)
    expect(screen.getByTestId('stance-count-agree').textContent).toContain('7')
  })

  it('locks the form when decisionOutcome is set', () => {
    render(<DecisionBar {...makeProps({ decisionOutcome: 'Approved' })} />)
    expect(screen.getByTestId('decision-bar-locked')).toBeTruthy()
    expect(screen.queryByTestId('stance-trigger-btn')).toBeNull()
    expect(screen.queryByTestId('stance-edit-btn')).toBeNull()
    expect(screen.queryByTestId('stance-rationale-input')).toBeNull()
  })

  it('shows feedback message when provided', () => {
    render(<DecisionBar {...makeProps({ stanceFeedback: { type: 'success', message: 'Position recorded' } })} />)
    expect(screen.getByTestId('decision-bar-feedback').textContent).toBe('Position recorded')
  })

  it('ArrowDown moves focus through stance menu items and wraps; ArrowUp goes back', () => {
    render(<DecisionBar {...makeProps()} />)
    fireEvent.click(screen.getByTestId('stance-trigger-btn'))
    const items = [
      screen.getByTestId('stance-btn-agree'),
      screen.getByTestId('stance-btn-disagree'),
      screen.getByTestId('stance-btn-need_more_info'),
    ]
    // The first item is auto-focused on open
    expect(document.activeElement).toBe(items[0])
    const menu = screen.getByTestId('stance-menu')
    fireEvent.keyDown(menu, { key: 'ArrowDown' })
    expect(document.activeElement).toBe(items[1])
    fireEvent.keyDown(menu, { key: 'End' })
    expect(document.activeElement).toBe(items[items.length - 1])
    fireEvent.keyDown(menu, { key: 'ArrowDown' })
    // Wraps from last back to first
    expect(document.activeElement).toBe(items[0])
    fireEvent.keyDown(menu, { key: 'ArrowUp' })
    // Wraps from first back to last
    expect(document.activeElement).toBe(items[items.length - 1])
    fireEvent.keyDown(menu, { key: 'Home' })
    expect(document.activeElement).toBe(items[0])
  })

  it('renders a clear fallback label for legacy submitted stances', () => {
    render(<DecisionBar {...makeProps({
      myStance: { stance: 'conditional', rationale: 'legacy data' },
      stanceRationale: 'legacy data',
    })} />)
    const trigger = screen.getByTestId('stance-edit-btn')
    expect(trigger.textContent).toContain('Other (Conditional)')
  })

  it('only one Others stance popover may be open at a time', () => {
    render(<DecisionBar {...makeProps({
      stanceAggregate: { agree: 1, disagree: 1, conditional: 0, abstain: 0, need_more_info: 0 },
      stanceResponses: [
        { id: 'r1', user_email: 'a@x.com', stance: 'agree' },
        { id: 'r2', user_email: 'b@x.com', stance: 'disagree' },
      ],
    })} />)
    fireEvent.click(screen.getByTestId('stance-count-agree'))
    expect(screen.getByTestId('stance-tooltip-agree')).toBeTruthy()
    expect(screen.queryByTestId('stance-tooltip-disagree')).toBeNull()
    // Opening a different chip closes the previous one
    fireEvent.click(screen.getByTestId('stance-count-disagree'))
    expect(screen.queryByTestId('stance-tooltip-agree')).toBeNull()
    expect(screen.getByTestId('stance-tooltip-disagree')).toBeTruthy()
  })
})
