import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MaterialsPanelHeader } from '../components/MaterialsTreePanel'

describe('MaterialsPanelHeader', () => {
  const noop = vi.fn()

  it('shows Session label when expanded', () => {
    render(<MaterialsPanelHeader collapsed={false} onCollapsedChange={noop} />)
    expect(screen.getByText('Session')).toBeTruthy()
  })

  it('does not show badge when unreadCount is 0 and expanded', () => {
    render(<MaterialsPanelHeader collapsed={false} onCollapsedChange={noop} unreadCount={0} />)
    expect(screen.queryByTitle(/new material/i)).toBeNull()
  })

  it('shows pluralized "N new materials" badge when expanded and unread > 1', () => {
    render(<MaterialsPanelHeader collapsed={false} onCollapsedChange={noop} unreadCount={3} />)
    expect(screen.getByText('3 new materials')).toBeTruthy()
  })

  it('shows singular "1 new material" badge when expanded and unread is exactly 1', () => {
    render(<MaterialsPanelHeader collapsed={false} onCollapsedChange={noop} unreadCount={1} />)
    expect(screen.getByText('1 new material')).toBeTruthy()
  })

  it('shows compact count badge when collapsed and unread > 0', () => {
    render(<MaterialsPanelHeader collapsed={true} onCollapsedChange={noop} unreadCount={2} />)
    const badge = screen.getByTitle('2 new materials')
    expect(badge).toBeTruthy()
    expect(badge.textContent).toBe('2')
  })

  it('shows singular tooltip for unreadCount=1 when collapsed', () => {
    render(<MaterialsPanelHeader collapsed={true} onCollapsedChange={noop} unreadCount={1} />)
    expect(screen.getByTitle('1 new material')).toBeTruthy()
  })

  it('shows no badge when collapsed and unreadCount is 0', () => {
    render(<MaterialsPanelHeader collapsed={true} onCollapsedChange={noop} unreadCount={0} />)
    expect(screen.queryByTitle(/new material/i)).toBeNull()
  })

  it('hides Session label text when collapsed without itemCount', () => {
    render(<MaterialsPanelHeader collapsed={true} onCollapsedChange={noop} />)
    expect(screen.queryByText('Session')).toBeNull()
  })

  it('renders "Session" rail label when collapsed and itemCount provided (no numeric badge)', () => {
    render(<MaterialsPanelHeader collapsed={true} onCollapsedChange={noop} itemCount={4} />)
    const label = screen.getByTestId('session-collapsed-label')
    expect(label.textContent).toBe('Session')
  })

  it('renders "Session" rail label even when itemCount is zero', () => {
    render(<MaterialsPanelHeader collapsed={true} onCollapsedChange={noop} itemCount={0} />)
    expect(screen.getByTestId('session-collapsed-label').textContent).toBe('Session')
  })

  it('falls back to chevron rail when itemCount is not provided', () => {
    render(<MaterialsPanelHeader collapsed={true} onCollapsedChange={noop} />)
    expect(screen.queryByTestId('session-collapsed-label')).toBeNull()
    // Chevron remains the only visible affordance
    expect(screen.getByRole('button').textContent).toContain('▷')
  })

  it('collapsed aria-label is "Expand session panel" with no item-count phrasing', () => {
    const { rerender } = render(<MaterialsPanelHeader collapsed={true} onCollapsedChange={noop} itemCount={1} />)
    expect(screen.getByRole('button').getAttribute('aria-label')).toBe('Expand session panel')
    rerender(<MaterialsPanelHeader collapsed={true} onCollapsedChange={noop} itemCount={3} />)
    expect(screen.getByRole('button').getAttribute('aria-label')).toBe('Expand session panel')
    rerender(<MaterialsPanelHeader collapsed={true} onCollapsedChange={noop} />)
    expect(screen.getByRole('button').getAttribute('aria-label')).toBe('Expand session panel')
  })

  it('expanded aria-label is "Collapse session panel"', () => {
    render(<MaterialsPanelHeader collapsed={false} onCollapsedChange={noop} itemCount={5} />)
    expect(screen.getByRole('button').getAttribute('aria-label')).toBe('Collapse session panel')
  })

  it('expanded view is unchanged when itemCount is provided', () => {
    render(<MaterialsPanelHeader collapsed={false} onCollapsedChange={noop} itemCount={5} unreadCount={2} />)
    expect(screen.getByText('Session')).toBeTruthy()
    expect(screen.getByText('2 new materials')).toBeTruthy()
    expect(screen.queryByTestId('session-collapsed-label')).toBeNull()
  })

  it('clicking the collapsed session rail toggles expansion', () => {
    let collapsed = true
    const setCollapsed = vi.fn((next) => { collapsed = next })
    const { rerender } = render(
      <MaterialsPanelHeader collapsed={collapsed} onCollapsedChange={setCollapsed} itemCount={2} />
    )
    screen.getByRole('button').click()
    expect(setCollapsed).toHaveBeenCalledWith(false)
    rerender(<MaterialsPanelHeader collapsed={false} onCollapsedChange={setCollapsed} itemCount={2} />)
    // No collapsed-label rendered when expanded
    expect(screen.queryByTestId('session-collapsed-label')).toBeNull()
  })
})
