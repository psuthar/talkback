import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { StartHereChip } from '../components/StartHereChip'

describe('StartHereChip (SCRUM-484)', () => {
  it('renders nothing when open is false', () => {
    const { container } = render(<StartHereChip open={false} onDismiss={() => {}} />)
    expect(container.firstChild).toBeNull()
  })

  it('renders the chip when open is true', () => {
    render(<StartHereChip open onDismiss={() => {}} />)
    expect(screen.getByTestId('start-here-chip')).toBeInTheDocument()
    expect(screen.getByTestId('start-here-chip').textContent).toMatch(/start here/i)
  })

  it('calls onDismiss when the × button is clicked', () => {
    const onDismiss = vi.fn()
    render(<StartHereChip open onDismiss={onDismiss} />)
    fireEvent.click(screen.getByTestId('start-here-chip-dismiss'))
    expect(onDismiss).toHaveBeenCalledTimes(1)
  })

  it('does not propagate the dismiss click to the row', () => {
    const onRowClick = vi.fn()
    render(
      <div onClick={onRowClick}>
        <StartHereChip open onDismiss={() => {}} />
      </div>,
    )
    fireEvent.click(screen.getByTestId('start-here-chip-dismiss'))
    expect(onRowClick).not.toHaveBeenCalled()
  })
})
