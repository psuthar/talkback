// SCRUM-582: bulk deletion of users from the Admin → Users table.
// Covers selection mechanics (row + select-all + indeterminate + last-admin
// guard), the bulk action bar, the confirmation dialog (count, names, active-
// session callout), the happy-path bulk delete (per-user DELETE + progress +
// success toast), and partial-failure handling (failed rows stay selected with
// a retry affordance).

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, within, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AdminUsers } from '../components/AdminUsers'

const USERS = [
  { id: 'admin1', email: 'admin@x.com', display_name: 'Admin', global_role: 'admin', status: 'active', session_ids: [] },
  { id: 'u1', email: 'u1@x.com', display_name: 'User One', global_role: 'creator', status: 'active', session_ids: ['s1'] },
  { id: 'u2', email: 'u2@x.com', display_name: 'User Two', global_role: 'participant', status: 'active', session_ids: [] },
  { id: 'u3', email: 'u3@x.com', display_name: 'User Three', global_role: 'participant', status: 'active', session_ids: [] },
]

// installs a fetch mock that serves the user list and records DELETE calls.
// `failIds` is a set of user ids whose DELETE should return a 500.
function installFetch({ failIds = new Set() } = {}) {
  const deleted = []
  const fetchMock = vi.fn(async (url, opts = {}) => {
    const u = String(url)
    if (u.includes('/api/admin/users') && (!opts.method || opts.method === 'GET')) {
      return { ok: true, json: async () => ({ users: USERS }) }
    }
    if (u.includes('/api/admin/users/') && opts.method === 'DELETE') {
      const id = u.split('/api/admin/users/')[1]
      if (failIds.has(id)) {
        return { ok: false, status: 500, json: async () => ({ error: 'boom' }) }
      }
      deleted.push(id)
      return { ok: true, json: async () => ({}) }
    }
    if (u.includes('/api/sessions')) return { ok: true, json: async () => [] }
    return { ok: true, json: async () => ({}) }
  })
  vi.stubGlobal('fetch', fetchMock)
  return { fetchMock, deleted }
}

function renderUsers() {
  return render(
    <AdminUsers apiBaseUrl="" usersExpanded={true} onUsersExpandedChange={() => {}} />,
  )
}

beforeEach(() => {
  installFetch()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('AdminUsers bulk delete (SCRUM-582)', () => {
  it('renders a checkbox per row and disables the last-admin checkbox', async () => {
    renderUsers()
    const adminCb = await screen.findByLabelText('Select admin@x.com')
    expect(adminCb).toBeDisabled()
    expect(screen.getByLabelText('Select u1@x.com')).toBeEnabled()
    expect(screen.getByLabelText('Select all users')).toBeInTheDocument()
  })

  it('select-all selects only selectable rows and shows the bulk action bar', async () => {
    const user = userEvent.setup()
    renderUsers()
    const selectAll = await screen.findByLabelText('Select all users')
    await user.click(selectAll)

    // admin (last admin) excluded → 3 selectable users selected.
    expect(screen.getByText('3 users selected')).toBeInTheDocument()
    expect(screen.getByLabelText('Select admin@x.com')).not.toBeChecked()
    expect(screen.getByLabelText('Select u1@x.com')).toBeChecked()
    expect(selectAll).toBeChecked()
    expect(screen.getByRole('button', { name: /Delete selected/ })).toBeInTheDocument()
  })

  it('shows the indeterminate state when only some rows are selected', async () => {
    const user = userEvent.setup()
    renderUsers()
    const row = await screen.findByLabelText('Select u1@x.com')
    await user.click(row)
    const selectAll = screen.getByLabelText('Select all users')
    expect(selectAll.indeterminate).toBe(true)
    expect(selectAll).not.toBeChecked()
    expect(screen.getByText('1 user selected')).toBeInTheDocument()
  })

  it('confirms, deletes selected users, removes rows and shows a success toast', async () => {
    const user = userEvent.setup()
    const { deleted } = installFetch()
    renderUsers()

    await user.click(await screen.findByLabelText('Select u1@x.com'))
    await user.click(screen.getByLabelText('Select u2@x.com'))
    await user.click(screen.getByRole('button', { name: /Delete selected/ }))

    // Confirmation dialog: count, names, active-session callout.
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('Delete 2 users?')).toBeInTheDocument()
    expect(within(dialog).getByText(/User One \(u1@x\.com\)/)).toBeInTheDocument()
    expect(within(dialog).getByText(/1 of these user has active sessions/)).toBeInTheDocument()

    await user.click(within(dialog).getByRole('button', { name: /Delete users/ }))

    const toast = await screen.findByRole('status')
    expect(toast).toHaveTextContent('2 users deleted.')
    expect(deleted.sort()).toEqual(['u1', 'u2'])
    // Deleted rows are gone from the table.
    expect(screen.queryByLabelText('Select u1@x.com')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Select u2@x.com')).not.toBeInTheDocument()
    // Bar gone (selection cleared).
    expect(screen.queryByText(/users selected/)).not.toBeInTheDocument()
  })

  it('keeps failed rows selected and offers a retry on partial failure', async () => {
    const user = userEvent.setup()
    installFetch({ failIds: new Set(['u2']) })
    renderUsers()

    await user.click(await screen.findByLabelText('Select u1@x.com'))
    await user.click(screen.getByLabelText('Select u2@x.com'))
    await user.click(screen.getByRole('button', { name: /Delete selected/ }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: /Delete users/ }))

    // Result view: 1 of 2 deleted, the failure listed, retry offered.
    await within(dialog).findByText(/1 of 2 users deleted, 1 could not be removed/)
    expect(within(dialog).getByText(/u2@x\.com/)).toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: /Retry failed/ })).toBeInTheDocument()

    // u1 succeeded and left the table; u2 failed and remains.
    expect(screen.queryByLabelText('Select u1@x.com')).not.toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: /Close/ }))
    expect(screen.getByLabelText('Select u2@x.com')).toBeChecked()
  })
})
