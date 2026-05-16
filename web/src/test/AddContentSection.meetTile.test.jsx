import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AddContentSection } from '../components/AddContentSection'

const statusBody = (overrides = {}) => ({
  zoom: { enabled: false, connected: false, account_email: null },
  google_meet: { enabled: true, connected: false, account_email: null },
  teams: { enabled: false, connected: false, account_email: null },
  ...overrides,
})

const mockFetch = (responses) => {
  let i = 0
  return vi.fn().mockImplementation(() => {
    const next = responses[Math.min(i, responses.length - 1)]
    i++
    return Promise.resolve(next)
  })
}

describe('AddContentSection Google Meet tile (SCRUM-422)', () => {
  let originalFetch
  let originalWindowOpen
  beforeEach(() => {
    originalFetch = global.fetch
    originalWindowOpen = window.open
  })
  afterEach(() => {
    global.fetch = originalFetch
    window.open = originalWindowOpen
  })

  it('renders the Google Meet tile when integrations.google_meet.enabled=true', async () => {
    global.fetch = mockFetch([{ ok: true, status: 200, json: async () => statusBody() }])
    render(<AddContentSection sessionId="s1" apiBaseUrl="http://api.test" />)
    await waitFor(() => expect(screen.getByTestId('platform-tile-google_meet')).toBeTruthy())
    expect(screen.getByTestId('platform-tile-google_meet').getAttribute('data-state')).toBe('unconnected')
    expect(screen.getByTestId('platform-tile-google_meet-connect').textContent).toContain('Connect Google Meet')
  })

  it('hides the Meet tile when enabled=false', async () => {
    global.fetch = mockFetch([
      {
        ok: true,
        status: 200,
        json: async () => statusBody({ google_meet: { enabled: false, connected: false, account_email: null } }),
      },
    ])
    render(<AddContentSection sessionId="s1" apiBaseUrl="http://api.test" />)
    await waitFor(() => expect(global.fetch).toHaveBeenCalled())
    expect(screen.queryByTestId('platform-tile-google_meet')).toBeNull()
  })

  it('connected Meet tile shows the browse CTA + account email', async () => {
    global.fetch = mockFetch([
      {
        ok: true,
        status: 200,
        json: async () => statusBody({
          google_meet: { enabled: true, connected: true, account_email: 'meet@example.com' },
        }),
      },
    ])
    render(<AddContentSection sessionId="s1" apiBaseUrl="http://api.test" onBrowseGoogleMeet={() => {}} />)
    await waitFor(() => expect(screen.getByTestId('platform-tile-google_meet').getAttribute('data-state')).toBe('connected'))
    expect(screen.getByTestId('platform-tile-google_meet-browse').textContent).toContain('Browse Google Meet recordings')
    expect(screen.getByTestId('platform-tile-google_meet-account').textContent).toBe('meet@example.com')
  })

  it('Connect Google Meet opens the OAuth popup at /api/google-meet/connect', async () => {
    global.fetch = mockFetch([{ ok: true, status: 200, json: async () => statusBody() }])
    const popupStub = { closed: true }
    window.open = vi.fn().mockReturnValue(popupStub)
    const user = userEvent.setup()
    render(<AddContentSection sessionId="s1" apiBaseUrl="http://api.test" />)
    await waitFor(() => expect(screen.getByTestId('platform-tile-google_meet-connect')).toBeTruthy())
    await user.click(screen.getByTestId('platform-tile-google_meet-connect'))
    expect(window.open).toHaveBeenCalledWith(
      'http://api.test/api/google-meet/connect',
      'Google Meet_oauth',
      'width=600,height=720'
    )
  })

  it('Browse Google Meet recordings fires onBrowseGoogleMeet', async () => {
    const onBrowseGoogleMeet = vi.fn()
    global.fetch = mockFetch([
      {
        ok: true,
        status: 200,
        json: async () => statusBody({
          google_meet: { enabled: true, connected: true, account_email: 'meet@example.com' },
        }),
      },
    ])
    const user = userEvent.setup()
    render(<AddContentSection sessionId="s1" apiBaseUrl="http://api.test" onBrowseGoogleMeet={onBrowseGoogleMeet} />)
    await waitFor(() => expect(screen.getByTestId('platform-tile-google_meet-browse')).toBeTruthy())
    await user.click(screen.getByTestId('platform-tile-google_meet-browse'))
    expect(onBrowseGoogleMeet).toHaveBeenCalledTimes(1)
  })
})
