import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { ArtifactsList } from '../components/ArtifactsList'

describe('ArtifactsList', () => {
  it('renders nothing when artifacts is empty array', () => {
    const { container } = render(<ArtifactsList artifacts={[]} />)
    expect(container.firstChild).toBeNull()
  })

  it('renders nothing when artifacts is null', () => {
    const { container } = render(<ArtifactsList artifacts={null} />)
    expect(container.firstChild).toBeNull()
  })

  it('renders artifacts count in heading', () => {
    const artifacts = [
      { id: '1', title: 'Slide Deck', status: 'ready' },
      { id: '2', title: 'Notes', status: 'pending' },
    ]
    render(<ArtifactsList artifacts={artifacts} />)
    expect(screen.getByText('Artifacts (2)')).toBeTruthy()
  })

  it('renders each artifact title', () => {
    const artifacts = [
      { id: '1', title: 'Recording Transcript', status: 'ready' },
      { id: '2', title: 'Summary', status: 'pending' },
    ]
    render(<ArtifactsList artifacts={artifacts} />)
    expect(screen.getByText('Recording Transcript')).toBeTruthy()
    expect(screen.getByText('Summary')).toBeTruthy()
  })

  it('renders artifact description when provided', () => {
    const artifacts = [{ id: '1', title: 'Deck', description: 'Full slide deck', status: 'ready' }]
    render(<ArtifactsList artifacts={artifacts} />)
    expect(screen.getByText('Full slide deck')).toBeTruthy()
  })

  it('does not render description element when not provided', () => {
    const artifacts = [{ id: '1', title: 'Deck', status: 'ready' }]
    render(<ArtifactsList artifacts={artifacts} />)
    // Only one element with this class; no description text
    expect(screen.queryByText('Full slide deck')).toBeNull()
  })

  it('renders status for each artifact', () => {
    const artifacts = [{ id: '1', title: 'Deck', status: 'ready' }]
    render(<ArtifactsList artifacts={artifacts} />)
    expect(screen.getByText('ready')).toBeTruthy()
  })

  it('renders single artifact correctly', () => {
    const artifacts = [{ id: '42', title: 'Only Artifact', status: 'processing' }]
    render(<ArtifactsList artifacts={artifacts} />)
    expect(screen.getByText('Artifacts (1)')).toBeTruthy()
    expect(screen.getByText('Only Artifact')).toBeTruthy()
    expect(screen.getByText('processing')).toBeTruthy()
  })
})
