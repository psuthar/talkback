import { describe, it, expect, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { VideoStartOverlay } from '../components/VideoStartOverlay'

const STORAGE_KEY = 'talkback.videoStarted'

describe('VideoStartOverlay', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('shows the overlay for a new user who has not yet played the video', () => {
    render(<VideoStartOverlay sessionId="sess-1" playing={false} />)
    expect(screen.getByTestId('video-start-overlay')).toBeTruthy()
    expect(screen.getByText(/Start here/)).toBeTruthy()
  })

  it('does not show the overlay for a returning user (played key already in localStorage)', () => {
    localStorage.setItem(`${STORAGE_KEY}.sess-1`, 'true')
    render(<VideoStartOverlay sessionId="sess-1" playing={false} />)
    expect(screen.queryByTestId('video-start-overlay')).toBeNull()
  })

  it('hides the overlay and persists to localStorage when playback starts', () => {
    const { rerender } = render(<VideoStartOverlay sessionId="sess-1" playing={false} />)
    expect(screen.getByTestId('video-start-overlay')).toBeTruthy()

    rerender(<VideoStartOverlay sessionId="sess-1" playing={true} />)
    expect(screen.queryByTestId('video-start-overlay')).toBeNull()
    expect(localStorage.getItem(`${STORAGE_KEY}.sess-1`)).toBe('true')
  })

  it('remains hidden on subsequent renders after playback has started', () => {
    localStorage.setItem(`${STORAGE_KEY}.sess-2`, 'true')
    render(<VideoStartOverlay sessionId="sess-2" playing={false} />)
    expect(screen.queryByTestId('video-start-overlay')).toBeNull()
  })

  it('renders nothing when sessionId is not provided', () => {
    render(<VideoStartOverlay sessionId={null} playing={false} />)
    expect(screen.queryByTestId('video-start-overlay')).toBeNull()
  })
})
