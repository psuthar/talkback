import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { ParticipantSessionMenu } from '../components/ParticipantSessionMenu'

const baseUser = {
  id: 'user-abc-123',
  email: 'jane@example.com',
  display_name: 'Jane Doe',
  global_role: 'participant',
}

function renderMenu(extra = {}) {
  return render(
    <ParticipantSessionMenu
      authUser={baseUser}
      onShowAllSessions={vi.fn()}
      onLogout={vi.fn()}
      debugMode={false}
      setDebugMode={vi.fn()}
      {...extra}
    />
  )
}

describe('ParticipantSessionMenu', () => {
  it('renders an accessible trigger labeled "Session menu"', () => {
    renderMenu()
    const trigger = screen.getByTestId('participant-session-menu-btn')
    expect(trigger.getAttribute('aria-label')).toBe('Session menu')
    expect(trigger.getAttribute('aria-haspopup')).toBe('menu')
    expect(trigger.getAttribute('aria-expanded')).toBe('false')
    // Visible label, not icon-only
    expect(trigger.textContent).toContain('Session menu')
  })

  it('does not render the menu by default', () => {
    renderMenu()
    expect(screen.queryByTestId('participant-session-menu')).toBeNull()
  })

  it('opens the menu on click and shows identity (name, email, id)', () => {
    renderMenu()
    fireEvent.click(screen.getByTestId('participant-session-menu-btn'))
    const menu = screen.getByTestId('participant-session-menu')
    expect(menu.textContent).toContain('Jane Doe')
    expect(menu.textContent).toContain('jane@example.com')
    expect(menu.textContent).toContain('user-abc-123')
    expect(screen.getByTestId('participant-session-menu-btn').getAttribute('aria-expanded')).toBe('true')
  })

  it('orders menu items: identity → sessions → debug → destructive logout', () => {
    renderMenu()
    fireEvent.click(screen.getByTestId('participant-session-menu-btn'))
    const labels = Array.from(screen.getByTestId('participant-session-menu').querySelectorAll('[role^="menuitem"]')).map((b) => b.textContent.trim())
    const showAllIdx = labels.findIndex((l) => l.startsWith('Show all sessions'))
    const debugIdx = labels.findIndex((l) => l.includes('Debug panel'))
    const logoutIdx = labels.findIndex((l) => l.includes('Log out'))
    expect(showAllIdx).toBeGreaterThanOrEqual(0)
    expect(debugIdx).toBeGreaterThan(showAllIdx)
    expect(logoutIdx).toBeGreaterThan(debugIdx)
  })

  it('clicking Show all sessions calls onShowAllSessions and closes the menu', () => {
    const onShowAllSessions = vi.fn()
    renderMenu({ onShowAllSessions })
    fireEvent.click(screen.getByTestId('participant-session-menu-btn'))
    fireEvent.click(screen.getByTestId('menu-show-all-sessions'))
    expect(onShowAllSessions).toHaveBeenCalled()
    expect(screen.queryByTestId('participant-session-menu')).toBeNull()
  })

  it('Debug toggle reflects state and calls setDebugMode with the inverse value', () => {
    const setDebugMode = vi.fn()
    renderMenu({ setDebugMode, debugMode: false })
    fireEvent.click(screen.getByTestId('participant-session-menu-btn'))
    const debugBtn = screen.getByTestId('menu-debug-toggle')
    expect(debugBtn.getAttribute('aria-checked')).toBe('false')
    fireEvent.click(debugBtn)
    expect(setDebugMode).toHaveBeenCalledWith(true)
  })

  it('Debug toggle shows checked state when debugMode is true', () => {
    renderMenu({ debugMode: true })
    fireEvent.click(screen.getByTestId('participant-session-menu-btn'))
    const debugBtn = screen.getByTestId('menu-debug-toggle')
    expect(debugBtn.getAttribute('aria-checked')).toBe('true')
    expect(debugBtn.textContent).toContain('☑')
  })

  it('Logout calls the handler and closes the menu', () => {
    const onLogout = vi.fn()
    renderMenu({ onLogout })
    fireEvent.click(screen.getByTestId('participant-session-menu-btn'))
    fireEvent.click(screen.getByTestId('menu-logout'))
    expect(onLogout).toHaveBeenCalled()
    expect(screen.queryByTestId('participant-session-menu')).toBeNull()
  })

  it('Escape closes the menu', () => {
    renderMenu()
    fireEvent.click(screen.getByTestId('participant-session-menu-btn'))
    expect(screen.getByTestId('participant-session-menu')).toBeTruthy()
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByTestId('participant-session-menu')).toBeNull()
  })

  it('outside mousedown closes the menu', () => {
    render(
      <div>
        <ParticipantSessionMenu authUser={baseUser} onLogout={vi.fn()} debugMode={false} setDebugMode={vi.fn()} />
        <div data-testid="outside" />
      </div>
    )
    fireEvent.click(screen.getByTestId('participant-session-menu-btn'))
    expect(screen.getByTestId('participant-session-menu')).toBeTruthy()
    fireEvent.mouseDown(screen.getByTestId('outside'))
    expect(screen.queryByTestId('participant-session-menu')).toBeNull()
  })

  it('renders an Admin link when global_role is admin (SCRUM-474: absolute /?mode=admin so the link works from session URLs)', () => {
    renderMenu({ authUser: { ...baseUser, global_role: 'admin' } })
    fireEvent.click(screen.getByTestId('participant-session-menu-btn'))
    const adminLink = screen.getByTestId('menu-admin-link')
    // Must be root-anchored: the relative form "?mode=admin" resolves against
    // the current pathname, so from /app/sessions/{id} it would navigate to
    // /app/sessions/{id}?mode=admin which the App router ignores.
    expect(adminLink.getAttribute('href')).toBe('/?mode=admin')
    // Identity row also shows the Admin badge
    expect(screen.getByTestId('participant-session-menu').textContent).toContain('Admin')
  })

  it('omits Show all sessions when no handler is provided', () => {
    renderMenu({ onShowAllSessions: undefined })
    fireEvent.click(screen.getByTestId('participant-session-menu-btn'))
    expect(screen.queryByTestId('menu-show-all-sessions')).toBeNull()
  })

  it('omits Debug toggle when no setter is provided', () => {
    renderMenu({ setDebugMode: undefined })
    fireEvent.click(screen.getByTestId('participant-session-menu-btn'))
    expect(screen.queryByTestId('menu-debug-toggle')).toBeNull()
  })

  it('omits Logout when no handler is provided', () => {
    renderMenu({ onLogout: undefined })
    fireEvent.click(screen.getByTestId('participant-session-menu-btn'))
    expect(screen.queryByTestId('menu-logout')).toBeNull()
  })

  it('renders View as Participant link when participantViewHref is provided', () => {
    renderMenu({ participantViewHref: '/app/sessions/abc?mode=view' })
    fireEvent.click(screen.getByTestId('participant-session-menu-btn'))
    const link = screen.getByTestId('menu-view-as-participant')
    expect(link.getAttribute('href')).toBe('/app/sessions/abc?mode=view')
    expect(link.textContent).toContain('View as Participant')
  })
})
