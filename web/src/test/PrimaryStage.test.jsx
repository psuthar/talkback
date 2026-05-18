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

  it('renders the creator empty-state when there is no resolvable primary and no fallback video (SCRUM-288)', () => {
    render(
      <PrimaryStage
        selectedDocument={null}
        currentSession={{ session: {}, video_sources: [] }}
      />,
    )
    const empty = screen.getByTestId('primary-empty-state')
    expect(empty).toBeInTheDocument()
    expect(empty.textContent).toMatch(/No primary content yet/i)
    expect(empty.textContent).toMatch(/Pick any material, link, or video/i)
    // Empty-state replaces the legacy null-render; video container must not appear.
    expect(screen.queryByTestId('video-player-container')).toBeNull()
  })

  it('renders the participant empty-state when mode=participant (SCRUM-288)', () => {
    render(
      <PrimaryStage
        selectedDocument={null}
        currentSession={{ session: {}, video_sources: [] }}
        mode="participant"
      />,
    )
    const empty = screen.getByTestId('primary-empty-state')
    expect(empty.textContent).toMatch(/Waiting for the creator/i)
    expect(empty.textContent).not.toMatch(/Pick any material/i)
  })

  it('renders the video container when currentSession has video_sources AND a primary artifact', () => {
    // SCRUM-471: a video_source alone is no longer enough to render the
    // player. The session must also have primary_video_artifact_id set,
    // since post-creation imports now land as secondary and the user
    // explicitly promotes one to primary.
    render(
      <PrimaryStage
        selectedDocument={null}
        currentSession={{ session: { primary_video_artifact_id: 'fa-1' }, video_sources: [{ id: 'vs-1' }] }}
        video={{ id: 'vs-1', transcript_status: 'ready' }}
        primaryVideoAccessUrl="https://example.com/v.mp4"
      />,
    )
    expect(screen.getByTestId('video-player-container')).toBeInTheDocument()
    expect(screen.getByTestId('video-player')).toBeInTheDocument()
  })

  it('SCRUM-471: video_sources present but no primary → empty state with "Make primary" hint', () => {
    render(
      <PrimaryStage
        selectedDocument={null}
        currentSession={{ session: {}, video_sources: [{ id: 'vs-1' }] }}
        video={{ id: 'vs-1', transcript_status: 'ready' }}
        mode="creator"
      />,
    )
    const empty = screen.getByTestId('primary-empty-state')
    expect(empty.textContent).toMatch(/Make primary/i)
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

  it('renders the empty-state when primary.kind=document but the material is missing (SCRUM-284 collapse → SCRUM-288 empty-state)', () => {
    render(
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
    // SCRUM-282 prevents this state in the data model, but if it somehow
    // surfaces (e.g. mid-refresh), the empty-state is the right UX rather
    // than a blank pane.
    expect(screen.getByTestId('primary-empty-state')).toBeInTheDocument()
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

  // SCRUM-328: when the user has explicitly clicked a video row, PrimaryStage
  // must NOT fall back to rendering the session's primary document/link even
  // though selectedDocument is null. Without the guard the user's video
  // choice is silently overridden and the player never renders in Creator
  // mode (the parent passes selectedDocument=null after handleSelectVideo).
  describe('SCRUM-328 — userSelectedVideo bypasses the primary-descriptor fallback', () => {
    it('skips the document fallback when userSelectedVideo=true and renders the video player instead', () => {
      render(
        <PrimaryStage
          selectedDocument={null}
          userSelectedVideo={true}
          apiBaseUrl=""
          sessionId="s1"
          currentSession={{
            // SCRUM-471: a session with video_sources but no primary now
            // renders the empty state; the SCRUM-328 fallback guard only
            // matters when there IS a primary artifact set. Add one here.
            session: { primary_video_artifact_id: 'fa-vs-1' },
            video_sources: [{ id: 'vs-1' }],
            materials: [{ id: 'mat-42', filename: 'spec.pdf', text_status: 'ready' }],
            primary: { kind: 'document', id: 'mat-42' },
          }}
          video={{ id: 'vs-1', transcript_status: 'ready' }}
          primaryVideoAccessUrl="https://example.com/v.mp4"
        />,
      )
      expect(screen.getByTestId('video-player-container')).toBeInTheDocument()
      expect(screen.getByTestId('video-player')).toBeInTheDocument()
      expect(screen.queryByTestId('document-viewer')).toBeNull()
    })

    it('skips the link fallback when userSelectedVideo=true and renders the video player instead', () => {
      render(
        <PrimaryStage
          selectedDocument={null}
          userSelectedVideo={true}
          apiBaseUrl=""
          sessionId="s1"
          currentSession={{
            session: { primary_video_artifact_id: 'fa-vs-2' },
            video_sources: [{ id: 'vs-2' }],
            links: [{ id: 'link-7', url: 'https://example.com', title: 'ref' }],
            primary: { kind: 'link', id: 'link-7' },
          }}
          video={{ id: 'vs-2', transcript_status: 'ready' }}
          primaryVideoAccessUrl="https://example.com/v.mp4"
        />,
      )
      expect(screen.getByTestId('video-player-container')).toBeInTheDocument()
      expect(screen.queryByTestId('document-viewer')).toBeNull()
    })

    it('still renders the document fallback when userSelectedVideo=false (existing SCRUM-284 behavior preserved)', () => {
      render(
        <PrimaryStage
          selectedDocument={null}
          userSelectedVideo={false}
          apiBaseUrl=""
          sessionId="s1"
          currentSession={{
            session: {},
            video_sources: [{ id: 'vs-1' }],
            materials: [{ id: 'mat-42', filename: 'spec.pdf', text_status: 'ready' }],
            primary: { kind: 'document', id: 'mat-42' },
          }}
          video={{ id: 'vs-1', transcript_status: 'ready' }}
        />,
      )
      expect(screen.getByTestId('document-viewer')).toBeInTheDocument()
      expect(screen.queryByTestId('video-player-container')).toBeNull()
    })

    it('still renders an explicit selectedDocument even if userSelectedVideo=true (parent intent wins on every render)', () => {
      // Defensive: if the parent ever passes both an explicit selectedDocument
      // AND userSelectedVideo=true, the explicit doc selection takes
      // precedence. This is a guardrail against accidental contradictory
      // state in callers; in practice handleSelectDocument will reset
      // userSelectedVideo to false.
      render(
        <PrimaryStage
          selectedDocument={{ id: 'doc-explicit' }}
          userSelectedVideo={true}
          apiBaseUrl=""
          sessionId="s1"
          currentSession={{ session: {}, video_sources: [{ id: 'vs-1' }] }}
          video={{ id: 'vs-1' }}
        />,
      )
      expect(screen.getByTestId('document-viewer').textContent).toBe('doc:doc-explicit')
      expect(screen.queryByTestId('video-player-container')).toBeNull()
    })
  })
})
