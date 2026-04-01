import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, waitFor } from '@testing-library/react'
import { AdminUsers } from '../components/AdminUsers'

afterEach(() => {
  vi.restoreAllMocks()
})

describe('AdminUsers', () => {
  it('fetches sessions when expanded even if apiBaseUrl is empty string', async () => {
    const fetchMock = vi.fn(async (url) => {
      if (String(url).includes('/api/admin/users')) {
        return { ok: true, json: async () => ({ users: [] }) }
      }
      if (String(url).includes('/api/sessions')) {
        return { ok: true, json: async () => [] }
      }
      return { ok: true, json: async () => ({}) }
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <AdminUsers
        apiBaseUrl=""
        sessionsExpanded={true}
        onSessionsExpandedChange={() => {}}
      />,
    )

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith('/api/sessions', { credentials: 'include' })
    })
  })
})
