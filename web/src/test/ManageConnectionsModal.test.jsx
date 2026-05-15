import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ManageConnectionsModal } from '../components/ManageConnectionsModal'

const statusBody = {
  zoom: { enabled: true, connected: true, account_email: 'z@example.com' },
  google_meet: { enabled: true, connected: false, account_email: null },
  teams: { enabled: false, connected: false, account_email: null },
}

const mockFetch = (responses) => {
  let i = 0
  return vi.fn().mockImplementation(() => {
    const next = responses[Math.min(i, responses.length - 1)]
    i++
    return Promise.resolve(next)
  })
}

describe('ManageConnectionsModal', () => {
  let originalFetch
  beforeEach(() => {
    originalFetch = global.fetch
  })
  afterEach(() => {
    global.fetch = originalFetch
  })

  it('renders one row per platform with the correct state', async () => {
    global.fetch = mockFetch([
      { ok: true, status: 200, json: async () => statusBody },
    ])
    render(<ManageConnectionsModal apiBaseUrl="http://api.test" userEmail="u@example.com" onClose={() => {}} />)

    await waitFor(() => expect(screen.getByTestId('connection-row-zoom')).toBeTruthy())
    expect(screen.getByTestId('connection-state-zoom').textContent).toContain('Connected')
    expect(screen.getByTestId('connection-state-zoom').textContent).toContain('z@example.com')

    expect(screen.getByTestId('connection-state-google_meet').textContent).toBe('Not connected')

    expect(screen.getByTestId('connection-row-teams').getAttribute('aria-disabled')).toBe('true')
    expect(screen.getByTestId('connection-state-teams').textContent).toBe('Not available')
  })

  it('opens the confirm dialog when Disconnect is clicked and cancel closes it without calling DELETE', async () => {
    global.fetch = mockFetch([
      { ok: true, status: 200, json: async () => statusBody },
    ])
    const user = userEvent.setup()
    render(<ManageConnectionsModal apiBaseUrl="http://api.test" userEmail="u@example.com" onClose={() => {}} />)

    await waitFor(() => expect(screen.getByTestId('disconnect-zoom')).toBeTruthy())
    await user.click(screen.getByTestId('disconnect-zoom'))
    expect(screen.getByTestId('disconnect-confirm')).toBeTruthy()

    await user.click(screen.getByTestId('disconnect-cancel-button'))
    expect(screen.queryByTestId('disconnect-confirm')).toBeNull()
    expect(global.fetch).toHaveBeenCalledTimes(1)
  })

  it('confirm posts to /api/zoom/disconnect and refreshes status', async () => {
    const after = {
      ...statusBody,
      zoom: { enabled: true, connected: false, account_email: null },
    }
    global.fetch = mockFetch([
      { ok: true, status: 200, json: async () => statusBody },
      { ok: true, status: 204, json: async () => ({}) },
      { ok: true, status: 200, json: async () => after },
    ])
    const user = userEvent.setup()
    render(<ManageConnectionsModal apiBaseUrl="http://api.test" userEmail="u@example.com" onClose={() => {}} />)

    await waitFor(() => expect(screen.getByTestId('disconnect-zoom')).toBeTruthy())
    await user.click(screen.getByTestId('disconnect-zoom'))
    await user.click(screen.getByTestId('disconnect-confirm-button'))

    await waitFor(() => expect(screen.getByTestId('connection-state-zoom').textContent).toBe('Not connected'))

    const calls = global.fetch.mock.calls
    expect(calls.length).toBe(3)
    expect(calls[1][0]).toBe('http://api.test/api/zoom/disconnect')
    expect(calls[1][1].method).toBe('POST')
    expect(calls[1][1].headers['X-Creator-Identity']).toBe('u@example.com')
  })

  it('surfaces a backend error from disconnect', async () => {
    global.fetch = mockFetch([
      { ok: true, status: 200, json: async () => statusBody },
      { ok: false, status: 500, json: async () => ({}) },
    ])
    const user = userEvent.setup()
    render(<ManageConnectionsModal apiBaseUrl="http://api.test" userEmail="u@example.com" onClose={() => {}} />)

    await waitFor(() => expect(screen.getByTestId('disconnect-zoom')).toBeTruthy())
    await user.click(screen.getByTestId('disconnect-zoom'))
    await user.click(screen.getByTestId('disconnect-confirm-button'))

    await waitFor(() => expect(screen.queryByTestId('disconnect-error')).not.toBeNull())
  })
})
