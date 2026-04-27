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
})
