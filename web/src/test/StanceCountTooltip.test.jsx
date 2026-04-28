import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { StanceCountTooltip } from '../components/StanceCountTooltip'

describe('StanceCountTooltip (click-only)', () => {
  const baseProps = {
    stance: 'agree',
    label: 'Agree',
    count: 3,
    members: [
      { id: '1', user_email: 'alice@example.com' },
      { id: '2', user_email: 'bob@example.com' },
      { id: '3', user_email: 'carol@example.com' },
    ],
  }

  it('renders count chip with label, number, and chevron when members are present', () => {
    render(<StanceCountTooltip {...baseProps} />)
    const btn = screen.getByTestId('stance-count-agree')
    expect(btn.textContent).toContain('Agree')
    expect(btn.textContent).toContain('3')
    expect(btn.textContent).toContain('▾')
    expect(btn.getAttribute('aria-haspopup')).toBe('true')
    expect(btn.getAttribute('aria-expanded')).toBe('false')
    expect(btn.getAttribute('aria-label')).toContain('tap to view members')
  })

  it('omits the chevron and the "tap" hint when there are no members', () => {
    render(<StanceCountTooltip {...baseProps} count={0} members={[]} />)
    const btn = screen.getByTestId('stance-count-agree')
    expect(btn.textContent).not.toContain('▾')
    expect(btn.getAttribute('aria-label')).not.toContain('tap to view members')
  })

  it('does not show the tooltip on initial render', () => {
    render(<StanceCountTooltip {...baseProps} />)
    expect(screen.queryByTestId('stance-tooltip-agree')).toBeNull()
  })

  it('does not open on hover (uncontrolled)', () => {
    render(<StanceCountTooltip {...baseProps} />)
    const wrap = screen.getByTestId('stance-count-agree').parentElement
    fireEvent.mouseEnter(wrap)
    expect(screen.queryByTestId('stance-tooltip-agree')).toBeNull()
    fireEvent.mouseOver(wrap)
    expect(screen.queryByTestId('stance-tooltip-agree')).toBeNull()
  })

  it('does not open on focus (uncontrolled)', () => {
    render(<StanceCountTooltip {...baseProps} />)
    const wrap = screen.getByTestId('stance-count-agree').parentElement
    fireEvent.focus(wrap)
    expect(screen.queryByTestId('stance-tooltip-agree')).toBeNull()
  })

  it('click opens the tooltip; second click closes it', () => {
    render(<StanceCountTooltip {...baseProps} />)
    const btn = screen.getByTestId('stance-count-agree')
    fireEvent.click(btn)
    expect(screen.getByTestId('stance-tooltip-agree')).toBeTruthy()
    expect(btn.getAttribute('aria-expanded')).toBe('true')
    fireEvent.click(btn)
    expect(screen.queryByTestId('stance-tooltip-agree')).toBeNull()
    expect(btn.getAttribute('aria-expanded')).toBe('false')
  })

  it('Escape closes an open tooltip', () => {
    render(<StanceCountTooltip {...baseProps} />)
    fireEvent.click(screen.getByTestId('stance-count-agree'))
    expect(screen.getByTestId('stance-tooltip-agree')).toBeTruthy()
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByTestId('stance-tooltip-agree')).toBeNull()
  })

  it('outside mousedown closes an open tooltip', () => {
    render(
      <div>
        <StanceCountTooltip {...baseProps} />
        <div data-testid="outside" />
      </div>
    )
    fireEvent.click(screen.getByTestId('stance-count-agree'))
    expect(screen.getByTestId('stance-tooltip-agree')).toBeTruthy()
    fireEvent.mouseDown(screen.getByTestId('outside'))
    expect(screen.queryByTestId('stance-tooltip-agree')).toBeNull()
  })

  it('renders the members list inside the tooltip', () => {
    render(<StanceCountTooltip {...baseProps} />)
    fireEvent.click(screen.getByTestId('stance-count-agree'))
    const tooltip = screen.getByTestId('stance-tooltip-agree')
    expect(tooltip.textContent).toContain('alice@example.com')
    expect(tooltip.textContent).toContain('bob@example.com')
    expect(tooltip.textContent).toContain('carol@example.com')
    expect(tooltip.textContent).toContain('Agree (3)')
  })

  it('shows an empty placeholder when members list is empty', () => {
    render(<StanceCountTooltip {...baseProps} count={0} members={[]} />)
    fireEvent.click(screen.getByTestId('stance-count-agree'))
    expect(screen.getByTestId('stance-tooltip-agree').textContent).toContain('—')
  })

  it('handles missing user_email gracefully', () => {
    render(<StanceCountTooltip {...baseProps} members={[{ id: 'x' }]} count={1} />)
    fireEvent.click(screen.getByTestId('stance-count-agree'))
    expect(screen.getByTestId('stance-tooltip-agree').textContent).toContain('Unknown')
  })

  describe('controlled mode', () => {
    it('respects external isOpen prop and does not maintain internal state', () => {
      const { rerender } = render(
        <StanceCountTooltip {...baseProps} isOpen={false} onToggle={() => {}} />
      )
      expect(screen.queryByTestId('stance-tooltip-agree')).toBeNull()
      rerender(<StanceCountTooltip {...baseProps} isOpen onToggle={() => {}} />)
      expect(screen.getByTestId('stance-tooltip-agree')).toBeTruthy()
    })

    it('click invokes onToggle with the stance key when opening, and null when closing', () => {
      const onToggle = vi.fn()
      const { rerender } = render(
        <StanceCountTooltip {...baseProps} isOpen={false} onToggle={onToggle} />
      )
      fireEvent.click(screen.getByTestId('stance-count-agree'))
      expect(onToggle).toHaveBeenLastCalledWith('agree')
      rerender(<StanceCountTooltip {...baseProps} isOpen onToggle={onToggle} />)
      fireEvent.click(screen.getByTestId('stance-count-agree'))
      expect(onToggle).toHaveBeenLastCalledWith(null)
    })

    it('Escape on a controlled tooltip invokes onToggle(null)', () => {
      const onToggle = vi.fn()
      render(<StanceCountTooltip {...baseProps} isOpen onToggle={onToggle} />)
      fireEvent.keyDown(document, { key: 'Escape' })
      expect(onToggle).toHaveBeenCalledWith(null)
    })
  })

  it('cleans up document listeners when unmounted while open', () => {
    const removeSpy = vi.spyOn(document, 'removeEventListener')
    const { unmount } = render(<StanceCountTooltip {...baseProps} />)
    fireEvent.click(screen.getByTestId('stance-count-agree'))
    unmount()
    expect(removeSpy).toHaveBeenCalledWith('mousedown', expect.any(Function))
    expect(removeSpy).toHaveBeenCalledWith('keydown', expect.any(Function))
    removeSpy.mockRestore()
  })
})
