import { useState, useEffect } from 'react'

const STORAGE_KEY = 'talkback.videoStarted'

/**
 * Shows a "→ Start here: Watch main video" prompt on top of the video player
 * for participants who have not yet started playback in this session.
 *
 * Props:
 *   sessionId  — used to namespace the localStorage flag per session
 *   playing    — true when the video is currently playing; triggers dismissal
 */
export function VideoStartOverlay({ sessionId, playing }) {
  const [visible, setVisible] = useState(false)

  // Initialise from localStorage when session changes
  useEffect(() => {
    if (!sessionId) {
      setVisible(false)
      return
    }
    try {
      const played = localStorage.getItem(`${STORAGE_KEY}.${sessionId}`)
      setVisible(played !== 'true')
    } catch {
      setVisible(false)
    }
  }, [sessionId])

  // Dismiss and persist when playback begins
  useEffect(() => {
    if (playing && visible) {
      setVisible(false)
      if (sessionId) {
        try {
          localStorage.setItem(`${STORAGE_KEY}.${sessionId}`, 'true')
        } catch {
          // ignore — overlay will rehide on next session load if storage fails
        }
      }
    }
  }, [playing, visible, sessionId])

  if (!visible) return null

  return (
    <div
      data-testid="video-start-overlay"
      style={{
        position: 'absolute',
        top: 0,
        left: 0,
        right: 0,
        bottom: 0,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        backgroundColor: 'rgba(0, 0, 0, 0.45)',
        zIndex: 10,
        // pointer-events: none so the overlay never blocks the native video controls
        pointerEvents: 'none',
        borderRadius: '4px',
      }}
    >
      <div
        style={{
          backgroundColor: 'rgba(255, 255, 255, 0.95)',
          padding: '14px 22px',
          borderRadius: '8px',
          fontSize: '15px',
          fontWeight: 600,
          color: '#1a1a1a',
          boxShadow: '0 4px 20px rgba(0, 0, 0, 0.35)',
          letterSpacing: '0.01em',
          userSelect: 'none',
        }}
      >
        → Start here: Watch main video
      </div>
    </div>
  )
}
