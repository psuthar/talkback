import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { DecisionBriefHeader } from '../components/DecisionBriefHeader'

describe('DecisionBriefHeader', () => {
  it('returns nothing when premise, decision, and outcome are all empty', () => {
    const { container } = render(<DecisionBriefHeader />)
    expect(container.firstChild).toBeNull()
  })

  it('renders premise and decision text', () => {
    render(<DecisionBriefHeader premise="Hiring John" decision="Should we extend an offer?" />)
    expect(screen.getByText('Hiring John')).toBeTruthy()
    expect(screen.getByText('Should we extend an offer?')).toBeTruthy()
    expect(screen.getByText('Premise')).toBeTruthy()
    expect(screen.getByText('Decision')).toBeTruthy()
  })

  it('renders only decision when no premise', () => {
    render(<DecisionBriefHeader decision="Pick X" />)
    expect(screen.queryByText('Premise')).toBeNull()
    expect(screen.getByText('Decision')).toBeTruthy()
    expect(screen.getByText('Pick X')).toBeTruthy()
    expect(screen.queryByTestId('decision-outcome-badge')).toBeNull()
  })

  it('renders outcome row when decisionOutcome is set alongside premise and decision', () => {
    render(
      <DecisionBriefHeader
        premise="Premise text"
        decision="Decision question"
        decisionOutcome="Approved"
      />
    )
    expect(screen.getByText('Outcome')).toBeTruthy()
    const badge = screen.getByTestId('decision-outcome-badge')
    expect(badge.textContent).toContain('Decision: Approved')
    expect(screen.getByText('Premise text')).toBeTruthy()
    expect(screen.getByText('Decision question')).toBeTruthy()
  })

  it('renders only the outcome row when only decisionOutcome is set', () => {
    render(<DecisionBriefHeader decisionOutcome="Approved" />)
    expect(screen.getByText('Outcome')).toBeTruthy()
    expect(screen.getByTestId('decision-outcome-badge').textContent).toContain('Approved')
    expect(screen.queryByText('Premise')).toBeNull()
    expect(screen.queryByText('Decision')).toBeNull()
  })

  it('treats whitespace-only premise/decision/outcome as empty', () => {
    const { container } = render(
      <DecisionBriefHeader premise="   " decision="" decisionOutcome={null} />
    )
    expect(container.firstChild).toBeNull()
  })

  it('hides the readiness row from non-creators when no decision makers are assigned', () => {
    render(
      <DecisionBriefHeader
        decision="Should we extend an offer?"
        readiness={{ decision_maker_total: 0, decision_maker_voted: 0, ready_to_close: false }}
      />
    )
    expect(screen.queryByTestId('decision-readiness')).toBeNull()
  })

  it('shows a muted "none assigned" hint to creators when no decision makers are assigned', () => {
    render(
      <DecisionBriefHeader
        decision="Should we extend an offer?"
        readiness={{ decision_maker_total: 0, decision_maker_voted: 0, ready_to_close: false }}
        onCloseDecision={() => {}}
      />
    )
    const node = screen.getByTestId('decision-readiness')
    expect(node.textContent).toBe('Decision makers — none assigned')
    expect(node.getAttribute('data-ready')).toBe('false')
    expect(node.getAttribute('data-empty')).toBe('true')
  })

  it('uses the "Decision makers" label on the readiness row', () => {
    render(
      <DecisionBriefHeader
        decision="Pick X"
        readiness={{ decision_maker_total: 3, decision_maker_voted: 1, ready_to_close: false }}
      />
    )
    expect(screen.getByText('Decision makers')).toBeTruthy()
    expect(screen.queryByText('Votes')).toBeNull()
  })

  it('renders role-first partial-vote readiness when some Decision Makers have voted', () => {
    render(
      <DecisionBriefHeader
        decision="Pick X"
        readiness={{ decision_maker_total: 3, decision_maker_voted: 1, ready_to_close: false }}
      />
    )
    const node = screen.getByTestId('decision-readiness')
    expect(node.textContent).toBe('Decision makers — 1 of 3 submitted')
    expect(node.getAttribute('data-ready')).toBe('false')
    expect(node.getAttribute('data-empty')).toBe('false')
  })

  it('flags ready-to-close with role-first copy when every Decision Maker has voted', () => {
    render(
      <DecisionBriefHeader
        decision="Pick X"
        readiness={{ decision_maker_total: 2, decision_maker_voted: 2, ready_to_close: true }}
      />
    )
    const node = screen.getByTestId('decision-readiness')
    expect(node.textContent).toBe('All decision makers submitted — ready to close')
    expect(node.getAttribute('data-ready')).toBe('true')
  })

  it('hides readiness once decisionOutcome is recorded — outcome row carries the signal', () => {
    render(
      <DecisionBriefHeader
        decision="Pick X"
        decisionOutcome="Approved"
        readiness={{ decision_maker_total: 2, decision_maker_voted: 2, ready_to_close: false }}
      />
    )
    expect(screen.queryByTestId('decision-readiness')).toBeNull()
    expect(screen.getByTestId('decision-outcome-badge').textContent).toContain('Approved')
  })

  it('hides readiness when no decision question is set', () => {
    render(
      <DecisionBriefHeader
        premise="Premise text"
        readiness={{ decision_maker_total: 2, decision_maker_voted: 1, ready_to_close: false }}
      />
    )
    expect(screen.queryByTestId('decision-readiness')).toBeNull()
  })

  it('hides the Close decision action when no callback is supplied (participant view)', () => {
    render(
      <DecisionBriefHeader
        decision="Pick X"
        readiness={{ decision_maker_total: 2, decision_maker_voted: 2, ready_to_close: true }}
      />
    )
    expect(screen.queryByTestId('close-decision-btn')).toBeNull()
  })

  it('hides the Close decision action when readiness is not yet ready', () => {
    const cb = vi.fn()
    render(
      <DecisionBriefHeader
        decision="Pick X"
        readiness={{ decision_maker_total: 2, decision_maker_voted: 1, ready_to_close: false }}
        onCloseDecision={cb}
      />
    )
    expect(screen.queryByTestId('close-decision-btn')).toBeNull()
  })

  it('shows the Close decision action only when callback is supplied AND readiness is ready', () => {
    const cb = vi.fn()
    render(
      <DecisionBriefHeader
        decision="Pick X"
        readiness={{ decision_maker_total: 2, decision_maker_voted: 2, ready_to_close: true }}
        onCloseDecision={cb}
      />
    )
    expect(screen.getByTestId('close-decision-btn')).toBeTruthy()
  })

  it('hides the Close decision action once decisionOutcome is recorded', () => {
    const cb = vi.fn()
    render(
      <DecisionBriefHeader
        decision="Pick X"
        decisionOutcome="Approved"
        readiness={{ decision_maker_total: 2, decision_maker_voted: 2, ready_to_close: false }}
        onCloseDecision={cb}
      />
    )
    expect(screen.queryByTestId('close-decision-btn')).toBeNull()
  })

  it('inline form submits the entered outcome via the callback and clears on success', async () => {
    const cb = vi.fn().mockResolvedValue({ ok: true })
    render(
      <DecisionBriefHeader
        decision="Pick X"
        readiness={{ decision_maker_total: 2, decision_maker_voted: 2, ready_to_close: true }}
        onCloseDecision={cb}
      />
    )
    fireEvent.click(screen.getByTestId('close-decision-btn'))
    const input = screen.getByTestId('close-decision-input')
    fireEvent.change(input, { target: { value: '   Approved by board   ' } })
    fireEvent.click(screen.getByTestId('close-decision-confirm'))
    await waitFor(() => expect(cb).toHaveBeenCalledTimes(1))
    expect(cb).toHaveBeenCalledWith('Approved by board')
    // After success, the form returns to the collapsed CTA.
    await waitFor(() => expect(screen.queryByTestId('close-decision-form')).toBeNull())
    expect(screen.getByTestId('close-decision-btn')).toBeTruthy()
  })

  it('inline form surfaces errors returned by the callback and keeps the form open', async () => {
    const cb = vi.fn().mockResolvedValue({ ok: false, error: 'forbidden' })
    render(
      <DecisionBriefHeader
        decision="Pick X"
        readiness={{ decision_maker_total: 2, decision_maker_voted: 2, ready_to_close: true }}
        onCloseDecision={cb}
      />
    )
    fireEvent.click(screen.getByTestId('close-decision-btn'))
    fireEvent.change(screen.getByTestId('close-decision-input'), { target: { value: 'Approved' } })
    fireEvent.click(screen.getByTestId('close-decision-confirm'))
    await waitFor(() => expect(screen.getByTestId('close-decision-error').textContent).toContain('forbidden'))
    expect(screen.getByTestId('close-decision-form')).toBeTruthy()
  })

  it('inline form rejects empty submissions without calling the callback', () => {
    const cb = vi.fn()
    render(
      <DecisionBriefHeader
        decision="Pick X"
        readiness={{ decision_maker_total: 2, decision_maker_voted: 2, ready_to_close: true }}
        onCloseDecision={cb}
      />
    )
    fireEvent.click(screen.getByTestId('close-decision-btn'))
    // Submit button is disabled when the input is empty/whitespace.
    expect(screen.getByTestId('close-decision-confirm').disabled).toBe(true)
    fireEvent.change(screen.getByTestId('close-decision-input'), { target: { value: '   ' } })
    expect(screen.getByTestId('close-decision-confirm').disabled).toBe(true)
    expect(cb).not.toHaveBeenCalled()
  })

  it('Cancel returns the inline form to the collapsed CTA without calling the callback', () => {
    const cb = vi.fn()
    render(
      <DecisionBriefHeader
        decision="Pick X"
        readiness={{ decision_maker_total: 2, decision_maker_voted: 2, ready_to_close: true }}
        onCloseDecision={cb}
      />
    )
    fireEvent.click(screen.getByTestId('close-decision-btn'))
    fireEvent.change(screen.getByTestId('close-decision-input'), { target: { value: 'Some text' } })
    fireEvent.click(screen.getByTestId('close-decision-cancel'))
    expect(screen.queryByTestId('close-decision-form')).toBeNull()
    expect(screen.getByTestId('close-decision-btn')).toBeTruthy()
    expect(cb).not.toHaveBeenCalled()
  })
})
