import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MaterialsPanelHeader } from '../components/MaterialsTreePanel'

describe('MaterialsPanelHeader', () => {
  const noop = vi.fn()

  it('shows Materials label when expanded', () => {
    render(<MaterialsPanelHeader collapsed={false} onCollapsedChange={noop} />)
    expect(screen.getByText('Materials')).toBeTruthy()
  })

  it('does not show badge when unreadCount is 0 and expanded', () => {
    render(<MaterialsPanelHeader collapsed={false} onCollapsedChange={noop} unreadCount={0} />)
    expect(screen.queryByTitle(/new material/i)).toBeNull()
  })

  it('shows New badge with count when expanded and unread > 0', () => {
    render(<MaterialsPanelHeader collapsed={false} onCollapsedChange={noop} unreadCount={3} />)
    expect(screen.getByText('New 3')).toBeTruthy()
  })

  it('shows compact count badge when collapsed and unread > 0', () => {
    render(<MaterialsPanelHeader collapsed={true} onCollapsedChange={noop} unreadCount={2} />)
    const badge = screen.getByTitle('2 new materials')
    expect(badge).toBeTruthy()
    expect(badge.textContent).toBe('2')
  })

  it('shows singular label for unreadCount=1 when collapsed', () => {
    render(<MaterialsPanelHeader collapsed={true} onCollapsedChange={noop} unreadCount={1} />)
    expect(screen.getByTitle('1 new material')).toBeTruthy()
  })

  it('shows no badge when collapsed and unreadCount is 0', () => {
    render(<MaterialsPanelHeader collapsed={true} onCollapsedChange={noop} unreadCount={0} />)
    expect(screen.queryByTitle(/new material/i)).toBeNull()
  })

  it('hides Materials label text when collapsed', () => {
    render(<MaterialsPanelHeader collapsed={true} onCollapsedChange={noop} />)
    expect(screen.queryByText('Materials')).toBeNull()
  })

  it('shows counted "Materials (N)" rail label when collapsed and itemCount provided', () => {
    render(<MaterialsPanelHeader collapsed={true} onCollapsedChange={noop} itemCount={4} />)
    const label = screen.getByTestId('materials-collapsed-label')
    expect(label.textContent).toBe('Materials (4)')
  })

  it('renders counted label even when itemCount is zero', () => {
    render(<MaterialsPanelHeader collapsed={true} onCollapsedChange={noop} itemCount={0} />)
    expect(screen.getByTestId('materials-collapsed-label').textContent).toBe('Materials (0)')
  })

  it('falls back to chevron rail when itemCount is not provided', () => {
    render(<MaterialsPanelHeader collapsed={true} onCollapsedChange={noop} />)
    expect(screen.queryByTestId('materials-collapsed-label')).toBeNull()
    // Chevron remains the only visible affordance
    expect(screen.getByRole('button').textContent).toContain('▷')
  })

  it('counted rail aria-label includes item count and is pluralized', () => {
    const { rerender } = render(<MaterialsPanelHeader collapsed={true} onCollapsedChange={noop} itemCount={1} />)
    expect(screen.getByRole('button').getAttribute('aria-label')).toBe('Expand materials panel (1 item)')
    rerender(<MaterialsPanelHeader collapsed={true} onCollapsedChange={noop} itemCount={3} />)
    expect(screen.getByRole('button').getAttribute('aria-label')).toBe('Expand materials panel (3 items)')
  })

  it('expanded view is unchanged when itemCount is provided', () => {
    render(<MaterialsPanelHeader collapsed={false} onCollapsedChange={noop} itemCount={5} unreadCount={2} />)
    expect(screen.getByText('Materials')).toBeTruthy()
    expect(screen.getByText('New 2')).toBeTruthy()
    expect(screen.queryByTestId('materials-collapsed-label')).toBeNull()
  })

  it('clicking the collapsed counted rail toggles expansion', () => {
    let collapsed = true
    const setCollapsed = vi.fn((next) => { collapsed = next })
    const { rerender } = render(
      <MaterialsPanelHeader collapsed={collapsed} onCollapsedChange={setCollapsed} itemCount={2} />
    )
    screen.getByRole('button').click()
    expect(setCollapsed).toHaveBeenCalledWith(false)
    rerender(<MaterialsPanelHeader collapsed={false} onCollapsedChange={setCollapsed} itemCount={2} />)
    // No collapsed-label rendered when expanded
    expect(screen.queryByTestId('materials-collapsed-label')).toBeNull()
  })
})
