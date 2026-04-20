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

function makeQuestionWithoutAnswer(id) {
  return {
    id,
    question_text: `Question ${id}`,
    created_at: '2026-01-01T00:00:00Z',
    asked_by: 'user@example.com',
    video_time_seconds: null,
    replies: [],
    answer: null,
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

describe('QAHistory default expand behavior', () => {
  it('cards with answers are expanded by default on load', () => {
    const { container } = renderQAHistory([makeQuestion('q1', 'answered')])
    // Answer text should be visible without clicking Expand
    const answerEl = container.querySelector('[data-testid="answer-text"]')
    expect(answerEl).not.toBeNull()
    expect(answerEl.textContent).toContain('Some answer text.')
  })

  it('cards without answers remain collapsed by default', () => {
    const { container } = renderQAHistory([makeQuestionWithoutAnswer('q2')])
    const answerEl = container.querySelector('[data-testid="answer-text"]')
    expect(answerEl).toBeNull()
    // Expand button should be present
    const expandBtn = container.querySelector('button[aria-label="Expand"]')
    expect(expandBtn).not.toBeNull()
  })

  it('collapse toggle works after default-open state', () => {
    const { container } = renderQAHistory([makeQuestion('q3', 'answered')])
    // Initially expanded — answer visible
    expect(container.querySelector('[data-testid="answer-text"]')).not.toBeNull()
    // Click Collapse
    const collapseBtn = container.querySelector('button[aria-label="Collapse"]')
    expect(collapseBtn).not.toBeNull()
    fireEvent.click(collapseBtn)
    // After collapse — answer hidden
    expect(container.querySelector('[data-testid="answer-text"]')).toBeNull()
  })
})

describe('QAHistory answer_status mapping', () => {
  it('renders "Answered" for answered status', () => {
    const { container } = renderQAHistory([makeQuestion('q1', 'answered')])
    const statusEl = container.querySelector('[data-testid="answer-status"]')
    expect(statusEl).not.toBeNull()
    expect(statusEl.textContent).toBe('Answered')
    expect(statusEl.textContent).not.toBe('answered')
  })

  it('renders "Not covered by session" for not_covered status', () => {
    const { container } = renderQAHistory([makeQuestion('q2', 'not_covered')])
    const statusEl = container.querySelector('[data-testid="answer-status"]')
    expect(statusEl).not.toBeNull()
    expect(statusEl.textContent).toBe('Not covered by session')
    expect(statusEl.textContent).not.toBe('not_covered')
  })

  it('renders "Unable to answer" for error status', () => {
    const { container } = renderQAHistory([makeQuestion('q3', 'error')])
    const statusEl = container.querySelector('[data-testid="answer-status"]')
    expect(statusEl).not.toBeNull()
    expect(statusEl.textContent).toBe('Unable to answer')
    expect(statusEl.textContent).not.toBe('error')
  })

  it('renders "Unknown" for unrecognized status values', () => {
    const { container } = renderQAHistory([makeQuestion('q4', 'future_status')])
    const statusEl = container.querySelector('[data-testid="answer-status"]')
    expect(statusEl).not.toBeNull()
    expect(statusEl.textContent).toBe('Unknown')
    expect(statusEl.textContent).not.toBe('future_status')
  })
})
