import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { OrchestrationPanelHeader } from '../components/OrchestrationPanelHeader'

describe('OrchestrationPanelHeader (SCRUM-195)', () => {
  it('renders the panel title as 12px non-bold secondary text', () => {
    render(<OrchestrationPanelHeader loading={false} disabled={false} onRefresh={() => {}} />)
    const title = screen.getByTestId('orchestration-panel-title')
    expect(title.textContent).toBe('Suggested next actions')
    expect(title.style.fontSize).toBe('12px')
    // Not bold
    expect(['400', '500', 'normal']).toContain(title.style.fontWeight)
  })

  it('preserves the orchestration-refresh-btn testid for E2E targeting', () => {
    render(<OrchestrationPanelHeader loading={false} disabled={false} onRefresh={() => {}} />)
    expect(screen.getByTestId('orchestration-refresh-btn')).toBeTruthy()
  })

  it('refresh button is icon-only with aria-label and tooltip', () => {
    render(<OrchestrationPanelHeader loading={false} disabled={false} onRefresh={() => {}} />)
    const btn = screen.getByTestId('orchestration-refresh-btn')
    expect(btn.getAttribute('aria-label')).toBe('Refresh suggestions')
    expect(btn.getAttribute('title')).toBe('Refresh suggestions')
    // No visible text label
    expect(btn.textContent).toBe('')
    // Has the SVG icon
    expect(screen.getByTestId('orchestration-refresh-icon')).toBeTruthy()
  })

  it('renders a spinner instead of the arrow icon while loading', () => {
    render(<OrchestrationPanelHeader loading disabled onRefresh={() => {}} />)
    expect(screen.getByTestId('orchestration-refresh-spinner')).toBeTruthy()
    expect(screen.queryByTestId('orchestration-refresh-icon')).toBeNull()
  })

  it('updates the title attr to "Refreshing suggestions…" while loading (still aria-labeled)', () => {
    render(<OrchestrationPanelHeader loading disabled onRefresh={() => {}} />)
    const btn = screen.getByTestId('orchestration-refresh-btn')
    expect(btn.getAttribute('title')).toBe('Refreshing suggestions…')
    // aria-label stays stable so screen readers don't get a different name during loading
    expect(btn.getAttribute('aria-label')).toBe('Refresh suggestions')
  })

  it('clicking the refresh button invokes onRefresh', () => {
    const onRefresh = vi.fn()
    render(<OrchestrationPanelHeader loading={false} disabled={false} onRefresh={onRefresh} />)
    fireEvent.click(screen.getByTestId('orchestration-refresh-btn'))
    expect(onRefresh).toHaveBeenCalled()
  })

  it('disables the refresh button when disabled is true (no-op click)', () => {
    const onRefresh = vi.fn()
    render(<OrchestrationPanelHeader loading={false} disabled onRefresh={onRefresh} />)
    const btn = screen.getByTestId('orchestration-refresh-btn')
    expect(btn.disabled).toBe(true)
    fireEvent.click(btn)
    expect(onRefresh).not.toHaveBeenCalled()
  })
})
