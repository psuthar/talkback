// SCRUM-427: cross-component happy-path test for the multi-recording UI
// surface. Walks one render through the realistic sequence a user goes
// through when they have two recordings imported:
//
//   1. RecordingsSection renders two rows with the correct per-platform
//      source badge and primary indicator on the right row.
//   2. Make primary on the secondary row -> PATCH /primary-recording
//      with the right body.
//   3. After refresh, primary moved to the other row; the previous
//      primary now shows the "Make primary" action in its kebab.
//   4. Remove on the now-secondary row -> DELETE /video-sources/{id}.
//
// Pure unit (no Playwright); the real-browser happy path is tracked as a
// SCRUM-427 follow-up (see ticket description).
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { RecordingsSection } from '../components/RecordingsSection'

function MultiRecordingHarness({ initial }) {
  const [recordings, setRecordings] = useState(initial)
  return (
    <RecordingsSection
      sessionId="s-multi"
      apiBaseUrl="http://api.test"
      userEmail="editor@example.com"
      recordings={recordings}
      onChanged={async () => {
        const last = global.fetch.mock.calls.at(-1)
        if (!last) return
        const [url, init] = last
        const method = init?.method
        if (method === 'PATCH' && url.endsWith('/primary-recording')) {
          const body = JSON.parse(init.body)
          setRecordings((prev) => prev.map((r) => ({
            ...r,
            video_role: r.id === body.video_source_id ? 'primary' : 'secondary',
          })))
        } else if (method === 'DELETE') {
          const id = url.split('/').pop()
          setRecordings((prev) => prev.filter((r) => r.id !== id))
        }
      }}
    />
  )
}

describe('SCRUM-427: multi-recording happy path through RecordingsSection', () => {
  let originalFetch
  beforeEach(() => { originalFetch = global.fetch })
  afterEach(() => { global.fetch = originalFetch })

  it('renders two recordings, flips primary, then removes one', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 204, json: async () => ({}) })
    const user = userEvent.setup()

    render(<MultiRecordingHarness initial={[
      { id: 'vs-zoom-a', provider: 'zoom', display_title: 'Zoom Standup 5/12',
        transcript_status: 'ready', transcription_source: 'zoom_api',
        video_role: 'primary', created_at: '2026-05-12T15:00:00Z' },
      { id: 'vs-meet-b', provider: 'google_meet', display_title: 'Meet Review 5/13',
        transcript_status: 'ready', transcription_source: 'google_meet_native',
        video_role: 'secondary', created_at: '2026-05-13T15:00:00Z' },
    ]} />)

    // 1. Two rows, correct source badges, primary indicator on vs-zoom-a.
    expect(screen.getByTestId('recording-row-vs-zoom-a-source-badge').textContent).toBe('Zoom')
    expect(screen.getByTestId('recording-row-vs-meet-b-source-badge').textContent).toBe('Google Meet')
    expect(screen.getByTestId('recording-row-vs-zoom-a-primary-indicator')).toBeTruthy()
    expect(screen.queryByTestId('recording-row-vs-meet-b-primary-indicator')).toBeNull()

    // 2. Flip primary to the Meet row.
    await user.click(screen.getByTestId('recording-row-vs-meet-b-kebab'))
    await user.click(screen.getByTestId('recording-row-vs-meet-b-make-primary'))
    await waitFor(() => {
      expect(screen.getByTestId('recording-row-vs-meet-b-primary-indicator')).toBeTruthy()
    })
    // Old primary lost its indicator.
    expect(screen.queryByTestId('recording-row-vs-zoom-a-primary-indicator')).toBeNull()

    // PATCH body is correct.
    const patchCall = global.fetch.mock.calls.find(([url, init]) =>
      url.endsWith('/primary-recording') && init.method === 'PATCH'
    )
    expect(patchCall).toBeTruthy()
    expect(JSON.parse(patchCall[1].body).video_source_id).toBe('vs-meet-b')

    // 3. Now the Zoom row is secondary — its kebab should offer "Make primary".
    await user.click(screen.getByTestId('recording-row-vs-zoom-a-kebab'))
    expect(screen.getByTestId('recording-row-vs-zoom-a-make-primary')).toBeTruthy()

    // 4. Remove the now-secondary Zoom row via DELETE.
    await user.click(screen.getByTestId('recording-row-vs-zoom-a-remove'))
    await waitFor(() => {
      expect(screen.queryByTestId('recording-row-vs-zoom-a')).toBeNull()
    })
    const deleteCall = global.fetch.mock.calls.find(([url, init]) =>
      init.method === 'DELETE' && url.endsWith('/video-sources/vs-zoom-a')
    )
    expect(deleteCall).toBeTruthy()

    // The remaining row is the Meet one and it is primary.
    expect(screen.getByTestId('recording-row-vs-meet-b')).toBeTruthy()
    expect(screen.getByTestId('recording-row-vs-meet-b-primary-indicator')).toBeTruthy()
  })
})
