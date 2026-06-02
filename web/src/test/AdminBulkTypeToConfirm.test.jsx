// SCRUM-584: type-to-confirm for large bulk deletes (> BULK_PROGRESS_THRESHOLD).
// Above 10 selected items, the confirm dialog requires typing the exact count
// before the destructive button enables — for both the Users and Sessions
// bulk-delete flows. At or below 10, no extra step (covered by the per-feature
// suites); this file focuses on the large-selection gating + reset behaviour.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AdminUsers } from '../components/AdminUsers'

const manyUsers = Array.from({ length: 12 }, (_, i) => ({
  id: `u${i}`, email: `u${i}@x.com`, display_name: `User ${i}`,
  global_role: 'participant', status: 'active', session_ids: [],
}))
const manySessions = Array.from({ length: 12 }, (_, i) => ({
  id: `s${i}`, title: `Session ${i}`, created_by: 'a@x.com', status: 'closed', updated_at: '2026-05-25T20:00:00Z',
}))

function installFetch({ users = [], sessions = [] } = {}) {
  const fetchMock = vi.fn(async (url, opts = {}) => {
    const u = String(url)
    if (u.includes('/api/admin/users') && (!opts.method || opts.method === 'GET')) {
      return { ok: true, json: async () => ({ users }) }
    }
    if (u.includes('/api/admin/users/') && opts.method === 'DELETE') {
      return { ok: true, json: async () => ({}) }
    }
    if (u.includes('/api/sessions/') && opts.method === 'DELETE') {
      return { ok: true, json: async () => ({}) }
    }
    if (u.includes('/api/sessions')) return { ok: true, json: async () => sessions }
    return { ok: true, json: async () => ({}) }
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

afterEach(() => vi.restoreAllMocks())

describe('Type-to-confirm for large bulk deletes (SCRUM-584)', () => {
  it('gates the Users delete behind typing the exact count when > 10 selected', async () => {
    const user = userEvent.setup()
    installFetch({ users: manyUsers })
    render(<AdminUsers apiBaseUrl="" usersExpanded={true} onUsersExpandedChange={() => {}} />)

    await user.click(await screen.findByLabelText('Select all users'))
    await user.click(screen.getByRole('button', { name: /Delete selected/ }))

    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('Delete 12 users?')).toBeInTheDocument()
    const deleteBtn = within(dialog).getByRole('button', { name: /Delete users/ })
    expect(deleteBtn).toBeDisabled()

    const input = within(dialog).getByLabelText(/Type 12 to confirm/)
    await user.type(input, '11')
    expect(deleteBtn).toBeDisabled()
    await user.clear(input)
    await user.type(input, '12')
    expect(deleteBtn).toBeEnabled()
  })

  it('gates the Sessions delete behind typing the exact count when > 10 selected', async () => {
    const user = userEvent.setup()
    installFetch({ sessions: manySessions })
    render(<AdminUsers apiBaseUrl="" sessionsExpanded={true} onSessionsExpandedChange={() => {}} />)

    await user.click(await screen.findByLabelText('Select all sessions'))
    await user.click(screen.getByRole('button', { name: /Delete selected/ }))

    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('Delete 12 sessions?')).toBeInTheDocument()
    const deleteBtn = within(dialog).getByRole('button', { name: /Delete sessions/ })
    expect(deleteBtn).toBeDisabled()

    await user.type(within(dialog).getByLabelText(/Type 12 to confirm/), '12')
    expect(deleteBtn).toBeEnabled()
  })

  it('does not require type-to-confirm at or below the threshold', async () => {
    const user = userEvent.setup()
    installFetch({ users: manyUsers.slice(0, 5) })
    render(<AdminUsers apiBaseUrl="" usersExpanded={true} onUsersExpandedChange={() => {}} />)

    await user.click(await screen.findByLabelText('Select all users'))
    await user.click(screen.getByRole('button', { name: /Delete selected/ }))

    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).queryByLabelText(/to confirm/)).not.toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: /Delete users/ })).toBeEnabled()
  })

  it('resets the typed confirmation when the dialog is cancelled and reopened', async () => {
    const user = userEvent.setup()
    installFetch({ users: manyUsers })
    render(<AdminUsers apiBaseUrl="" usersExpanded={true} onUsersExpandedChange={() => {}} />)

    await user.click(await screen.findByLabelText('Select all users'))
    await user.click(screen.getByRole('button', { name: /Delete selected/ }))
    let dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByLabelText(/Type 12 to confirm/), '12')
    await user.click(within(dialog).getByRole('button', { name: /Cancel/ }))

    // Reopen — input must be empty and the button disabled again.
    await user.click(screen.getByRole('button', { name: /Delete selected/ }))
    dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByLabelText(/Type 12 to confirm/)).toHaveValue('')
    expect(within(dialog).getByRole('button', { name: /Delete users/ })).toBeDisabled()
  })
})
