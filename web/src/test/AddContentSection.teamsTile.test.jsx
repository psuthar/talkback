import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AddContentSection } from '../components/AddContentSection'

const statusBody = (overrides = {}) => ({
  zoom: { enabled: false, connected: false, account_email: null },
  google_meet: { enabled: false, connected: false, account_email: null },
  teams: { enabled: true, connected: false, account_email: null },
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

describe('AddContentSection Microsoft Teams tile (SCRUM-423)', () => {
  let originalFetch, originalWindowOpen
  beforeEach(() => {
    originalFetch = global.fetch
    originalWindowOpen = window.open
  })
  afterEach(() => {
    global.fetch = originalFetch
    window.open = originalWindowOpen
  })

  it('renders the Teams tile when integrations.teams.enabled=true', async () => {
    global.fetch = mockFetch([{ ok: true, status: 200, json: async () => statusBody() }])
    render(<AddContentSection sessionId="s1" apiBaseUrl="http://api.test" />)
    await waitFor(() => expect(screen.getByTestId('platform-tile-teams')).toBeTruthy())
    expect(screen.getByTestId('platform-tile-teams').getAttribute('data-state')).toBe('unconnected')
    expect(screen.getByTestId('platform-tile-teams-connect').textContent).toContain('Connect Microsoft Teams')
  })

  it('hides the Teams tile when enabled=false', async () => {
    global.fetch = mockFetch([
      {
        ok: true,
        status: 200,
        json: async () => statusBody({ teams: { enabled: false, connected: false, account_email: null } }),
      },
    ])
    render(<AddContentSection sessionId="s1" apiBaseUrl="http://api.test" />)
    await waitFor(() => expect(global.fetch).toHaveBeenCalled())
    expect(screen.queryByTestId('platform-tile-teams')).toBeNull()
  })

  it('connected Teams tile shows browse + account email', async () => {
    global.fetch = mockFetch([
      {
        ok: true,
        status: 200,
        json: async () => statusBody({
          teams: { enabled: true, connected: true, account_email: 't@example.com' },
        }),
      },
    ])
    render(<AddContentSection sessionId="s1" apiBaseUrl="http://api.test" onBrowseTeams={() => {}} />)
    await waitFor(() => expect(screen.getByTestId('platform-tile-teams').getAttribute('data-state')).toBe('connected'))
    expect(screen.getByTestId('platform-tile-teams-browse').textContent).toContain('Browse Teams recordings')
    expect(screen.getByTestId('platform-tile-teams-account').textContent).toBe('t@example.com')
  })

  it('Connect Microsoft Teams opens the OAuth popup at /api/teams/connect', async () => {
    global.fetch = mockFetch([{ ok: true, status: 200, json: async () => statusBody() }])
    const popupStub = { closed: true }
    window.open = vi.fn().mockReturnValue(popupStub)
    const user = userEvent.setup()
    render(<AddContentSection sessionId="s1" apiBaseUrl="http://api.test" />)
    await waitFor(() => expect(screen.getByTestId('platform-tile-teams-connect')).toBeTruthy())
    await user.click(screen.getByTestId('platform-tile-teams-connect'))
    expect(window.open).toHaveBeenCalledWith(
      'http://api.test/api/teams/connect',
      'Microsoft Teams_oauth',
      'width=600,height=720'
    )
  })

  it('Browse Teams recordings fires onBrowseTeams', async () => {
    const onBrowseTeams = vi.fn()
    global.fetch = mockFetch([
      {
        ok: true,
        status: 200,
        json: async () => statusBody({
          teams: { enabled: true, connected: true, account_email: 't@example.com' },
        }),
      },
    ])
    const user = userEvent.setup()
    render(<AddContentSection sessionId="s1" apiBaseUrl="http://api.test" onBrowseTeams={onBrowseTeams} />)
    await waitFor(() => expect(screen.getByTestId('platform-tile-teams-browse')).toBeTruthy())
    await user.click(screen.getByTestId('platform-tile-teams-browse'))
    expect(onBrowseTeams).toHaveBeenCalledTimes(1)
  })
})
