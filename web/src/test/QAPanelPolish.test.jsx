import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { QAPanel } from '../components/QAPanel'
import { QAHistory } from '../components/QAHistory'

const baseProps = {
  questions: [],
  unreadQuestionIds: [],
  markQuestionViewed: vi.fn(),
  sessionId: 'sess-1',
  loading: false,
  questionText: '',
  setQuestionText: vi.fn(),
  askSessionQuestion: vi.fn(),
  askQuestionFeedback: { message: '', type: '' },
  currentAnswer: null,
  onCitationClick: vi.fn(),
  replyingToQuestionId: null,
  setReplyingToQuestionId: vi.fn(),
  currentAskerName: 'User',
  creatorDisplayName: 'Creator',
  voiceRecording: false,
  voiceUploading: false,
  toggleVoiceRecording: vi.fn(),
  voiceFeedback: { message: '', type: '' },
  showVoiceConfirm: false,
  voiceTranscribedText: '',
  setVoiceTranscribedText: vi.fn(),
  confirmVoiceQuestion: vi.fn(),
  cancelVoiceReview: vi.fn(),
  polishVoiceQuestion: null,
  polishQuestionText: null,
  voicePolishing: false,
  voicePolishMode: null,
}

describe('QAPanel — typed-question polish (SCRUM-156)', () => {
  it('renders the polish wand beside the Ask button when typed text is present', () => {
    const polishQuestionText = vi.fn()
    render(
      <QAPanel
        {...baseProps}
        questionText="my typed question"
        polishQuestionText={polishQuestionText}
      />
    )
    const polish = screen.getByTestId('polish-question-btn')
    expect(polish).toBeTruthy()
    expect(polish.getAttribute('aria-label')).toMatch(/polish/i)
    expect(polish.disabled).toBe(false)
  })

  it('disables polish when there is no typed text and no voice text', () => {
    const polishQuestionText = vi.fn()
    render(
      <QAPanel
        {...baseProps}
        questionText=""
        polishQuestionText={polishQuestionText}
      />
    )
    expect(screen.getByTestId('polish-question-btn').disabled).toBe(true)
  })

  it('clicking polish in the typed flow invokes the polish handler', () => {
    const polishQuestionText = vi.fn()
    render(
      <QAPanel
        {...baseProps}
        questionText="hi um like"
        polishQuestionText={polishQuestionText}
      />
    )
    fireEvent.click(screen.getByTestId('polish-question-btn'))
    expect(polishQuestionText).toHaveBeenCalledWith(true)
  })

  it('polish in voice-confirm mode is enabled by voiceTranscribedText, not questionText', () => {
    const polishQuestionText = vi.fn()
    render(
      <QAPanel
        {...baseProps}
        showVoiceConfirm
        voiceTranscribedText="um voice text"
        questionText=""
        polishQuestionText={polishQuestionText}
      />
    )
    const polish = screen.getByTestId('polish-question-btn')
    expect(polish.disabled).toBe(false)
    fireEvent.click(polish)
    expect(polishQuestionText).toHaveBeenCalledWith(true)
  })

  it('does not render polish in the typed flow when no polish handler is provided', () => {
    render(<QAPanel {...baseProps} questionText="text" />)
    expect(screen.queryByTestId('polish-question-btn')).toBeNull()
  })

  it('falls back to legacy polishVoiceQuestion when polishQuestionText is not passed', () => {
    const polishVoiceQuestion = vi.fn()
    render(
      <QAPanel
        {...baseProps}
        questionText="text"
        polishVoiceQuestion={polishVoiceQuestion}
      />
    )
    fireEvent.click(screen.getByTestId('polish-question-btn'))
    expect(polishVoiceQuestion).toHaveBeenCalledWith(true)
  })

  it('shows a busy spinner while polishing in LLM mode', () => {
    render(
      <QAPanel
        {...baseProps}
        questionText="text"
        polishQuestionText={vi.fn()}
        voicePolishing
        voicePolishMode="llm"
      />
    )
    const polish = screen.getByTestId('polish-question-btn')
    expect(polish.disabled).toBe(true)
    expect(polish.querySelector('.spinner')).toBeTruthy()
  })
})

describe('QAHistory inline reply — typed polish (SCRUM-156)', () => {
  function renderReply(extra = {}) {
    const questions = [
      { id: 'q1', question_text: 'What is the plan?', created_at: '2024-01-01T00:00:00Z', asked_by: 'someone@x.com', answer: null },
    ]
    return render(
      <QAHistory
        questions={questions}
        unreadQuestionIds={[]}
        markQuestionViewed={vi.fn()}
        sessionId="s1"
        readOnly={false}
        currentAskerName="me@x.com"
        onCitationClick={vi.fn()}
        creatorDisplayName="Creator"
        replyingToQuestionId="q1"
        setReplyingToQuestionId={vi.fn()}
        questionText="follow up"
        setQuestionText={vi.fn()}
        askSessionQuestion={vi.fn()}
        loading={false}
        toggleVoiceRecording={vi.fn()}
        voiceRecording={false}
        voiceUploading={false}
        voiceFeedback={{ message: '', type: '' }}
        showVoiceConfirm={false}
        voiceTranscribedText=""
        setVoiceTranscribedText={vi.fn()}
        confirmVoiceQuestion={vi.fn()}
        cancelVoiceReview={vi.fn()}
        polishVoiceQuestion={vi.fn()}
        askQuestionFeedback={{ message: '', type: '' }}
        {...extra}
      />
    )
  }

  it('renders a polish button in the typed reply flow when polishQuestionText is provided', () => {
    const polishQuestionText = vi.fn()
    renderReply({ polishQuestionText })
    const polish = screen.getByTestId('polish-reply-btn')
    expect(polish).toBeTruthy()
    fireEvent.click(polish)
    expect(polishQuestionText).toHaveBeenCalledWith(true)
  })

  it('falls back to polishVoiceQuestion in the typed reply flow when polishQuestionText is missing', () => {
    const polishVoiceQuestion = vi.fn()
    renderReply({ polishVoiceQuestion, polishQuestionText: undefined })
    fireEvent.click(screen.getByTestId('polish-reply-btn'))
    expect(polishVoiceQuestion).toHaveBeenCalledWith(true)
  })
})
