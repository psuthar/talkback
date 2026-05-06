import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { QAPanel } from '../components/QAPanel'

// SCRUM-310: Enter in the Q&A textarea submits the question without clicking Ask.
// Covers Shift+Enter (newline), empty/loading guards, voice-confirm exclusion,
// and IME composition handling.

const baseProps = {
  questions: [],
  unreadQuestionIds: [],
  markQuestionViewed: vi.fn(),
  sessionId: 'sess-1',
  loading: false,
  questionText: 'my question',
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
  voicePolishing: false,
  voicePolishMode: null,
}

describe('QAPanel — Enter-to-submit', () => {
  it('Enter on a non-empty textarea calls askSessionQuestion (same path as Ask click)', () => {
    const ask = vi.fn()
    render(<QAPanel {...baseProps} askSessionQuestion={ask} />)
    fireEvent.keyDown(screen.getByTestId('question-input'), { key: 'Enter' })
    expect(ask).toHaveBeenCalledTimes(1)
  })

  it('Shift+Enter does NOT submit (default newline behavior preserved)', () => {
    const ask = vi.fn()
    render(<QAPanel {...baseProps} askSessionQuestion={ask} />)
    fireEvent.keyDown(screen.getByTestId('question-input'), { key: 'Enter', shiftKey: true })
    expect(ask).not.toHaveBeenCalled()
  })

  it('Enter on empty textarea is a no-op (matches Ask button disabled state)', () => {
    const ask = vi.fn()
    render(<QAPanel {...baseProps} askSessionQuestion={ask} questionText="" />)
    fireEvent.keyDown(screen.getByTestId('question-input'), { key: 'Enter' })
    expect(ask).not.toHaveBeenCalled()
  })

  it('Enter on whitespace-only textarea is a no-op', () => {
    const ask = vi.fn()
    render(<QAPanel {...baseProps} askSessionQuestion={ask} questionText={'   \n  '} />)
    fireEvent.keyDown(screen.getByTestId('question-input'), { key: 'Enter' })
    expect(ask).not.toHaveBeenCalled()
  })

  it('Enter while loading is a no-op (prevents double-submit)', () => {
    const ask = vi.fn()
    render(<QAPanel {...baseProps} askSessionQuestion={ask} loading={true} />)
    fireEvent.keyDown(screen.getByTestId('question-input'), { key: 'Enter' })
    expect(ask).not.toHaveBeenCalled()
  })

  it('Enter during IME composition is a no-op (does not commit candidate as submit)', () => {
    const ask = vi.fn()
    render(<QAPanel {...baseProps} askSessionQuestion={ask} />)
    // KeyboardEventInit accepts isComposing; React's SyntheticEvent surfaces it
    // via e.nativeEvent.isComposing. fireEvent forwards top-level keys into
    // the underlying KeyboardEvent constructor.
    fireEvent.keyDown(screen.getByTestId('question-input'), { key: 'Enter', isComposing: true })
    expect(ask).not.toHaveBeenCalled()
  })

  it('Enter in voice-confirm mode is a no-op (Confirm & Submit click is the only path)', () => {
    const ask = vi.fn()
    const confirm = vi.fn()
    render(
      <QAPanel
        {...baseProps}
        askSessionQuestion={ask}
        confirmVoiceQuestion={confirm}
        showVoiceConfirm={true}
        voiceTranscribedText="transcribed"
      />
    )
    fireEvent.keyDown(screen.getByTestId('question-input'), { key: 'Enter' })
    expect(ask).not.toHaveBeenCalled()
    expect(confirm).not.toHaveBeenCalled()
  })

  it('keys other than Enter do not trigger submit', () => {
    const ask = vi.fn()
    render(<QAPanel {...baseProps} askSessionQuestion={ask} />)
    fireEvent.keyDown(screen.getByTestId('question-input'), { key: 'a' })
    fireEvent.keyDown(screen.getByTestId('question-input'), { key: 'Tab' })
    fireEvent.keyDown(screen.getByTestId('question-input'), { key: 'Escape' })
    expect(ask).not.toHaveBeenCalled()
  })
})
