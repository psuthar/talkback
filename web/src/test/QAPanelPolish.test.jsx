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

describe('QAPanel — speech-only polish visibility (SCRUM-162)', () => {
  it('does not render the polish wand in typed ask flow even when text + handlers exist', () => {
    const polishQuestionText = vi.fn()
    render(
      <QAPanel
        {...baseProps}
        questionText="my typed question"
        polishQuestionText={polishQuestionText}
      />
    )
    expect(screen.queryByTestId('polish-question-btn')).toBeNull()
  })

  it('still renders polish in voice-confirm mode and uses transcribed text gating', () => {
    const polishQuestionText = vi.fn()
    render(
      <QAPanel
        {...baseProps}
        showVoiceConfirm
        voiceTranscribedText="um voice text"
        questionText="typed text ignored here"
        polishQuestionText={polishQuestionText}
      />
    )
    const polish = screen.getByTestId('polish-question-btn')
    expect(polish.disabled).toBe(false)
    fireEvent.click(polish)
    expect(polishQuestionText).toHaveBeenCalledWith(true)
  })

  it('does not render polish in typed flow when only legacy polishVoiceQuestion is provided', () => {
    const polishQuestionText = vi.fn()
    render(
      <QAPanel
        {...baseProps}
        questionText="hi um like"
        polishVoiceQuestion={polishQuestionText}
      />
    )
    expect(screen.queryByTestId('polish-question-btn')).toBeNull()
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

  it('shows a busy spinner while polishing in LLM mode', () => {
    render(
      <QAPanel
        {...baseProps}
        showVoiceConfirm
        voiceTranscribedText="voice text"
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

describe('QAHistory inline reply — speech-only polish visibility (SCRUM-162)', () => {
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

  it('does not render polish in typed reply flow even when polish handlers are provided', () => {
    const polishQuestionText = vi.fn()
    renderReply({ polishQuestionText })
    expect(screen.queryByTestId('polish-reply-btn')).toBeNull()
  })

  it('renders polish in voice-confirm reply flow and invokes handler', () => {
    const polishQuestionText = vi.fn()
    renderReply({
      showVoiceConfirm: true,
      voiceTranscribedText: 'voice follow up',
      polishQuestionText,
    })
    const polish = screen.getByTestId('polish-reply-btn')
    fireEvent.click(polish)
    expect(polishQuestionText).toHaveBeenCalledWith(true)
  })
})
