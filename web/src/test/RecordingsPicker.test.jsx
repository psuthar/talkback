import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
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
    duration_minutes: 480,
    meeting_uuid: 'big-uuid',
    instance_uuid: 'big-instance',
    has_video: true,
    has_transcript: true,
    recording_count: 1,
  },
]

const allConnectedIntegrations = {
  zoom:        { enabled: true, connected: true, account_email: 'z@example.com' },
  google_meet: { enabled: true, connected: true, account_email: 'm@example.com' },
  teams:       { enabled: true, connected: true, account_email: 't@example.com' },
}

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
      sessionId="sess-1"
      apiBaseUrl="http://api.test"
      integrations={allConnectedIntegrations}
      importedExternalIds={['imported-instance']}
      onClose={() => {}}
      onImported={() => {}}
      onConnect={() => {}}
      onSwitchAccount={() => {}}
      userEmail="u@example.com"
      initialPlatform="zoom"
      {...overrides}
    />
  )

describe('RecordingsPicker (SCRUM-463 unified)', () => {
  let originalFetch
  beforeEach(() => {
    originalFetch = global.fetch
    try { window.localStorage.removeItem('tb.import.lastPlatform') } catch (_) {}
  })
  afterEach(() => { global.fetch = originalFetch })

  it('renders as aria-modal dialog with the unified title', () => {
    global.fetch = mockFetch([{ ok: true, status: 200, json: async () => ({ items: recordings }) }])
    renderPicker()
    const dialog = screen.getByTestId('recordings-picker')
    expect(dialog.getAttribute('role')).toBe('dialog')
    expect(dialog.getAttribute('aria-modal')).toBe('true')
    expect(screen.getByText('Import meeting recording')).toBeTruthy()
  })

  it('shows pre-load placeholder before Load is clicked (no auto-fetch)', () => {
    global.fetch = vi.fn()
    renderPicker()
    expect(screen.getByTestId('recordings-picker-preload')).toBeTruthy()
    expect(screen.queryByTestId('recordings-picker-list')).toBeNull()
    expect(global.fetch).not.toHaveBeenCalled()
  })

  it('clicking Load recordings fetches and renders rows', async () => {
    global.fetch = mockFetch([{ ok: true, status: 200, json: async () => ({ items: recordings }) }])
    const user = userEvent.setup()
    renderPicker()
    await user.click(screen.getByTestId('recordings-picker-load'))
    await waitFor(() => expect(screen.getByTestId('recordings-picker-list')).toBeTruthy())
    expect(screen.getByTestId('recording-row-std-instance').getAttribute('data-state')).toBe('available')
    expect(screen.getByTestId('recording-already-imported-imported-instance')).toBeTruthy()
    expect(screen.getByTestId('recording-oversized-big-instance')).toBeTruthy()
  })

  it('Load recordings persists the platform to localStorage', async () => {
    global.fetch = mockFetch([{ ok: true, status: 200, json: async () => ({ items: [] }) }])
    const user = userEvent.setup()
    renderPicker({ initialPlatform: 'teams' })
    await user.click(screen.getByTestId('recordings-picker-load'))
    await waitFor(() => expect(screen.getByTestId('recordings-picker-empty')).toBeTruthy())
    expect(window.localStorage.getItem('tb.import.lastPlatform')).toBe('teams')
  })

  it('empty state after Load when API returns no items', async () => {
    global.fetch = mockFetch([{ ok: true, status: 200, json: async () => ({ items: [] }) }])
    const user = userEvent.setup()
    renderPicker()
    await user.click(screen.getByTestId('recordings-picker-load'))
    await waitFor(() => expect(screen.getByTestId('recordings-picker-empty')).toBeTruthy())
  })

  it('error state on non-ok response surfaces Retry', async () => {
    global.fetch = mockFetch([{ ok: false, status: 500, json: async () => ({ message: 'boom' }) }])
    const user = userEvent.setup()
    renderPicker()
    await user.click(screen.getByTestId('recordings-picker-load'))
    await waitFor(() => expect(screen.getByTestId('recordings-picker-error')).toBeTruthy())
    expect(screen.getByTestId('recordings-picker-retry')).toBeTruthy()
  })

  it('imported and oversized rows are disabled and not selectable', async () => {
    global.fetch = mockFetch([{ ok: true, status: 200, json: async () => ({ items: recordings }) }])
    const user = userEvent.setup()
    renderPicker()
    await user.click(screen.getByTestId('recordings-picker-load'))
    await waitFor(() => expect(screen.getByTestId('recordings-picker-list')).toBeTruthy())
    expect(screen.getByTestId('recording-checkbox-imported-instance').disabled).toBe(true)
    expect(screen.getByTestId('recording-checkbox-big-instance').disabled).toBe(true)
  })

  it('multi-select happy path: select two → confirm → POST each → onImported fires', async () => {
    const onImported = vi.fn()
    const onClose = vi.fn()
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
      { ok: true, status: 200, json: async () => ({ items: extra }) },
      { ok: true, status: 202, json: async () => ({ job_id: 'j1', state: 'queued' }) },
      { ok: true, status: 202, json: async () => ({ job_id: 'j2', state: 'queued' }) },
    ])
    const user = userEvent.setup()
    renderPicker({ onImported, onClose })
    await user.click(screen.getByTestId('recordings-picker-load'))
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

  it('429 cap-exceeded stops the batch and surfaces an error', async () => {
    global.fetch = mockFetch([
      { ok: true, status: 200, json: async () => ({ items: recordings }) },
      { ok: false, status: 429, json: async () => ({ error: 'session_recording_cap_exceeded', cap: 10, current: 10 }) },
    ])
    const user = userEvent.setup()
    renderPicker()
    await user.click(screen.getByTestId('recordings-picker-load'))
    await waitFor(() => expect(screen.getByTestId('recordings-picker-list')).toBeTruthy())
    await user.click(screen.getByTestId('recording-checkbox-std-instance'))
    await user.click(screen.getByTestId('recordings-picker-import'))
    await user.click(screen.getByTestId('recordings-picker-confirm-button'))
    await waitFor(() => expect(screen.getByTestId('recordings-picker-import-errors')).toBeTruthy())
    expect(screen.getByTestId('recordings-picker-import-error-0').textContent).toContain('Cap exceeded')
  })

  // ──────────────────────────────────────────────────────────────────
  // SCRUM-462: modal redesign — popup, X close, transcript icon, debounce.
  // ──────────────────────────────────────────────────────────────────

  it('SCRUM-462: backdrop click closes; dialog-card click does NOT', async () => {
    const onClose = vi.fn()
    global.fetch = mockFetch([{ ok: true, status: 200, json: async () => ({ items: [] }) }])
    const user = userEvent.setup()
    renderPicker({ onClose })
    await user.click(screen.getByTestId('recordings-picker-dialog'))
    expect(onClose).not.toHaveBeenCalled()
    await user.click(screen.getByTestId('recordings-picker'))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('SCRUM-462: Esc closes', async () => {
    const onClose = vi.fn()
    global.fetch = mockFetch([{ ok: true, status: 200, json: async () => ({ items: [] }) }])
    const user = userEvent.setup()
    renderPicker({ onClose })
    await user.keyboard('{Escape}')
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('SCRUM-462: × close affordance has aria-label and glyph', () => {
    global.fetch = mockFetch([{ ok: true, status: 200, json: async () => ({ items: [] }) }])
    renderPicker()
    const closeBtn = screen.getByTestId('recordings-picker-close')
    expect(closeBtn.getAttribute('aria-label')).toBe('Close recordings picker')
    expect(closeBtn.textContent.trim()).toBe('×')
  })

  it('SCRUM-462: rows with has_transcript=true render the transcript icon', async () => {
    global.fetch = mockFetch([{ ok: true, status: 200, json: async () => ({ items: recordings }) }])
    const user = userEvent.setup()
    renderPicker()
    await user.click(screen.getByTestId('recordings-picker-load'))
    await waitFor(() => expect(screen.getByTestId('recordings-picker-list')).toBeTruthy())
    expect(screen.getByTestId('recording-transcript-icon-std-instance')).toBeTruthy()
    expect(screen.queryByTestId('recording-transcript-icon-imported-instance')).toBeNull()
  })

  it('SCRUM-462: typing in search auto-refetches AFTER initial Load (debounced)', async () => {
    const fetchSpy = vi.fn()
      .mockResolvedValueOnce({ ok: true, status: 200, json: async () => ({ items: recordings }) })
      .mockResolvedValueOnce({ ok: true, status: 200, json: async () => ({ items: [] }) })
    global.fetch = fetchSpy
    const user = userEvent.setup()
    renderPicker()
    await user.click(screen.getByTestId('recordings-picker-load'))
    await waitFor(() => expect(fetchSpy).toHaveBeenCalledTimes(1), { timeout: 1500 })
    fireEvent.change(screen.getByTestId('recordings-picker-search'), { target: { value: 'stand' } })
    await waitFor(() => expect(fetchSpy.mock.calls.length).toBeGreaterThanOrEqual(2), { timeout: 1500 })
    expect(String(fetchSpy.mock.calls.at(-1)[0])).toMatch(/[?&]q=stand/)
  })

  // ──────────────────────────────────────────────────────────────────
  // SCRUM-463: unified entry — platform selector, Load gating, etc.
  // ──────────────────────────────────────────────────────────────────

  it('SCRUM-463: renders segmented platform selector with three options', () => {
    global.fetch = mockFetch([{ ok: true, status: 200, json: async () => ({ items: [] }) }])
    renderPicker()
    expect(screen.getByTestId('recordings-picker-platform-selector')).toBeTruthy()
    expect(screen.getByTestId('recordings-picker-platform-zoom')).toBeTruthy()
    expect(screen.getByTestId('recordings-picker-platform-google_meet')).toBeTruthy()
    expect(screen.getByTestId('recordings-picker-platform-teams')).toBeTruthy()
  })

  it('SCRUM-463: each platform segment shows a connection-status dot', () => {
    global.fetch = mockFetch([{ ok: true, status: 200, json: async () => ({ items: [] }) }])
    renderPicker({
      integrations: {
        zoom:        { enabled: true, connected: true, account_email: 'z@example.com' },
        google_meet: { enabled: true, connected: false },
        teams:       { enabled: true, connected: true, account_email: 't@example.com' },
      },
    })
    expect(screen.getByTestId('recordings-picker-platform-zoom-status').getAttribute('data-status')).toBe('connected')
    expect(screen.getByTestId('recordings-picker-platform-google_meet-status').getAttribute('data-status')).toBe('disconnected')
    expect(screen.getByTestId('recordings-picker-platform-teams-status').getAttribute('data-status')).toBe('connected')
  })

  it('SCRUM-463: clicking a different platform resets filters + clears list + needs Load again', async () => {
    global.fetch = mockFetch([{ ok: true, status: 200, json: async () => ({ items: recordings }) }])
    const user = userEvent.setup()
    renderPicker()
    // First Load on Zoom populates the list.
    await user.click(screen.getByTestId('recordings-picker-load'))
    await waitFor(() => expect(screen.getByTestId('recordings-picker-list')).toBeTruthy())
    // Type a search to verify filter state.
    fireEvent.change(screen.getByTestId('recordings-picker-search'), { target: { value: 'stand' } })
    expect(screen.getByTestId('recordings-picker-search').value).toBe('stand')
    // Switch to Google Meet — list clears, filters reset, pre-load placeholder back.
    await user.click(screen.getByTestId('recordings-picker-platform-google_meet'))
    expect(screen.queryByTestId('recordings-picker-list')).toBeNull()
    expect(screen.getByTestId('recordings-picker-preload')).toBeTruthy()
    expect(screen.getByTestId('recordings-picker-search').value).toBe('')
  })

  it('SCRUM-463: disconnected platform shows inline Connect CTA; Load button hidden', () => {
    global.fetch = vi.fn()
    renderPicker({
      integrations: {
        zoom:        { enabled: true, connected: false },
        google_meet: { enabled: true, connected: false },
        teams:       { enabled: true, connected: false },
      },
    })
    expect(screen.getByTestId('recordings-picker-disconnected')).toBeTruthy()
    expect(screen.getByTestId('recordings-picker-connect-zoom')).toBeTruthy()
    // Filters + Load are NOT in the disconnected variant.
    expect(screen.queryByTestId('recordings-picker-load')).toBeNull()
    expect(screen.queryByTestId('recordings-picker-search')).toBeNull()
  })

  it('SCRUM-463: Connect CTA in the disconnected variant fires onConnect with the active platform', async () => {
    const onConnect = vi.fn()
    global.fetch = vi.fn()
    const user = userEvent.setup()
    renderPicker({
      onConnect,
      integrations: {
        zoom:        { enabled: true, connected: false },
        google_meet: { enabled: true, connected: false },
        teams:       { enabled: true, connected: true, account_email: 't@example.com' },
      },
      initialPlatform: 'zoom',
    })
    await user.click(screen.getByTestId('recordings-picker-connect-zoom'))
    expect(onConnect).toHaveBeenCalledWith('zoom')
  })

  it('SCRUM-463: default platform is last-used from localStorage if available', () => {
    try { window.localStorage.setItem('tb.import.lastPlatform', 'teams') } catch (_) {}
    global.fetch = vi.fn()
    renderPicker({ initialPlatform: undefined })
    expect(screen.getByTestId('recordings-picker-platform-teams').getAttribute('data-active')).toBe('true')
  })

  it('SCRUM-463: with no lastPlatform, default is first connected platform', () => {
    try { window.localStorage.removeItem('tb.import.lastPlatform') } catch (_) {}
    global.fetch = vi.fn()
    renderPicker({
      initialPlatform: undefined,
      integrations: {
        zoom:        { enabled: true, connected: false },
        google_meet: { enabled: true, connected: true, account_email: 'm@example.com' },
        teams:       { enabled: true, connected: true, account_email: 't@example.com' },
      },
    })
    expect(screen.getByTestId('recordings-picker-platform-google_meet').getAttribute('data-active')).toBe('true')
  })

  it('SCRUM-463: Switch account fires onSwitchAccount with active platform', async () => {
    const onSwitchAccount = vi.fn()
    global.fetch = mockFetch([{ ok: true, status: 200, json: async () => ({ items: [] }) }])
    const user = userEvent.setup()
    renderPicker({ onSwitchAccount, initialPlatform: 'zoom' })
    await user.click(screen.getByTestId('recordings-picker-switch-account'))
    expect(onSwitchAccount).toHaveBeenCalledWith('zoom')
  })

  // ──────────────────────────────────────────────────────────────────
  // SCRUM-464: empty apiBaseUrl is valid (same-origin SPA). Same fix
  // pattern as SCRUM-459 — guard must not bail on falsy base.
  // ──────────────────────────────────────────────────────────────────

  it('SCRUM-464: Load recordings fires the fetch with a same-origin relative URL when apiBaseUrl=""', async () => {
    const fetchSpy = vi.fn().mockResolvedValueOnce({ ok: true, status: 200, json: async () => ({ items: recordings }) })
    global.fetch = fetchSpy
    const user = userEvent.setup()
    renderPicker({ apiBaseUrl: '' })
    await user.click(screen.getByTestId('recordings-picker-load'))
    await waitFor(() => expect(fetchSpy).toHaveBeenCalledTimes(1), { timeout: 1500 })
    const url = String(fetchSpy.mock.calls[0][0])
    expect(url.startsWith('/api/zoom/recordings')).toBe(true)
  })

  // ──────────────────────────────────────────────────────────────────
  // SCRUM-465: selection collision + stale confirm dialog on platform switch.
  // ──────────────────────────────────────────────────────────────────

  it('SCRUM-465: rows with empty external IDs do NOT collide in selection', async () => {
    const collidingRows = [
      { meeting_topic: 'A', start_time: '2026-03-21T21:00:00Z', duration_minutes: 30, meeting_uuid: '', instance_uuid: '', has_transcript: false },
      { meeting_topic: 'B', start_time: '2026-03-21T21:00:00Z', duration_minutes: 30, meeting_uuid: '', instance_uuid: '', has_transcript: false },
      { meeting_topic: 'C', start_time: '2026-03-21T21:20:00Z', duration_minutes: 20, meeting_uuid: '', instance_uuid: '', has_transcript: false },
    ]
    global.fetch = mockFetch([{ ok: true, status: 200, json: async () => ({ items: collidingRows }) }])
    const user = userEvent.setup()
    renderPicker()
    await user.click(screen.getByTestId('recordings-picker-load'))
    await waitFor(() => expect(screen.getByTestId('recordings-picker-list')).toBeTruthy())
    // Click the FIRST row's checkbox. Only one row should select.
    const checkboxes = screen.getAllByRole('checkbox')
    expect(checkboxes.length).toBe(3)
    await user.click(checkboxes[0])
    expect(checkboxes[0].checked).toBe(true)
    expect(checkboxes[1].checked).toBe(false)
    expect(checkboxes[2].checked).toBe(false)
    // Import label reflects ONE selection.
    expect(screen.getByTestId('recordings-picker-import').textContent).toBe('Import 1 recording')
  })

  it('SCRUM-465: switching platforms clears the confirm dialog and stale import errors', async () => {
    global.fetch = vi.fn()
      .mockResolvedValueOnce({ ok: true, status: 200, json: async () => ({ items: recordings }) })
      .mockResolvedValueOnce({ ok: false, status: 401, json: async () => ({ message: 'unauthorized' }) })
    const user = userEvent.setup()
    renderPicker({ initialPlatform: 'zoom' })
    await user.click(screen.getByTestId('recordings-picker-load'))
    await waitFor(() => expect(screen.getByTestId('recordings-picker-list')).toBeTruthy())
    await user.click(screen.getByTestId('recording-checkbox-std-instance'))
    await user.click(screen.getByTestId('recordings-picker-import'))
    await user.click(screen.getByTestId('recordings-picker-confirm-button'))
    // Confirm dialog open + error visible.
    await waitFor(() => expect(screen.getByTestId('recordings-picker-import-errors')).toBeTruthy())
    expect(screen.getByTestId('recordings-picker-confirm')).toBeTruthy()
    // Switch to a different platform → confirm dialog dismounts, import errors gone.
    await user.click(screen.getByTestId('recordings-picker-platform-teams'))
    expect(screen.queryByTestId('recordings-picker-confirm')).toBeNull()
    expect(screen.queryByTestId('recordings-picker-import-errors')).toBeNull()
  })

  it('SCRUM-464: Import POST fires with a same-origin relative URL when apiBaseUrl=""', async () => {
    const onImported = vi.fn()
    global.fetch = vi.fn()
      .mockResolvedValueOnce({ ok: true, status: 200, json: async () => ({ items: recordings }) })
      .mockResolvedValueOnce({ ok: true, status: 202, json: async () => ({ job_id: 'j1' }) })
    const user = userEvent.setup()
    renderPicker({ apiBaseUrl: '', onImported })
    await user.click(screen.getByTestId('recordings-picker-load'))
    await waitFor(() => expect(screen.getByTestId('recordings-picker-list')).toBeTruthy())
    await user.click(screen.getByTestId('recording-checkbox-std-instance'))
    await user.click(screen.getByTestId('recordings-picker-import'))
    await user.click(screen.getByTestId('recordings-picker-confirm-button'))
    await waitFor(() => expect(onImported).toHaveBeenCalled())
    const postCall = global.fetch.mock.calls.find(([_url, init]) => init?.method === 'POST')
    expect(postCall).toBeTruthy()
    expect(String(postCall[0]).startsWith('/api/sessions/sess-1/import/')).toBe(true)
  })

  // ──────────────────────────────────────────────────────────────────
  // SCRUM-479: create-mode — picker drives Create Session flow via
  // chained POST /sessions + POST /api/sessions/{id}/import/{platform}.
  // ──────────────────────────────────────────────────────────────────

  it('SCRUM-479: create-mode header reads "Create from meeting recording"', () => {
    global.fetch = mockFetch([{ ok: true, status: 200, json: async () => ({ items: [] }) }])
    renderPicker({ mode: 'create', sessionId: undefined })
    expect(screen.getByText('Create from meeting recording')).toBeTruthy()
    expect(screen.queryByText('Import meeting recording')).toBeNull()
  })

  it('SCRUM-479: create-mode enforces single-select (picking a second row deselects the first)', async () => {
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
    global.fetch = mockFetch([{ ok: true, status: 200, json: async () => ({ items: extra }) }])
    const user = userEvent.setup()
    renderPicker({ mode: 'create', sessionId: undefined })
    await user.click(screen.getByTestId('recordings-picker-load'))
    await waitFor(() => expect(screen.getByTestId('recordings-picker-list')).toBeTruthy())
    await user.click(screen.getByTestId('recording-checkbox-std-instance'))
    expect(screen.getByTestId('recording-checkbox-std-instance').checked).toBe(true)
    await user.click(screen.getByTestId('recording-checkbox-second-instance'))
    expect(screen.getByTestId('recording-checkbox-std-instance').checked).toBe(false)
    expect(screen.getByTestId('recording-checkbox-second-instance').checked).toBe(true)
  })

  it('SCRUM-479: create-mode footer button reads "Continue"', async () => {
    global.fetch = mockFetch([{ ok: true, status: 200, json: async () => ({ items: recordings }) }])
    const user = userEvent.setup()
    renderPicker({ mode: 'create', sessionId: undefined })
    await user.click(screen.getByTestId('recordings-picker-load'))
    await waitFor(() => expect(screen.getByTestId('recordings-picker-list')).toBeTruthy())
    await user.click(screen.getByTestId('recording-checkbox-std-instance'))
    expect(screen.getByTestId('recordings-picker-import').textContent).toBe('Continue')
  })

  it('SCRUM-479: create-mode confirm dialog renders title field pre-filled from recording', async () => {
    global.fetch = mockFetch([{ ok: true, status: 200, json: async () => ({ items: recordings }) }])
    const user = userEvent.setup()
    renderPicker({ mode: 'create', sessionId: undefined })
    await user.click(screen.getByTestId('recordings-picker-load'))
    await waitFor(() => expect(screen.getByTestId('recordings-picker-list')).toBeTruthy())
    await user.click(screen.getByTestId('recording-checkbox-std-instance'))
    await user.click(screen.getByTestId('recordings-picker-import'))
    const titleInput = screen.getByTestId('recordings-picker-confirm-title')
    expect(titleInput).toBeTruthy()
    expect(titleInput.value).toBe('Standup')
  })

  it('SCRUM-479: create-mode empty title blocks submit and surfaces validation error', async () => {
    global.fetch = mockFetch([{ ok: true, status: 200, json: async () => ({ items: recordings }) }])
    const user = userEvent.setup()
    renderPicker({ mode: 'create', sessionId: undefined })
    await user.click(screen.getByTestId('recordings-picker-load'))
    await waitFor(() => expect(screen.getByTestId('recordings-picker-list')).toBeTruthy())
    await user.click(screen.getByTestId('recording-checkbox-std-instance'))
    await user.click(screen.getByTestId('recordings-picker-import'))
    fireEvent.change(screen.getByTestId('recordings-picker-confirm-title'), { target: { value: '  ' } })
    await user.click(screen.getByTestId('recordings-picker-confirm-button'))
    expect(screen.getByTestId('recordings-picker-confirm-title-error')).toBeTruthy()
    // No POST should have fired (only the initial GET listing call).
    const postCalls = global.fetch.mock.calls.filter(([_url, init]) => init?.method === 'POST')
    expect(postCalls.length).toBe(0)
  })

  it('SCRUM-479: create-mode chains POST /sessions then POST /api/sessions/{id}/import/{platform}', async () => {
    const onImported = vi.fn()
    const onClose = vi.fn()
    global.fetch = vi.fn()
      .mockResolvedValueOnce({ ok: true, status: 200, json: async () => ({ items: recordings }) })
      .mockResolvedValueOnce({ ok: true, status: 201, json: async () => ({ id: 'new-sess-99', title: 'Standup' }) })
      .mockResolvedValueOnce({ ok: true, status: 202, json: async () => ({ job_id: 'j-create', state: 'queued' }) })
    const user = userEvent.setup()
    renderPicker({ mode: 'create', sessionId: undefined, apiBaseUrl: '', onImported, onClose })
    await user.click(screen.getByTestId('recordings-picker-load'))
    await waitFor(() => expect(screen.getByTestId('recordings-picker-list')).toBeTruthy())
    await user.click(screen.getByTestId('recording-checkbox-std-instance'))
    await user.click(screen.getByTestId('recordings-picker-import'))
    await user.click(screen.getByTestId('recordings-picker-confirm-button'))
    await waitFor(() => expect(onImported).toHaveBeenCalled())
    const postCalls = global.fetch.mock.calls.filter(([_url, init]) => init?.method === 'POST')
    expect(postCalls.length).toBe(2)
    expect(String(postCalls[0][0])).toBe('/sessions')
    expect(JSON.parse(postCalls[0][1].body)).toEqual({ title: 'Standup' })
    expect(String(postCalls[1][0])).toBe('/api/sessions/new-sess-99/import/zoom')
    expect(JSON.parse(postCalls[1][1].body)).toEqual({ meeting_uuid: 'std-uuid', instance_uuid: 'std-instance' })
    expect(onImported.mock.calls[0][0]).toHaveLength(1)
    expect(onImported.mock.calls[0][0][0].session).toEqual({ id: 'new-sess-99', title: 'Standup' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('SCRUM-479: create-mode 409 on session create surfaces "already exists" error and skips attach POST', async () => {
    global.fetch = vi.fn()
      .mockResolvedValueOnce({ ok: true, status: 200, json: async () => ({ items: recordings }) })
      .mockResolvedValueOnce({ ok: false, status: 409, json: async () => ({ error: 'session already exists' }) })
    const user = userEvent.setup()
    renderPicker({ mode: 'create', sessionId: undefined, apiBaseUrl: '' })
    await user.click(screen.getByTestId('recordings-picker-load'))
    await waitFor(() => expect(screen.getByTestId('recordings-picker-list')).toBeTruthy())
    await user.click(screen.getByTestId('recording-checkbox-std-instance'))
    await user.click(screen.getByTestId('recordings-picker-import'))
    await user.click(screen.getByTestId('recordings-picker-confirm-button'))
    await waitFor(() => expect(screen.getByTestId('recordings-picker-import-errors')).toBeTruthy())
    expect(screen.getByTestId('recordings-picker-import-error-0').textContent).toContain('already exists')
    const postCalls = global.fetch.mock.calls.filter(([_url, init]) => init?.method === 'POST')
    // Only the failed create-session POST — no attach POST.
    expect(postCalls.length).toBe(1)
    expect(String(postCalls[0][0])).toBe('/sessions')
  })

  it('SCRUM-479: create-mode does not require sessionId and Continue is enabled after select', async () => {
    global.fetch = mockFetch([{ ok: true, status: 200, json: async () => ({ items: recordings }) }])
    const user = userEvent.setup()
    renderPicker({ mode: 'create', sessionId: undefined })
    await user.click(screen.getByTestId('recordings-picker-load'))
    await waitFor(() => expect(screen.getByTestId('recordings-picker-list')).toBeTruthy())
    const importBtn = screen.getByTestId('recordings-picker-import')
    expect(importBtn.disabled).toBe(true)
    await user.click(screen.getByTestId('recording-checkbox-std-instance'))
    expect(importBtn.disabled).toBe(false)
  })

  it('SCRUM-479: attach-mode (default) keeps multi-select behavior — no regression', async () => {
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
    global.fetch = mockFetch([{ ok: true, status: 200, json: async () => ({ items: extra }) }])
    const user = userEvent.setup()
    renderPicker()
    await user.click(screen.getByTestId('recordings-picker-load'))
    await waitFor(() => expect(screen.getByTestId('recordings-picker-list')).toBeTruthy())
    await user.click(screen.getByTestId('recording-checkbox-std-instance'))
    await user.click(screen.getByTestId('recording-checkbox-second-instance'))
    expect(screen.getByTestId('recording-checkbox-std-instance').checked).toBe(true)
    expect(screen.getByTestId('recording-checkbox-second-instance').checked).toBe(true)
  })
})
