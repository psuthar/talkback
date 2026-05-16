import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { RecordingsPicker } from '../components/RecordingsPicker'

const recordings = [
  {
    meeting_topic: 'Standup',
    start_time: '2026-05-10T15:00:00Z',
    duration_minutes: 30,
    meeting_uuid: 'std-uuid',
    instance_uuid: 'std-instance',
    has_video: true,
    has_transcript: true,
    recording_count: 2,
  },
  {
    meeting_topic: 'Already imported one',
    start_time: '2026-05-09T15:00:00Z',
    duration_minutes: 45,
    meeting_uuid: 'imported-uuid',
    instance_uuid: 'imported-instance',
    has_video: true,
    has_transcript: false,
    recording_count: 1,
  },
  {
    meeting_topic: 'Day-long workshop',
    start_time: '2026-05-08T09:00:00Z',
    duration_minutes: 480, // 8h, oversized
    meeting_uuid: 'big-uuid',
    instance_uuid: 'big-instance',
    has_video: true,
    has_transcript: true,
    recording_count: 1,
  },
]

const mockFetch = (responses) => {
  let i = 0
  return vi.fn().mockImplementation(() => {
    const next = responses[Math.min(i, responses.length - 1)]
    i++
    return Promise.resolve(next)
  })
}

const renderPicker = (overrides = {}) =>
  render(
    <RecordingsPicker
      platform="zoom"
      sessionId="sess-1"
      apiBaseUrl="http://api.test"
      accountEmail="z@example.com"
      importedExternalIds={['imported-instance']}
      onClose={() => {}}
      onImported={() => {}}
      userEmail="u@example.com"
      {...overrides}
    />
  )

describe('RecordingsPicker', () => {
  let originalFetch
  beforeEach(() => {
    originalFetch = global.fetch
  })
  afterEach(() => {
    global.fetch = originalFetch
  })

  it('shows loading then the recording rows', async () => {
    global.fetch = mockFetch([{ ok: true, status: 200, json: async () => ({ items: recordings }) }])
    renderPicker()
    await waitFor(() => expect(screen.getByTestId('recordings-picker-list')).toBeTruthy())
    expect(screen.getByTestId('recording-row-std-instance').getAttribute('data-state')).toBe('available')
    expect(screen.getByTestId('recording-already-imported-imported-instance')).toBeTruthy()
    expect(screen.getByTestId('recording-oversized-big-instance')).toBeTruthy()
  })

  it('shows the empty state when the API returns an empty list', async () => {
    global.fetch = mockFetch([{ ok: true, status: 200, json: async () => ({ items: [] }) }])
    renderPicker()
    await waitFor(() => expect(screen.getByTestId('recordings-picker-empty')).toBeTruthy())
  })

  it('shows the error state on non-ok response', async () => {
    global.fetch = mockFetch([{ ok: false, status: 500, json: async () => ({ message: 'boom' }) }])
    renderPicker()
    await waitFor(() => expect(screen.getByTestId('recordings-picker-error')).toBeTruthy())
  })

  it('imported and oversized rows are disabled and not selectable', async () => {
    global.fetch = mockFetch([{ ok: true, status: 200, json: async () => ({ items: recordings }) }])
    renderPicker()
    await waitFor(() => expect(screen.getByTestId('recordings-picker-list')).toBeTruthy())

    expect(screen.getByTestId('recording-checkbox-imported-instance').disabled).toBe(true)
    expect(screen.getByTestId('recording-checkbox-big-instance').disabled).toBe(true)
  })

  it('multi-select happy path: select two → confirm → POST each → onImported fires', async () => {
    const onImported = vi.fn()
    const onClose = vi.fn()
    // Add a 4th recording so we have 2 selectable rows.
    const extra = [...recordings, {
      meeting_topic: 'Second selectable',
      start_time: '2026-05-11T15:00:00Z',
      duration_minutes: 20,
      meeting_uuid: 'second-uuid',
      instance_uuid: 'second-instance',
      has_video: true,
      has_transcript: true,
      recording_count: 1,
    }]
    global.fetch = mockFetch([
      { ok: true, status: 200, json: async () => ({ items: extra }) }, // list
      { ok: true, status: 202, json: async () => ({ job_id: 'j1', state: 'queued' }) }, // attach 1
      { ok: true, status: 202, json: async () => ({ job_id: 'j2', state: 'queued' }) }, // attach 2
    ])
    const user = userEvent.setup()
    renderPicker({ onImported, onClose })
    await waitFor(() => expect(screen.getByTestId('recordings-picker-list')).toBeTruthy())

    await user.click(screen.getByTestId('recording-checkbox-std-instance'))
    await user.click(screen.getByTestId('recording-checkbox-second-instance'))
    expect(screen.getByTestId('recordings-picker-import').textContent).toContain('Import 2 recordings')

    await user.click(screen.getByTestId('recordings-picker-import'))
    expect(screen.getByTestId('recordings-picker-confirm')).toBeTruthy()
    await user.click(screen.getByTestId('recordings-picker-confirm-button'))

    await waitFor(() => expect(onImported).toHaveBeenCalledTimes(1))
    expect(onImported.mock.calls[0][0]).toHaveLength(2)
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('429 cap-exceeded stops the batch and surfaces an error in the confirm dialog', async () => {
    global.fetch = mockFetch([
      { ok: true, status: 200, json: async () => ({ items: recordings }) },
      { ok: false, status: 429, json: async () => ({ error: 'session_recording_cap_exceeded', cap: 10, current: 10 }) },
    ])
    const user = userEvent.setup()
    renderPicker()
    await waitFor(() => expect(screen.getByTestId('recordings-picker-list')).toBeTruthy())

    await user.click(screen.getByTestId('recording-checkbox-std-instance'))
    await user.click(screen.getByTestId('recordings-picker-import'))
    await user.click(screen.getByTestId('recordings-picker-confirm-button'))

    await waitFor(() => expect(screen.getByTestId('recordings-picker-import-errors')).toBeTruthy())
    expect(screen.getByTestId('recordings-picker-import-error-0').textContent).toContain('Cap exceeded')
  })

  it('Switch account fires onSwitchAccount', async () => {
    const onSwitchAccount = vi.fn()
    global.fetch = mockFetch([{ ok: true, status: 200, json: async () => ({ items: recordings }) }])
    const user = userEvent.setup()
    renderPicker({ onSwitchAccount })
    await waitFor(() => expect(screen.getByTestId('recordings-picker-list')).toBeTruthy())
    await user.click(screen.getByTestId('recordings-picker-switch-account'))
    expect(onSwitchAccount).toHaveBeenCalledTimes(1)
  })
})
