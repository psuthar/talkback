import { describe, it, expect } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { QAHistory } from '../components/QAHistory'

function makeQuestion(id, answerStatus) {
  return {
    id,
    question_text: `Question ${id}`,
    created_at: '2026-01-01T00:00:00Z',
    asked_by: 'user@example.com',
    video_time_seconds: null,
    replies: [],
    answer: {
      answer_text: 'Some answer text.',
      answer_status: answerStatus,
      model: 'gpt-4',
      confidence: 0.9,
      confirmed: false,
      cited_materials: [],
      citations: [],
    },
  }
}

function renderQAHistory(questions) {
  return render(
    <QAHistory
      questions={questions}
      unreadQuestionIds={[]}
      readOnly={false}
    />
  )
}

function expandCard(container) {
  const expandBtn = container.querySelector('button[aria-label="Expand"]')
  if (expandBtn) {
    fireEvent.click(expandBtn)
  }
}

describe('QAHistory answer_status mapping', () => {
  it('renders "Answered" for answered status', () => {
    const { container } = renderQAHistory([makeQuestion('q1', 'answered')])
    expandCard(container)
    const statusEl = container.querySelector('[data-testid="answer-status"]')
    expect(statusEl).not.toBeNull()
    expect(statusEl.textContent).toBe('Answered')
    expect(statusEl.textContent).not.toBe('answered')
  })

  it('renders "Not covered by session" for not_covered status', () => {
    const { container } = renderQAHistory([makeQuestion('q2', 'not_covered')])
    expandCard(container)
    const statusEl = container.querySelector('[data-testid="answer-status"]')
    expect(statusEl).not.toBeNull()
    expect(statusEl.textContent).toBe('Not covered by session')
    expect(statusEl.textContent).not.toBe('not_covered')
  })

  it('renders "Unable to answer" for error status', () => {
    const { container } = renderQAHistory([makeQuestion('q3', 'error')])
    expandCard(container)
    const statusEl = container.querySelector('[data-testid="answer-status"]')
    expect(statusEl).not.toBeNull()
    expect(statusEl.textContent).toBe('Unable to answer')
    expect(statusEl.textContent).not.toBe('error')
  })

  it('renders "Unknown" for unrecognized status values', () => {
    const { container } = renderQAHistory([makeQuestion('q4', 'future_status')])
    expandCard(container)
    const statusEl = container.querySelector('[data-testid="answer-status"]')
    expect(statusEl).not.toBeNull()
    expect(statusEl.textContent).toBe('Unknown')
    expect(statusEl.textContent).not.toBe('future_status')
  })
})
