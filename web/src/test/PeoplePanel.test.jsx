import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { PeoplePanel } from '../components/PeoplePanel'

const baseBody = {
  labels: [
    { source_label: 'Speaker 0', segment_count: 6 },
    { source_label: 'Speaker 1', segment_count: 4 },
    { source_label: 'alice@example.com', segment_count: 2 },
  ],
  aliases: [],
}

const mockFetch = (responses) => {
  let i = 0
  return vi.fn().mockImplementation(() => {
    const next = responses[Math.min(i, responses.length - 1)]
    i++
    return Promise.resolve(next)
  })
}

describe('PeoplePanel', () => {
  let originalFetch
  beforeEach(() => { originalFetch = global.fetch })
  afterEach(() => { global.fetch = originalFetch })

  it('renders unmapped labels as Person N placeholders with airtime %', async () => {
    global.fetch = mockFetch([{ ok: true, status: 200, json: async () => baseBody }])
    render(<PeoplePanel sessionId="s1" apiBaseUrl="http://api.test" userEmail="u@example.com" />)
    await waitFor(() => expect(screen.getByTestId('people-panel-groups')).toBeTruthy())
    // 3 unmapped labels → 3 Person N groups.
    expect(screen.getAllByText(/^Person \d+$/).length).toBe(3)
  })

  it('groups already-aliased labels under one canonical display name', async () => {
    const canonical = 'aaaaaaaa-0000-0000-0000-000000000001'
    const body = {
      labels: baseBody.labels,
      aliases: [
        { id: 'a1', session_id: 's1', canonical_person_id: canonical, source_label: 'Speaker 0', canonical_display_name: 'Alice' },
        { id: 'a2', session_id: 's1', canonical_person_id: canonical, source_label: 'alice@example.com', canonical_display_name: 'Alice', canonical_email: 'alice@example.com' },
      ],
    }
    global.fetch = mockFetch([{ ok: true, status: 200, json: async () => body }])
    render(<PeoplePanel sessionId="s1" apiBaseUrl="http://api.test" userEmail="u@example.com" />)
    await waitFor(() => expect(screen.getByTestId(`people-group-${canonical}`)).toBeTruthy())
    expect(screen.getByTestId(`people-group-name-${canonical}`).textContent).toBe('Alice')
    expect(screen.getByTestId(`people-group-email-${canonical}`).textContent).toBe('alice@example.com')
    // 2 labels in the merged group; 1 standalone (Speaker 1).
    const labelsList = screen.getByTestId(`people-group-labels-${canonical}`)
    expect(labelsList.querySelectorAll('li').length).toBe(2)
  })

  it('Merge happy path: pick two labels → name + confirm → POSTs both with the same canonical', async () => {
    global.fetch = mockFetch([
      { ok: true, status: 200, json: async () => baseBody },
      { ok: true, status: 200, json: async () => ({ canonical_person_id: 'new-canonical' }) },
      { ok: true, status: 200, json: async () => ({ canonical_person_id: 'new-canonical' }) },
      { ok: true, status: 200, json: async () => ({ labels: baseBody.labels, aliases: [] }) }, // refresh
    ])
    const user = userEvent.setup()
    render(<PeoplePanel sessionId="s1" apiBaseUrl="http://api.test" userEmail="u@example.com" />)
    await waitFor(() => expect(screen.getByTestId('people-panel-groups')).toBeTruthy())

    await user.click(screen.getByTestId('people-label-checkbox-Speaker 0'))
    await user.click(screen.getByTestId('people-label-checkbox-alice@example.com'))
    expect(screen.getByTestId('people-panel-merge').textContent).toContain('(2)')

    await user.click(screen.getByTestId('people-panel-merge'))
    expect(screen.getByTestId('people-merge-confirm')).toBeTruthy()
    await user.type(screen.getByTestId('people-merge-display-name'), 'Alice')
    await user.type(screen.getByTestId('people-merge-email'), 'alice@example.com')
    await user.click(screen.getByTestId('people-merge-submit'))

    await waitFor(() => expect(screen.queryByTestId('people-merge-confirm')).toBeNull())

    const calls = global.fetch.mock.calls
    // GET initial + 2 POSTs + refresh GET = 4.
    expect(calls.length).toBe(4)
    expect(calls[1][1].method).toBe('POST')
    const firstPostBody = JSON.parse(calls[1][1].body)
    const secondPostBody = JSON.parse(calls[2][1].body)
    expect(firstPostBody.canonical_display_name).toBe('Alice')
    expect(firstPostBody.canonical_email).toBe('alice@example.com')
    // First POST has no canonical_person_id; second has the one minted by the first.
    expect(firstPostBody.canonical_person_id).toBeUndefined()
    expect(secondPostBody.canonical_person_id).toBe('new-canonical')
  })

  it('Merge button is disabled until 2+ labels selected', async () => {
    global.fetch = mockFetch([{ ok: true, status: 200, json: async () => baseBody }])
    const user = userEvent.setup()
    render(<PeoplePanel sessionId="s1" apiBaseUrl="http://api.test" userEmail="u@example.com" />)
    await waitFor(() => expect(screen.getByTestId('people-panel-groups')).toBeTruthy())

    expect(screen.getByTestId('people-panel-merge').disabled).toBe(true)
    await user.click(screen.getByTestId('people-label-checkbox-Speaker 0'))
    expect(screen.getByTestId('people-panel-merge').disabled).toBe(true)
    await user.click(screen.getByTestId('people-label-checkbox-Speaker 1'))
    expect(screen.getByTestId('people-panel-merge').disabled).toBe(false)
  })

  it('Unmap: DELETE each alias in the group, then refresh', async () => {
    const canonical = 'aaaaaaaa-0000-0000-0000-000000000001'
    const body = {
      labels: baseBody.labels,
      aliases: [
        { id: 'a1', session_id: 's1', canonical_person_id: canonical, source_label: 'Speaker 0', canonical_display_name: 'Alice' },
      ],
    }
    global.fetch = mockFetch([
      { ok: true, status: 200, json: async () => body },
      { ok: true, status: 204, json: async () => ({}) }, // DELETE
      { ok: true, status: 200, json: async () => ({ labels: baseBody.labels, aliases: [] }) },
    ])
    const user = userEvent.setup()
    render(<PeoplePanel sessionId="s1" apiBaseUrl="http://api.test" userEmail="u@example.com" />)
    await waitFor(() => expect(screen.getByTestId(`people-group-unmap-${canonical}`)).toBeTruthy())
    await user.click(screen.getByTestId(`people-group-unmap-${canonical}`))
    await waitFor(() => expect(global.fetch).toHaveBeenCalledTimes(3))
    expect(global.fetch.mock.calls[1][1].method).toBe('DELETE')
    expect(global.fetch.mock.calls[1][0]).toBe('http://api.test/api/sessions/s1/people/aliases/a1')
  })

  it('Error state surfaced on non-ok response', async () => {
    global.fetch = mockFetch([{ ok: false, status: 500, json: async () => ({ message: 'boom' }) }])
    render(<PeoplePanel sessionId="s1" apiBaseUrl="http://api.test" userEmail="u@example.com" />)
    await waitFor(() => expect(screen.getByTestId('people-panel-error')).toBeTruthy())
  })
})
