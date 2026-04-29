import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
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

  it('renders a "no Decision Makers assigned" hint when readiness has total=0', () => {
    render(
      <DecisionBriefHeader
        decision="Should we extend an offer?"
        readiness={{ decision_maker_total: 0, decision_maker_voted: 0, ready_to_close: false }}
      />
    )
    const node = screen.getByTestId('decision-readiness')
    expect(node.textContent).toBe('No Decision Makers assigned')
    expect(node.getAttribute('data-ready')).toBe('false')
  })

  it('renders partial-vote readiness when some Decision Makers have voted', () => {
    render(
      <DecisionBriefHeader
        decision="Pick X"
        readiness={{ decision_maker_total: 3, decision_maker_voted: 1, ready_to_close: false }}
      />
    )
    const node = screen.getByTestId('decision-readiness')
    expect(node.textContent).toBe('1/3 Decision Makers submitted')
    expect(node.getAttribute('data-ready')).toBe('false')
  })

  it('flags ready-to-close when every Decision Maker has voted', () => {
    render(
      <DecisionBriefHeader
        decision="Pick X"
        readiness={{ decision_maker_total: 2, decision_maker_voted: 2, ready_to_close: true }}
      />
    )
    const node = screen.getByTestId('decision-readiness')
    expect(node.textContent).toBe('2/2 Decision Makers submitted — ready to close')
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
})
