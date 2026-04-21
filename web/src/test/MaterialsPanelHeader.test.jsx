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
})
