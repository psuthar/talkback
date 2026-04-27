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

  it('renders three pending chips when no progress signals are set', () => {
    render(<DecisionBriefHeader decision="Pick X" />)
    const chips = [1, 2, 3].map((i) => screen.getByTestId(`decision-chip-${i}`))
    expect(chips).toHaveLength(3)
    chips.forEach((chip) => expect(chip.getAttribute('data-done')).toBe('false'))
    expect(screen.getByText('Review video')).toBeTruthy()
    expect(screen.getByText('Ask questions')).toBeTruthy()
    expect(screen.getByText('Decide')).toBeTruthy()
  })

  it('marks chip 1 done when videoStarted is true', () => {
    render(<DecisionBriefHeader decision="Pick X" videoStarted />)
    expect(screen.getByTestId('decision-chip-1').getAttribute('data-done')).toBe('true')
    expect(screen.getByTestId('decision-chip-2').getAttribute('data-done')).toBe('false')
    expect(screen.getByTestId('decision-chip-3').getAttribute('data-done')).toBe('false')
  })

  it('marks chip 2 done when qaCount is greater than zero', () => {
    render(<DecisionBriefHeader decision="Pick X" qaCount={1} />)
    expect(screen.getByTestId('decision-chip-2').getAttribute('data-done')).toBe('true')
  })

  it('does not mark chip 2 done when qaCount is zero', () => {
    render(<DecisionBriefHeader decision="Pick X" qaCount={0} />)
    expect(screen.getByTestId('decision-chip-2').getAttribute('data-done')).toBe('false')
  })

  it('marks chip 3 done when stanceSubmitted is true', () => {
    render(<DecisionBriefHeader decision="Pick X" stanceSubmitted />)
    expect(screen.getByTestId('decision-chip-3').getAttribute('data-done')).toBe('true')
  })

  it('replaces the chip list with a Decision outcome badge when decisionOutcome is set', () => {
    render(
      <DecisionBriefHeader
        premise="Premise text"
        decision="Decision question"
        decisionOutcome="Approved"
        videoStarted
        qaCount={3}
        stanceSubmitted
      />
    )
    const badge = screen.getByTestId('decision-outcome-badge')
    expect(badge.textContent).toContain('Decision: Approved')
    // Chips are not rendered when outcome is present
    expect(screen.queryByTestId('decision-chip-1')).toBeNull()
    expect(screen.queryByTestId('decision-chip-2')).toBeNull()
    expect(screen.queryByTestId('decision-chip-3')).toBeNull()
    // Premise / decision still render
    expect(screen.getByText('Premise text')).toBeTruthy()
    expect(screen.getByText('Decision question')).toBeTruthy()
  })

  it('renders the chip list when only decision is set (no premise)', () => {
    render(<DecisionBriefHeader decision="Pick X" />)
    expect(screen.queryByText('Premise')).toBeNull()
    expect(screen.getByTestId('decision-chip-1')).toBeTruthy()
  })

  it('renders only the outcome row when only decisionOutcome is set', () => {
    render(<DecisionBriefHeader decisionOutcome="Approved" />)
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
