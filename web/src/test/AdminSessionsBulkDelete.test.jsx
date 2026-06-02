// SCRUM-583: bulk deletion of sessions from the Admin → Sessions table.
// Mirrors the Users bulk-delete coverage (SCRUM-582): selection mechanics
// (row + select-all + indeterminate), the bulk action bar, the confirmation
// dialog (count, titles, open-session callout), the happy-path bulk delete
// (per-session DELETE + progress + success toast), and partial-failure handling
// (failed rows stay selected with a retry affordance).

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AdminUsers } from '../components/AdminUsers'

const SESSIONS = [
  { id: 's1', title: 'Alpha', created_by: 'a@x.com', status: 'open', updated_at: '2026-05-25T20:00:00Z' },
  { id: 's2', title: 'Beta', created_by: 'a@x.com', status: 'closed', updated_at: '2026-05-25T19:00:00Z' },
  { id: 's3', title: 'Gamma', created_by: 'b@x.com', status: 'open', updated_at: '2026-05-25T18:00:00Z' },
]

function installFetch({ failIds = new Set() } = {}) {
  const deleted = []
  const fetchMock = vi.fn(async (url, opts = {}) => {
    const u = String(url)
    if (u.includes('/api/admin/users') && (!opts.method || opts.method === 'GET')) {
      return { ok: true, json: async () => ({ users: [] }) }
    }
    if (u.includes('/api/sessions/') && opts.method === 'DELETE') {
      const id = u.split('/api/sessions/')[1]
      if (failIds.has(id)) {
        return { ok: false, status: 500, json: async () => ({ error: 'boom' }) }
      }
      deleted.push(id)
      return { ok: true, json: async () => ({}) }
    }
    if (u.includes('/api/sessions')) {
      return { ok: true, json: async () => SESSIONS }
    }
    return { ok: true, json: async () => ({}) }
  })
  vi.stubGlobal('fetch', fetchMock)
  return { fetchMock, deleted }
}

function renderAdmin() {
  return render(
    <AdminUsers apiBaseUrl="" sessionsExpanded={true} onSessionsExpandedChange={() => {}} />,
  )
}

beforeEach(() => {
  installFetch()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('AdminUsers bulk session delete (SCRUM-583)', () => {
  it('renders a checkbox per session row and a select-all', async () => {
    renderAdmin()
    expect(await screen.findByLabelText('Select Alpha')).toBeEnabled()
    expect(screen.getByLabelText('Select Beta')).toBeInTheDocument()
    expect(screen.getByLabelText('Select all sessions')).toBeInTheDocument()
  })

  it('select-all selects every session and shows the bulk action bar', async () => {
    const user = userEvent.setup()
    renderAdmin()
    const selectAll = await screen.findByLabelText('Select all sessions')
    await user.click(selectAll)
    expect(screen.getByText('3 sessions selected')).toBeInTheDocument()
    expect(selectAll).toBeChecked()
    expect(screen.getByRole('button', { name: /Delete selected/ })).toBeInTheDocument()
  })

  it('shows the indeterminate state when only some rows are selected', async () => {
    const user = userEvent.setup()
    renderAdmin()
    await user.click(await screen.findByLabelText('Select Alpha'))
    const selectAll = screen.getByLabelText('Select all sessions')
    expect(selectAll.indeterminate).toBe(true)
    expect(selectAll).not.toBeChecked()
    expect(screen.getByText('1 session selected')).toBeInTheDocument()
  })

  it('confirms, deletes selected sessions, removes rows and shows a success toast', async () => {
    const user = userEvent.setup()
    const { deleted } = installFetch()
    renderAdmin()

    await user.click(await screen.findByLabelText('Select Alpha'))
    await user.click(screen.getByLabelText('Select Beta'))
    await user.click(screen.getByRole('button', { name: /Delete selected/ }))

    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('Delete 2 sessions?')).toBeInTheDocument()
    expect(within(dialog).getByText('Alpha')).toBeInTheDocument()
    // Alpha is open → open-session callout.
    expect(within(dialog).getByText(/1 of these session is open/)).toBeInTheDocument()

    await user.click(within(dialog).getByRole('button', { name: /Delete sessions/ }))

    const toast = await screen.findByRole('status')
    expect(toast).toHaveTextContent('2 sessions deleted.')
    expect(deleted.sort()).toEqual(['s1', 's2'])
    expect(screen.queryByLabelText('Select Alpha')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Select Beta')).not.toBeInTheDocument()
    expect(screen.queryByText(/sessions selected/)).not.toBeInTheDocument()
  })

  it('keeps failed rows selected and offers a retry on partial failure', async () => {
    const user = userEvent.setup()
    installFetch({ failIds: new Set(['s2']) })
    renderAdmin()

    await user.click(await screen.findByLabelText('Select Alpha'))
    await user.click(screen.getByLabelText('Select Beta'))
    await user.click(screen.getByRole('button', { name: /Delete selected/ }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: /Delete sessions/ }))

    await within(dialog).findByText(/1 of 2 sessions deleted, 1 could not be removed/)
    expect(within(dialog).getByRole('button', { name: /Retry failed/ })).toBeInTheDocument()

    expect(screen.queryByLabelText('Select Alpha')).not.toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: /Close/ }))
    expect(screen.getByLabelText('Select Beta')).toBeChecked()
  })
})
