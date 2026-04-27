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

  it('State A: renders the five stance buttons and a rationale input when no stance is submitted', () => {
    render(<DecisionBar {...makeProps()} />)
    expect(screen.getByTestId('stance-btn-agree')).toBeTruthy()
    expect(screen.getByTestId('stance-btn-disagree')).toBeTruthy()
    expect(screen.getByTestId('stance-btn-conditional')).toBeTruthy()
    expect(screen.getByTestId('stance-btn-abstain')).toBeTruthy()
    expect(screen.getByTestId('stance-btn-need_more_info')).toBeTruthy()
    expect(screen.getByTestId('stance-rationale-input')).toBeTruthy()
    expect(screen.queryByTestId('decision-bar-submitted')).toBeNull()
  })

  it('clicking a stance button calls submitStance with that key', () => {
    const submitStance = vi.fn()
    render(<DecisionBar {...makeProps({ submitStance })} />)
    fireEvent.click(screen.getByTestId('stance-btn-agree'))
    expect(submitStance).toHaveBeenCalledWith('agree')
  })

  it('State B: shows the submitted stance with a checkmark and an Edit toggle', () => {
    render(<DecisionBar {...makeProps({
      myStance: { stance: 'agree', rationale: 'Strong fit' },
      stanceAggregate: { agree: 1, disagree: 0, conditional: 0, abstain: 0, need_more_info: 0 },
    })} />)
    const submitted = screen.getByTestId('decision-bar-submitted')
    expect(submitted.textContent).toContain('✓')
    expect(submitted.textContent).toContain('Agree')
    expect(submitted.textContent).toContain('Strong fit')
    expect(screen.getByTestId('stance-edit-btn')).toBeTruthy()
    // Stance buttons hidden when in submitted view
    expect(screen.queryByTestId('stance-btn-agree')).toBeNull()
  })

  it('clicking Edit ▾ flips submitted view back to State A controls', () => {
    render(<DecisionBar {...makeProps({
      myStance: { stance: 'disagree', rationale: 'Concerned' },
    })} />)
    expect(screen.queryByTestId('stance-btn-agree')).toBeNull()
    fireEvent.click(screen.getByTestId('stance-edit-btn'))
    expect(screen.getByTestId('stance-btn-agree')).toBeTruthy()
    expect(screen.getByTestId('stance-cancel-edit-btn')).toBeTruthy()
  })

  it('OTHERS row renders all five stance counts plus a Pending count', () => {
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
    expect(screen.getByTestId('stance-count-conditional').textContent).toContain('1')
    expect(screen.getByTestId('stance-count-abstain').textContent).toContain('0')
    expect(screen.getByTestId('stance-count-need_more_info').textContent).toContain('1')
    // Pending: 4 invited - 2 responded = 2
    expect(screen.getByTestId('stance-count-pending').textContent).toContain('2')
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
    expect(screen.queryByTestId('stance-btn-agree')).toBeNull()
    expect(screen.queryByTestId('decision-bar-submitted')).toBeNull()
  })

  it('shows feedback message when provided', () => {
    render(<DecisionBar {...makeProps({ stanceFeedback: { type: 'success', message: 'Position recorded' } })} />)
    expect(screen.getByTestId('decision-bar-feedback').textContent).toBe('Position recorded')
  })
})
