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
  invited_role: 'creator',
  status: 'accepted',
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

  it('renders nothing for accepted rows (SCRUM-213 introduces the accepted menu)', () => {
    const { container } = render(<MemberRowActions invitation={acceptedInv()} apiBaseUrl="http://api" />)
    expect(container.firstChild).toBeNull()
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
