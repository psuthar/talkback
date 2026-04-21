import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent, act } from '@testing-library/react'
import { QAPanel } from '../components/QAPanel'

const defaultProps = {
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

describe('QAPanel — isProcessing state', () => {
  it('does not show Thinking indicator before ask is clicked', () => {
    render(<QAPanel {...defaultProps} loading={false} />)
    expect(screen.queryByText('Thinking…')).toBeNull()
  })

  it('shows Thinking indicator after ask is clicked (loading transitions to true)', async () => {
    const { rerender } = render(<QAPanel {...defaultProps} loading={false} />)
    fireEvent.click(screen.getByTestId('ask-button'))

    await act(async () => {
      rerender(<QAPanel {...defaultProps} loading={true} />)
    })
    expect(screen.getByText('Thinking…')).toBeTruthy()
  })

  it('hides Thinking indicator when loading becomes false even if questionText is still set', async () => {
    const { rerender } = render(<QAPanel {...defaultProps} loading={false} />)
    fireEvent.click(screen.getByTestId('ask-button'))

    await act(async () => {
      rerender(<QAPanel {...defaultProps} loading={true} questionText="my question" />)
    })
    expect(screen.getByText('Thinking…')).toBeTruthy()

    await act(async () => {
      rerender(<QAPanel {...defaultProps} loading={false} questionText="my question" />)
    })
    expect(screen.queryByText('Thinking…')).toBeNull()
  })

  it('hides Thinking indicator when loading clears even after questionText is cleared', async () => {
    const { rerender } = render(<QAPanel {...defaultProps} loading={false} />)
    fireEvent.click(screen.getByTestId('ask-button'))

    await act(async () => {
      rerender(<QAPanel {...defaultProps} loading={true} questionText="my question" />)
    })
    expect(screen.getByText('Thinking…')).toBeTruthy()

    await act(async () => {
      rerender(<QAPanel {...defaultProps} loading={false} questionText="" />)
    })
    expect(screen.queryByText('Thinking…')).toBeNull()
  })

  it('does not show Thinking when loading=true but ask was never clicked', () => {
    render(<QAPanel {...defaultProps} loading={true} questionText="" />)
    expect(screen.queryByText('Thinking…')).toBeNull()
  })
})
