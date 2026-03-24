import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { SessionLinksSection } from '../components/SessionLinksSection'

describe('SessionLinksSection', () => {
  it('renders the heading', () => {
    render(<SessionLinksSection sessionId="s1" links={[]} apiBaseUrl="" refetchSession={() => {}} />)
    expect(screen.getByText(/Links/)).toBeTruthy()
  })

  it('shows "No links added yet." when links is empty', () => {
    render(<SessionLinksSection sessionId="s1" links={[]} apiBaseUrl="" refetchSession={() => {}} />)
    expect(screen.getByText('No links added yet.')).toBeTruthy()
  })

  it('shows "No links added yet." when links is undefined', () => {
    render(<SessionLinksSection sessionId="s1" apiBaseUrl="" refetchSession={() => {}} />)
    expect(screen.getByText('No links added yet.')).toBeTruthy()
  })

  it('renders each link title or URL', () => {
    const links = [
      { id: 'l1', url: 'https://example.com', title: 'Example Site', status: 'verified' },
      { id: 'l2', url: 'https://other.com', title: '', status: 'pending' },
    ]
    render(<SessionLinksSection sessionId="s1" links={links} apiBaseUrl="" refetchSession={() => {}} />)
    expect(screen.getByText('Example Site')).toBeTruthy()
    // Second link has no title so URL appears as both the title and the URL display
    expect(screen.getAllByText('https://other.com').length).toBeGreaterThanOrEqual(1)
  })

  it('shows link count in heading when links are present', () => {
    const links = [
      { id: 'l1', url: 'https://a.com', title: 'A', status: 'verified' },
      { id: 'l2', url: 'https://b.com', title: 'B', status: 'verified' },
    ]
    render(<SessionLinksSection sessionId="s1" links={links} apiBaseUrl="" refetchSession={() => {}} />)
    expect(screen.getByText('Links (2)')).toBeTruthy()
  })

  it('renders "Add link" button', () => {
    render(<SessionLinksSection sessionId="s1" links={[]} apiBaseUrl="" refetchSession={() => {}} />)
    expect(screen.getByRole('button', { name: /add link/i })).toBeTruthy()
  })

  it('Add link button is disabled when input is empty', () => {
    render(<SessionLinksSection sessionId="s1" links={[]} apiBaseUrl="" refetchSession={() => {}} />)
    const btn = screen.getByRole('button', { name: /add link/i })
    expect(btn).toBeDisabled()
  })

  it('Add link button is enabled after typing a URL', async () => {
    const user = userEvent.setup()
    render(<SessionLinksSection sessionId="s1" links={[]} apiBaseUrl="" refetchSession={() => {}} />)
    const input = screen.getByPlaceholderText('https://...')
    await user.type(input, 'https://example.com')
    const btn = screen.getByRole('button', { name: /add link/i })
    expect(btn).not.toBeDisabled()
  })

  it('shows "Processing…" for pending/processing links', () => {
    const links = [{ id: 'l1', url: 'https://x.com', title: 'X', status: 'processing' }]
    render(<SessionLinksSection sessionId="s1" links={links} apiBaseUrl="" refetchSession={() => {}} />)
    expect(screen.getByText(/Processing/)).toBeTruthy()
  })

  it('renders Delete button for each existing link', () => {
    const links = [
      { id: 'l1', url: 'https://a.com', title: 'A', status: 'verified' },
      { id: 'l2', url: 'https://b.com', title: 'B', status: 'verified' },
    ]
    render(<SessionLinksSection sessionId="s1" links={links} apiBaseUrl="" refetchSession={() => {}} />)
    const deleteBtns = screen.getAllByRole('button', { name: /delete/i })
    expect(deleteBtns).toHaveLength(2)
  })
})
