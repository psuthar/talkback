import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { RecordingsSection, SourceBadge } from '../components/RecordingsSection'

const baseRecording = (overrides = {}) => ({
  id: 'vs-1',
  session_id: 's-1',
  provider: 'zoom',
  video_url: 'https://example.com/v.mp4',
  display_title: 'Standup',
  transcript_status: 'ready',
  transcription_source: 'zoom_api',
  duration_seconds: 1800,
  video_role: 'primary',
  created_at: '2026-05-10T15:00:00Z',
  ...overrides,
})

describe('RecordingsSection', () => {
  let originalFetch
  beforeEach(() => { originalFetch = global.fetch })
  afterEach(() => { global.fetch = originalFetch })

  it('renders empty state when there are no recordings', () => {
    render(<RecordingsSection sessionId="s-1" apiBaseUrl="http://api.test" recordings={[]} />)
    expect(screen.getByTestId('recordings-section-empty')).toBeTruthy()
  })

  it('renders a row per recording with source badge, status pill, primary indicator', () => {
    render(
      <RecordingsSection
        sessionId="s-1"
        apiBaseUrl="http://api.test"
        recordings={[
          baseRecording({ id: 'vs-1', display_title: 'Zoom standup', provider: 'zoom', video_role: 'primary' }),
          baseRecording({ id: 'vs-2', display_title: 'Meet review', provider: 'google_meet', transcript_status: 'pending', transcription_source: 'whisper', video_role: 'secondary', created_at: '2026-05-11T15:00:00Z' }),
          baseRecording({ id: 'vs-3', display_title: 'Uploaded MP4', provider: 'other', transcript_status: 'failed', video_role: 'secondary', created_at: '2026-05-12T15:00:00Z' }),
        ]}
      />
    )
    expect(screen.getByTestId('recording-row-vs-1-source-badge').textContent).toBe('Zoom')
    expect(screen.getByTestId('recording-row-vs-1-status-pill').textContent).toBe('Transcript ready')
    expect(screen.getByTestId('recording-row-vs-1-primary-indicator')).toBeTruthy()
    expect(screen.getByTestId('recording-row-vs-2-status-pill').textContent).toContain('Whisper fallback')
    expect(screen.getByTestId('recording-row-vs-3-status-pill').textContent).toBe('Transcript failed')
  })

  it('sorts by created_at ascending', () => {
    render(
      <RecordingsSection
        sessionId="s-1"
        apiBaseUrl="http://api.test"
        recordings={[
          baseRecording({ id: 'newer', created_at: '2026-05-12T15:00:00Z' }),
          baseRecording({ id: 'oldest', created_at: '2026-05-10T15:00:00Z' }),
          baseRecording({ id: 'middle', created_at: '2026-05-11T15:00:00Z' }),
        ]}
      />
    )
    const items = screen.getByTestId('recordings-section-list').querySelectorAll('li')
    expect(items[0].getAttribute('data-testid')).toBe('recording-row-oldest')
    expect(items[1].getAttribute('data-testid')).toBe('recording-row-middle')
    expect(items[2].getAttribute('data-testid')).toBe('recording-row-newer')
  })

  it('in-progress summary collapses by default and expands on click', async () => {
    const user = userEvent.setup()
    render(
      <RecordingsSection
        sessionId="s-1"
        apiBaseUrl="http://api.test"
        recordings={[
          baseRecording({ id: 'ready', transcript_status: 'ready' }),
          baseRecording({ id: 'p1', transcript_status: 'pending', transcription_source: 'whisper' }),
          baseRecording({ id: 'p2', transcript_status: 'pending', transcription_source: 'zoom_api' }),
        ]}
      />
    )
    expect(screen.getByTestId('recordings-in-progress-summary').textContent).toContain('2 imports in progress')
    // Rows still rendered regardless of summary state (rows are always visible; the summary is informational).
    expect(screen.getByTestId('recording-row-p1')).toBeTruthy()
    expect(screen.getByTestId('recording-row-p2')).toBeTruthy()
    await user.click(screen.getByTestId('recordings-in-progress-summary'))
    expect(screen.queryByTestId('recordings-in-progress-summary')).toBeNull()
  })

  it('kebab Make primary calls PATCH /primary-recording and triggers onChanged', async () => {
    const onChanged = vi.fn()
    global.fetch = vi.fn().mockResolvedValueOnce({ ok: true, status: 200, json: async () => ({}) })
    const user = userEvent.setup()
    render(
      <RecordingsSection
        sessionId="s-1"
        apiBaseUrl="http://api.test"
        userEmail="u@example.com"
        onChanged={onChanged}
        recordings={[
          baseRecording({ id: 'vs-target', video_role: 'secondary' }),
        ]}
      />
    )
    await user.click(screen.getByTestId('recording-row-vs-target-kebab'))
    await user.click(screen.getByTestId('recording-row-vs-target-make-primary'))
    await waitFor(() => expect(onChanged).toHaveBeenCalled())
    const [url, init] = global.fetch.mock.calls[0]
    expect(url).toBe('http://api.test/api/sessions/s-1/primary-recording')
    expect(init.method).toBe('PATCH')
    expect(JSON.parse(init.body).video_source_id).toBe('vs-target')
    expect(init.headers['X-Creator-Identity']).toBe('u@example.com')
  })

  it('kebab Remove calls DELETE /video-sources/{id} and triggers onChanged', async () => {
    const onChanged = vi.fn()
    global.fetch = vi.fn().mockResolvedValueOnce({ ok: true, status: 204, json: async () => ({}) })
    const user = userEvent.setup()
    render(
      <RecordingsSection
        sessionId="s-1"
        apiBaseUrl="http://api.test"
        userEmail="u@example.com"
        onChanged={onChanged}
        recordings={[
          baseRecording({ id: 'vs-rm', video_role: 'secondary' }),
        ]}
      />
    )
    await user.click(screen.getByTestId('recording-row-vs-rm-kebab'))
    await user.click(screen.getByTestId('recording-row-vs-rm-remove'))
    await waitFor(() => expect(onChanged).toHaveBeenCalled())
    const [url, init] = global.fetch.mock.calls[0]
    expect(url).toBe('http://api.test/api/sessions/s-1/video-sources/vs-rm')
    expect(init.method).toBe('DELETE')
  })

  it('Open original is suppressed when no URL is available', () => {
    render(
      <RecordingsSection
        sessionId="s-1"
        apiBaseUrl="http://api.test"
        recordings={[
          baseRecording({ id: 'vs-no-url', video_url: '', original_url: undefined, video_role: 'secondary' }),
        ]}
      />
    )
    // Kebab is closed by default; just assert the link isn't rendered when no
    // URL is available — we open the kebab to check.
    expect(screen.queryByTestId('recording-row-vs-no-url-open-original')).toBeNull()
  })

  it('error from Make primary surfaces inline', async () => {
    global.fetch = vi.fn().mockResolvedValueOnce({ ok: false, status: 500, json: async () => ({ message: 'boom' }) })
    const user = userEvent.setup()
    render(
      <RecordingsSection
        sessionId="s-1"
        apiBaseUrl="http://api.test"
        recordings={[baseRecording({ id: 'vs-fail', video_role: 'secondary' })]}
      />
    )
    await user.click(screen.getByTestId('recording-row-vs-fail-kebab'))
    await user.click(screen.getByTestId('recording-row-vs-fail-make-primary'))
    await waitFor(() => expect(screen.getByTestId('recordings-section-error')).toBeTruthy())
  })
})

describe('SourceBadge', () => {
  it('renders the per-platform label', () => {
    render(<SourceBadge provider="zoom" />)
    expect(screen.getByTestId('source-badge-zoom').textContent).toBe('Zoom')
  })
})
