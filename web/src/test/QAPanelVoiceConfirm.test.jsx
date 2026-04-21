import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { QAPanel } from '../components/QAPanel'

const defaultProps = {
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
  voicePolishing: false,
  voicePolishMode: null,
}

describe('QAPanel — voice confirm inline layout', () => {
  it('shows only one textarea when not in voice confirm mode', () => {
    render(<QAPanel {...defaultProps} />)
    const textareas = screen.getAllByRole('textbox')
    expect(textareas.length).toBe(1)
  })

  it('shows only one textarea when in voice confirm mode', () => {
    render(<QAPanel {...defaultProps} showVoiceConfirm={true} voiceTranscribedText="hello" />)
    const textareas = screen.getAllByRole('textbox')
    expect(textareas.length).toBe(1)
  })

  it('textarea uses questionText when not in voice confirm mode', () => {
    render(<QAPanel {...defaultProps} questionText="my typed question" />)
    expect(screen.getByRole('textbox').value).toBe('my typed question')
  })

  it('textarea uses voiceTranscribedText when in voice confirm mode', () => {
    render(<QAPanel {...defaultProps} showVoiceConfirm={true} voiceTranscribedText="voice transcribed text" />)
    expect(screen.getByRole('textbox').value).toBe('voice transcribed text')
  })

  it('shows Ask button when not in voice confirm mode', () => {
    render(<QAPanel {...defaultProps} questionText="something" />)
    expect(screen.getByTestId('ask-button')).toBeTruthy()
  })

  it('shows Confirm & Submit and Cancel buttons instead of Ask when in voice confirm mode', () => {
    render(<QAPanel {...defaultProps} showVoiceConfirm={true} voiceTranscribedText="text" />)
    expect(screen.queryByTestId('ask-button')).toBeNull()
    expect(screen.getByText('Confirm & Submit')).toBeTruthy()
    expect(screen.getByText('Cancel')).toBeTruthy()
  })

  it('calls cancelVoiceReview when Cancel is clicked', () => {
    const cancelVoiceReview = vi.fn()
    render(<QAPanel {...defaultProps} showVoiceConfirm={true} voiceTranscribedText="text" cancelVoiceReview={cancelVoiceReview} />)
    fireEvent.click(screen.getByText('Cancel'))
    expect(cancelVoiceReview).toHaveBeenCalled()
  })

  it('calls setVoiceTranscribedText when textarea edited in voice confirm mode', () => {
    const setVoiceTranscribedText = vi.fn()
    render(<QAPanel {...defaultProps} showVoiceConfirm={true} voiceTranscribedText="original" setVoiceTranscribedText={setVoiceTranscribedText} />)
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'edited' } })
    expect(setVoiceTranscribedText).toHaveBeenCalledWith('edited')
  })
})
