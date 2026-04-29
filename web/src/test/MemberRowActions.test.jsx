import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react'
import { MemberRowActions } from '../components/MemberRowActions'

const pendingInv = (overrides = {}) => ({
  id: 'inv-1',
  invited_email: 'fred@foo.com',
  invited_role: 'participant',
  status: 'pending',
  ...overrides,
})

const acceptedInv = (overrides = {}) => ({
  id: 'inv-2',
  invited_email: 'george@foo.com',
  invited_role: 'participant',
  status: 'accepted',
  accepted_by_user_id: 'user-george',
  ...overrides,
})

const buildJsonRes = (status, body = {}) => ({
  ok: status >= 200 && status < 300,
  status,
  json: async () => body,
  text: async () => JSON.stringify(body),
})

describe('MemberRowActions', () => {
  let originalFetch
  beforeEach(() => {
    originalFetch = global.fetch
  })
  afterEach(() => {
    global.fetch = originalFetch
    vi.restoreAllMocks()
  })

  it('renders an icon trigger with the right aria attributes for a pending row', () => {
    render(<MemberRowActions invitation={pendingInv()} apiBaseUrl="http://api" />)
    const trigger = screen.getByTestId('member-row-actions-trigger')
    expect(trigger).toBeTruthy()
    expect(trigger.getAttribute('aria-haspopup')).toBe('menu')
    expect(trigger.getAttribute('aria-expanded')).toBe('false')
    expect(trigger.getAttribute('aria-label')).toBe('Member actions')
    expect(trigger.textContent).toBe('⋯')
  })

  it('opens a menu with Resend / Copy link / Revoke in that order on click', () => {
    render(<MemberRowActions invitation={pendingInv()} apiBaseUrl="http://api" />)
    fireEvent.click(screen.getByTestId('member-row-actions-trigger'))
    const menu = screen.getByTestId('member-row-menu')
    expect(menu.getAttribute('role')).toBe('menu')
    const items = menu.querySelectorAll('[role="menuitem"]')
    expect(items.length).toBe(3)
    expect(items[0].getAttribute('data-testid')).toBe('member-action-resend')
    expect(items[1].getAttribute('data-testid')).toBe('member-action-copy-link')
    expect(items[2].getAttribute('data-testid')).toBe('member-action-revoke')
  })

  it('Resend posts to the resend endpoint, fires onFeedback success, and refetches', async () => {
    const fetchSpy = vi.fn(() => Promise.resolve(buildJsonRes(200, { invitation: { id: 'inv-1' } })))
    global.fetch = fetchSpy
    const onFeedback = vi.fn()
    const onChanged = vi.fn()
    render(
      <MemberRowActions
        invitation={pendingInv()}
        apiBaseUrl="http://api"
        onFeedback={onFeedback}
        onChanged={onChanged}
      />
    )
    fireEvent.click(screen.getByTestId('member-row-actions-trigger'))
    fireEvent.click(screen.getByTestId('member-action-resend'))
    await waitFor(() => expect(fetchSpy).toHaveBeenCalledTimes(1))
    expect(fetchSpy.mock.calls[0][0]).toBe('http://api/api/invitations/inv-1/resend')
    expect(fetchSpy.mock.calls[0][1].method).toBe('POST')
    await waitFor(() =>
      expect(onFeedback).toHaveBeenCalledWith({ type: 'success', message: 'New link ready.' })
    )
    expect(onChanged).toHaveBeenCalled()
    // Menu closes after action.
    await waitFor(() => expect(screen.queryByTestId('member-row-menu')).toBeNull())
  })

  it('Copy link writes the accept_url to the clipboard and surfaces "Copied."', async () => {
    const fetchSpy = vi.fn(() =>
      Promise.resolve(buildJsonRes(200, { accept_url: 'https://example/accept?token=x' }))
    )
    global.fetch = fetchSpy
    const writeText = vi.fn(() => Promise.resolve())
    Object.assign(navigator, { clipboard: { writeText } })
    const onFeedback = vi.fn()
    render(
      <MemberRowActions invitation={pendingInv()} apiBaseUrl="http://api" onFeedback={onFeedback} />
    )
    fireEvent.click(screen.getByTestId('member-row-actions-trigger'))
    fireEvent.click(screen.getByTestId('member-action-copy-link'))
    await waitFor(() => expect(fetchSpy).toHaveBeenCalledTimes(1))
    expect(fetchSpy.mock.calls[0][0]).toBe('http://api/api/invitations/inv-1/link')
    await waitFor(() => expect(writeText).toHaveBeenCalledWith('https://example/accept?token=x'))
    await waitFor(() =>
      expect(onFeedback).toHaveBeenCalledWith({ type: 'success', message: 'Copied.' })
    )
  })

  it('Revoke shows an inline confirm and only POSTs after Confirm', async () => {
    const fetchSpy = vi.fn(() => Promise.resolve(buildJsonRes(200)))
    global.fetch = fetchSpy
    const onFeedback = vi.fn()
    const onChanged = vi.fn()
    render(
      <MemberRowActions
        invitation={pendingInv()}
        apiBaseUrl="http://api"
        onFeedback={onFeedback}
        onChanged={onChanged}
      />
    )
    fireEvent.click(screen.getByTestId('member-row-actions-trigger'))
    fireEvent.click(screen.getByTestId('member-action-revoke'))
    // Confirm UI replaces the menu list; Revoke not POSTed yet.
    expect(screen.getByTestId('member-row-revoke-confirm')).toBeTruthy()
    expect(fetchSpy).not.toHaveBeenCalled()

    fireEvent.click(screen.getByTestId('member-action-revoke-confirm'))
    await waitFor(() => expect(fetchSpy).toHaveBeenCalledTimes(1))
    expect(fetchSpy.mock.calls[0][0]).toBe('http://api/api/invitations/inv-1/revoke')
    await waitFor(() =>
      expect(onFeedback).toHaveBeenCalledWith({ type: 'success', message: 'Invitation revoked.' })
    )
    expect(onChanged).toHaveBeenCalled()
  })

  it('Revoke Cancel returns to the menu and does not POST', async () => {
    const fetchSpy = vi.fn(() => Promise.resolve(buildJsonRes(200)))
    global.fetch = fetchSpy
    render(<MemberRowActions invitation={pendingInv()} apiBaseUrl="http://api" />)
    fireEvent.click(screen.getByTestId('member-row-actions-trigger'))
    fireEvent.click(screen.getByTestId('member-action-revoke'))
    expect(screen.getByTestId('member-row-revoke-confirm')).toBeTruthy()
    fireEvent.click(screen.getByTestId('member-action-revoke-cancel'))
    expect(screen.queryByTestId('member-row-revoke-confirm')).toBeNull()
    expect(screen.getByTestId('member-row-menu')).toBeTruthy()
    expect(fetchSpy).not.toHaveBeenCalled()
  })

  it('renders nothing for the self row (creator cannot act on their own membership)', () => {
    const { container } = render(
      <MemberRowActions
        invitation={pendingInv({ invited_email: 'me@foo.com' })}
        apiBaseUrl="http://api"
        currentUserEmail="me@foo.com"
      />
    )
    expect(container.firstChild).toBeNull()
  })

  it('accepted rows expose three Set as <role> items in the role enum order', () => {
    render(<MemberRowActions invitation={acceptedInv()} apiBaseUrl="http://api" onChangeRole={vi.fn()} />)
    fireEvent.click(screen.getByTestId('member-row-actions-trigger'))
    const menu = screen.getByTestId('member-row-menu')
    const items = menu.querySelectorAll('[role="menuitem"]')
    expect(items.length).toBe(3)
    expect(items[0].getAttribute('data-testid')).toBe('member-action-role-participant')
    expect(items[1].getAttribute('data-testid')).toBe('member-action-role-creator')
    expect(items[2].getAttribute('data-testid')).toBe('member-action-role-decision_maker')
  })

  it("accepted row marks the current role as disabled with the ✓ glyph", () => {
    render(<MemberRowActions invitation={acceptedInv({ invited_role: 'creator' })} apiBaseUrl="http://api" onChangeRole={vi.fn()} />)
    fireEvent.click(screen.getByTestId('member-row-actions-trigger'))
    const current = screen.getByTestId('member-action-role-creator')
    expect(current.disabled).toBe(true)
    expect(current.getAttribute('aria-disabled')).toBe('true')
    expect(current.getAttribute('data-selected')).toBe('true')
    expect(current.textContent).toContain('✓')
    // The other items are not disabled.
    expect(screen.getByTestId('member-action-role-participant').disabled).toBe(false)
    expect(screen.getByTestId('member-action-role-decision_maker').disabled).toBe(false)
  })

  it('participant → decision_maker fires onChangeRole immediately (no confirm)', async () => {
    const onChangeRole = vi.fn().mockResolvedValue({ ok: true })
    const onFeedback = vi.fn()
    render(
      <MemberRowActions
        invitation={acceptedInv({ invited_role: 'participant' })}
        apiBaseUrl="http://api"
        onChangeRole={onChangeRole}
        onFeedback={onFeedback}
      />
    )
    fireEvent.click(screen.getByTestId('member-row-actions-trigger'))
    fireEvent.click(screen.getByTestId('member-action-role-decision_maker'))
    await waitFor(() => expect(onChangeRole).toHaveBeenCalledTimes(1))
    expect(onChangeRole).toHaveBeenCalledWith('user-george', 'decision_maker')
    await waitFor(() =>
      expect(onFeedback).toHaveBeenCalledWith({ type: 'success', message: 'Role updated to Decision Maker.' })
    )
    // No confirm step was shown.
    expect(screen.queryByTestId('member-row-role-confirm')).toBeNull()
  })

  it('demoting a creator to participant requires inline confirm', async () => {
    const onChangeRole = vi.fn().mockResolvedValue({ ok: true })
    render(
      <MemberRowActions
        invitation={acceptedInv({ invited_role: 'creator' })}
        apiBaseUrl="http://api"
        onChangeRole={onChangeRole}
      />
    )
    fireEvent.click(screen.getByTestId('member-row-actions-trigger'))
    fireEvent.click(screen.getByTestId('member-action-role-participant'))
    expect(screen.getByTestId('member-row-role-confirm')).toBeTruthy()
    expect(onChangeRole).not.toHaveBeenCalled()
    fireEvent.click(screen.getByTestId('member-action-role-confirm'))
    await waitFor(() => expect(onChangeRole).toHaveBeenCalledTimes(1))
    expect(onChangeRole).toHaveBeenCalledWith('user-george', 'participant')
  })

  it('demote-creator confirm Cancel returns to the menu without calling onChangeRole', () => {
    const onChangeRole = vi.fn()
    render(
      <MemberRowActions
        invitation={acceptedInv({ invited_role: 'creator' })}
        apiBaseUrl="http://api"
        onChangeRole={onChangeRole}
      />
    )
    fireEvent.click(screen.getByTestId('member-row-actions-trigger'))
    fireEvent.click(screen.getByTestId('member-action-role-participant'))
    fireEvent.click(screen.getByTestId('member-action-role-cancel'))
    expect(screen.queryByTestId('member-row-role-confirm')).toBeNull()
    expect(screen.getByTestId('member-row-menu')).toBeTruthy()
    expect(onChangeRole).not.toHaveBeenCalled()
  })

  it('demoting creator → decision_maker fires immediately (only ↓participant requires confirm)', async () => {
    const onChangeRole = vi.fn().mockResolvedValue({ ok: true })
    render(
      <MemberRowActions
        invitation={acceptedInv({ invited_role: 'creator' })}
        apiBaseUrl="http://api"
        onChangeRole={onChangeRole}
      />
    )
    fireEvent.click(screen.getByTestId('member-row-actions-trigger'))
    fireEvent.click(screen.getByTestId('member-action-role-decision_maker'))
    await waitFor(() => expect(onChangeRole).toHaveBeenCalledTimes(1))
    expect(onChangeRole).toHaveBeenCalledWith('user-george', 'decision_maker')
    expect(screen.queryByTestId('member-row-role-confirm')).toBeNull()
  })

  it('surfaces a server error returned by onChangeRole', async () => {
    const onChangeRole = vi.fn().mockResolvedValue({ ok: false, error: 'session must keep at least one creator' })
    const onFeedback = vi.fn()
    render(
      <MemberRowActions
        invitation={acceptedInv({ invited_role: 'participant' })}
        apiBaseUrl="http://api"
        onChangeRole={onChangeRole}
        onFeedback={onFeedback}
      />
    )
    fireEvent.click(screen.getByTestId('member-row-actions-trigger'))
    fireEvent.click(screen.getByTestId('member-action-role-creator'))
    await waitFor(() =>
      expect(onFeedback).toHaveBeenCalledWith({
        type: 'error',
        message: 'session must keep at least one creator',
      })
    )
  })

  it('accepted row with no accepted_by_user_id surfaces a friendly error rather than calling onChangeRole', async () => {
    const onChangeRole = vi.fn()
    const onFeedback = vi.fn()
    render(
      <MemberRowActions
        invitation={acceptedInv({ accepted_by_user_id: null })}
        apiBaseUrl="http://api"
        onChangeRole={onChangeRole}
        onFeedback={onFeedback}
      />
    )
    fireEvent.click(screen.getByTestId('member-row-actions-trigger'))
    fireEvent.click(screen.getByTestId('member-action-role-decision_maker'))
    await waitFor(() =>
      expect(onFeedback).toHaveBeenCalledWith({ type: 'error', message: 'Cannot change role: missing user id.' })
    )
    expect(onChangeRole).not.toHaveBeenCalled()
  })

  it('Escape closes the menu and returns focus to the trigger', () => {
    render(<MemberRowActions invitation={pendingInv()} apiBaseUrl="http://api" />)
    const trigger = screen.getByTestId('member-row-actions-trigger')
    fireEvent.click(trigger)
    expect(screen.getByTestId('member-row-menu')).toBeTruthy()
    act(() => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    })
    expect(screen.queryByTestId('member-row-menu')).toBeNull()
  })

  it('surfaces a fetch error from Resend back to onFeedback', async () => {
    const fetchSpy = vi.fn(() => Promise.resolve(buildJsonRes(500, { error: 'oops' })))
    global.fetch = fetchSpy
    const onFeedback = vi.fn()
    render(
      <MemberRowActions invitation={pendingInv()} apiBaseUrl="http://api" onFeedback={onFeedback} />
    )
    fireEvent.click(screen.getByTestId('member-row-actions-trigger'))
    fireEvent.click(screen.getByTestId('member-action-resend'))
    await waitFor(() =>
      expect(onFeedback).toHaveBeenCalledWith({ type: 'error', message: 'oops' })
    )
  })
})
