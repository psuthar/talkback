import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, act } from '@testing-library/react'
import { StanceCountTooltip } from '../components/StanceCountTooltip'

describe('StanceCountTooltip', () => {
  beforeEach(() => { vi.useFakeTimers() })
  afterEach(() => { vi.useRealTimers() })

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

  it('renders count chip with label and number', () => {
    render(<StanceCountTooltip {...baseProps} />)
    const btn = screen.getByTestId('stance-count-agree')
    expect(btn.textContent).toContain('Agree')
    expect(btn.textContent).toContain('3')
  })

  it('does not show tooltip on initial render', () => {
    render(<StanceCountTooltip {...baseProps} />)
    expect(screen.queryByTestId('stance-tooltip-agree')).toBeNull()
  })

  it('opens tooltip after ~150ms hover-in delay', () => {
    render(<StanceCountTooltip {...baseProps} />)
    fireEvent.mouseEnter(screen.getByTestId('stance-count-agree').parentElement)
    expect(screen.queryByTestId('stance-tooltip-agree')).toBeNull()
    act(() => { vi.advanceTimersByTime(149) })
    expect(screen.queryByTestId('stance-tooltip-agree')).toBeNull()
    act(() => { vi.advanceTimersByTime(2) })
    expect(screen.getByTestId('stance-tooltip-agree')).toBeTruthy()
  })

  it('closes tooltip after ~300ms hover-out delay', () => {
    render(<StanceCountTooltip {...baseProps} />)
    const wrap = screen.getByTestId('stance-count-agree').parentElement
    fireEvent.mouseEnter(wrap)
    act(() => { vi.advanceTimersByTime(160) })
    expect(screen.getByTestId('stance-tooltip-agree')).toBeTruthy()
    fireEvent.mouseLeave(wrap)
    expect(screen.getByTestId('stance-tooltip-agree')).toBeTruthy()
    act(() => { vi.advanceTimersByTime(310) })
    expect(screen.queryByTestId('stance-tooltip-agree')).toBeNull()
  })

  it('cancels hover-out timer when re-entered before timeout', () => {
    render(<StanceCountTooltip {...baseProps} />)
    const wrap = screen.getByTestId('stance-count-agree').parentElement
    fireEvent.mouseEnter(wrap)
    act(() => { vi.advanceTimersByTime(160) })
    fireEvent.mouseLeave(wrap)
    act(() => { vi.advanceTimersByTime(100) })
    fireEvent.mouseEnter(wrap)
    act(() => { vi.advanceTimersByTime(500) })
    expect(screen.getByTestId('stance-tooltip-agree')).toBeTruthy()
  })

  it('click pins the tooltip open and a second click unpins', () => {
    render(<StanceCountTooltip {...baseProps} />)
    const btn = screen.getByTestId('stance-count-agree')
    fireEvent.click(btn)
    expect(screen.getByTestId('stance-tooltip-agree')).toBeTruthy()
    expect(btn.getAttribute('data-pinned')).toBe('true')
    // Mouse leaves but tooltip stays because it is pinned
    fireEvent.mouseLeave(btn.parentElement)
    act(() => { vi.advanceTimersByTime(1000) })
    expect(screen.getByTestId('stance-tooltip-agree')).toBeTruthy()
    fireEvent.click(btn)
    expect(btn.getAttribute('data-pinned')).toBe('false')
    expect(screen.queryByTestId('stance-tooltip-agree')).toBeNull()
  })

  it('opens on focus and closes on blur (keyboard accessible)', () => {
    render(<StanceCountTooltip {...baseProps} />)
    const wrap = screen.getByTestId('stance-count-agree').parentElement
    fireEvent.focus(wrap)
    act(() => { vi.advanceTimersByTime(200) })
    expect(screen.getByTestId('stance-tooltip-agree')).toBeTruthy()
    fireEvent.blur(wrap)
    act(() => { vi.advanceTimersByTime(400) })
    expect(screen.queryByTestId('stance-tooltip-agree')).toBeNull()
  })

  it('renders members list inside the tooltip', () => {
    render(<StanceCountTooltip {...baseProps} />)
    fireEvent.click(screen.getByTestId('stance-count-agree'))
    const tooltip = screen.getByTestId('stance-tooltip-agree')
    expect(tooltip.textContent).toContain('alice@example.com')
    expect(tooltip.textContent).toContain('bob@example.com')
    expect(tooltip.textContent).toContain('carol@example.com')
    expect(tooltip.textContent).toContain('Agree (3)')
  })

  it('renders an empty placeholder when members list is empty', () => {
    render(<StanceCountTooltip {...baseProps} count={0} members={[]} />)
    fireEvent.click(screen.getByTestId('stance-count-agree'))
    const tooltip = screen.getByTestId('stance-tooltip-agree')
    expect(tooltip.textContent).toContain('—')
  })

  it('handles missing user_email gracefully', () => {
    render(<StanceCountTooltip {...baseProps} members={[{ id: 'x' }]} count={1} />)
    fireEvent.click(screen.getByTestId('stance-count-agree'))
    expect(screen.getByTestId('stance-tooltip-agree').textContent).toContain('Unknown')
  })
})
