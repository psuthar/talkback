import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { PrimaryStage } from '../components/PrimaryStage'

vi.mock('../components/DocumentViewer', () => ({
  DocumentViewer: ({ doc }) => <div data-testid="document-viewer">doc:{doc?.id}</div>,
}))
vi.mock('../VideoPlayer', () => ({
  VideoPlayer: ({ video }) => <div data-testid="video-player">video:{video?.id}</div>,
  PlayerEvent: {},
}))

describe('PrimaryStage (SCRUM-273)', () => {
  it('renders DocumentViewer when selectedDocument is set', () => {
    render(<PrimaryStage selectedDocument={{ id: 'doc-1', filename: 'spec.pdf' }} apiBaseUrl="" sessionId="s1" />)
    expect(screen.getByTestId('document-viewer')).toBeInTheDocument()
    expect(screen.queryByTestId('video-player-container')).toBeNull()
  })

  it('renders nothing when there is no document and no video sources / artifact', () => {
    const { container } = render(
      <PrimaryStage
        selectedDocument={null}
        currentSession={{ session: {}, video_sources: [] }}
      />,
    )
    expect(container.firstChild).toBeNull()
  })

  it('renders the video container when currentSession has video_sources', () => {
    render(
      <PrimaryStage
        selectedDocument={null}
        currentSession={{ session: {}, video_sources: [{ id: 'vs-1' }] }}
        video={{ id: 'vs-1', transcript_status: 'ready' }}
      />,
    )
    expect(screen.getByTestId('video-player-container')).toBeInTheDocument()
    expect(screen.getByTestId('video-player')).toBeInTheDocument()
  })

  it('renders the ingest-pending message in place of the video player when primary_video_artifact_id is set but not ready', () => {
    render(
      <PrimaryStage
        selectedDocument={null}
        currentSession={{
          session: { primary_video_artifact_id: 'fa-1' },
          playback_reason_code: 'VIDEO_INGEST_PENDING',
          playback_message: 'Video is still being prepared.',
        }}
        primaryVideoAccessUrl=""
        video={{ id: 'fa-1', transcript_status: 'pending' }}
      />,
    )
    expect(screen.getByText(/Video is still being prepared/i)).toBeInTheDocument()
    expect(screen.queryByTestId('video-player')).toBeNull()
  })

  it('renders Retry ingest button when ingest failed and retryProcessing is provided', () => {
    const retryProcessing = vi.fn()
    render(
      <PrimaryStage
        selectedDocument={null}
        currentSession={{
          session: { primary_video_artifact_id: 'fa-1' },
          playback_reason_code: 'VIDEO_INGEST_FAILED',
          playback_message: 'Video ingest failed.',
        }}
        primaryVideoAccessUrl=""
        retryProcessing={retryProcessing}
        processingRetrying={false}
      />,
    )
    const btn = screen.getByRole('button', { name: /Retry ingest/i })
    expect(btn).toBeEnabled()
  })

  it('falls back to DocumentViewer when selectedDocument is null but primary.kind=document (SCRUM-284)', () => {
    render(
      <PrimaryStage
        selectedDocument={null}
        apiBaseUrl=""
        sessionId="s1"
        currentSession={{
          session: {},
          video_sources: [],
          materials: [
            { id: 'mat-42', filename: 'spec.pdf', text_status: 'ready' },
          ],
          primary: { kind: 'document', id: 'mat-42' },
        }}
      />,
    )
    expect(screen.getByTestId('document-viewer')).toBeInTheDocument()
    expect(screen.getByTestId('document-viewer').textContent).toBe('doc:mat-42')
    expect(screen.queryByTestId('video-player-container')).toBeNull()
  })

  it('falls back to DocumentViewer (link iframe shape) when selectedDocument is null but primary.kind=link (SCRUM-284)', () => {
    render(
      <PrimaryStage
        selectedDocument={null}
        apiBaseUrl=""
        sessionId="s1"
        currentSession={{
          session: {},
          video_sources: [],
          links: [
            { id: 'link-7', url: 'https://example.com/spec', title: 'Reference link' },
          ],
          primary: { kind: 'link', id: 'link-7' },
        }}
      />,
    )
    expect(screen.getByTestId('document-viewer')).toBeInTheDocument()
    expect(screen.getByTestId('document-viewer').textContent).toBe('doc:link-link-7')
    expect(screen.queryByTestId('video-player-container')).toBeNull()
  })

  it('returns null when primary.kind=document but the material is missing (resolver collapse)', () => {
    const { container } = render(
      <PrimaryStage
        selectedDocument={null}
        currentSession={{
          session: {},
          video_sources: [],
          materials: [],
          primary: { kind: 'document', id: 'mat-missing' },
        }}
      />,
    )
    expect(container.firstChild).toBeNull()
  })

  it('renders the transcript header when hasPrimaryR2Video is true', () => {
    render(
      <PrimaryStage
        selectedDocument={null}
        currentSession={{ session: { primary_video_artifact_id: 'fa-1' }, video_sources: [{ id: 'fa-1' }] }}
        primaryVideoAccessUrl="https://example/video"
        hasPrimaryR2Video={true}
        video={{ id: 'fa-1', transcript_status: 'ready' }}
      />,
    )
    expect(screen.getByText(/Transcript:/)).toBeInTheDocument()
    expect(screen.getByText(/Ready/)).toBeInTheDocument()
  })
})
