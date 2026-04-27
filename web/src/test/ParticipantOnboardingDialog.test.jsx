import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { ParticipantOnboardingDialog } from '../components/ParticipantOnboardingDialog'

describe('ParticipantOnboardingDialog', () => {
  it('renders nothing when closed', () => {
    const { container } = render(<ParticipantOnboardingDialog open={false} onDismiss={() => {}} />)
    expect(container.firstChild).toBeNull()
  })

  it('shows dialog with title and primary action when open', () => {
    render(<ParticipantOnboardingDialog open onDismiss={() => {}} />)
    const dialog = screen.getByRole('dialog', { name: 'How this session works' })
    expect(dialog).toBeTruthy()
    expect(screen.getByTestId('participant-onboarding-dismiss')).toBeTruthy()
    expect(dialog.textContent).toMatch(/main presentation video/i)
  })

  it('calls onDismiss when Got it is clicked', () => {
    const onDismiss = vi.fn()
    render(<ParticipantOnboardingDialog open onDismiss={onDismiss} />)
    fireEvent.click(screen.getByTestId('participant-onboarding-dismiss'))
    expect(onDismiss).toHaveBeenCalledTimes(1)
  })

  it('calls onDismiss on Escape', () => {
    const onDismiss = vi.fn()
    render(<ParticipantOnboardingDialog open onDismiss={onDismiss} />)
    fireEvent.keyDown(document, { key: 'Escape', bubbles: true })
    expect(onDismiss).toHaveBeenCalledTimes(1)
  })
})
