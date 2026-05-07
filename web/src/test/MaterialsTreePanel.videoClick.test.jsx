import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MaterialsTreePanel } from '../components/MaterialsTreePanel'

vi.mock('../api/materials', () => ({
  getMaterialSlides: vi.fn().mockResolvedValue({ slides: [] }),
}))
vi.mock('../api/sessionPrimary', async () => {
  const actual = await vi.importActual('../api/sessionPrimary')
  return { ...actual, patchSessionPrimary: vi.fn() }
})

const baseSession = {
  session: { id: 'sess-327' },
  video_sources: [{
    id: 'v1',
    display_title: 'Recruiter Screen',
    transcript_status: 'ready',
    file_artifact_id: 'fa-1',
  }],
  materials: [{
    id: 'm1', filename: 'resume.pdf', kind: 'document',
    content_type: 'application/pdf', text_status: 'ready',
  }],
  links: [],
  primary_video: null,
  additional_videos: [],
  unread_material_ids: [],
  material_slides_ready: {},
  material_slides_status: {},
}

describe('SCRUM-327 — clicking a video row notifies the parent to clear document selection', () => {
  it('invokes onSelectVideo (handleBackToVideo wiring) when a video row is clicked while a document is currently selected', () => {
    const setSelectedVideo = vi.fn()
    const setVideoId = vi.fn()
    const setVideoPlayerKey = vi.fn()
    const onSelectVideo = vi.fn() // wired to handleBackToVideo in ParticipantMode/CreatorMode

    render(
      <MaterialsTreePanel
        apiBaseUrl=""
        session={baseSession}
        selectedVideo={null}
        setSelectedVideo={setSelectedVideo}
        setVideoId={setVideoId}
        setVideoPlayerKey={setVideoPlayerKey}
        onSelectDocument={vi.fn()}
        onSelectVideo={onSelectVideo}
        onSelectLink={vi.fn()}
        selectedDocumentId="m1" // a document is currently rendered in the top pane
        collapsed={false}
        onCollapsedChange={vi.fn()}
        hideHeader
      />,
    )

    const videoRow = screen.getByTestId('primary-video-item')
    fireEvent.click(videoRow)

    // The fix relies on this callback firing so the parent's handleBackToVideo
    // can clear selectedDocument and (post-SCRUM-327) set the user-selected-
    // video guard. Without it, the top pane would stay on the primary document.
    expect(onSelectVideo).toHaveBeenCalledTimes(1)

    // And the standard video-selection state updates must also fire.
    expect(setSelectedVideo).toHaveBeenCalledWith(baseSession.video_sources[0])
    expect(setVideoId).toHaveBeenCalledWith('v1')
    expect(setVideoPlayerKey).toHaveBeenCalled()
  })

  it('invokes onSelectVideo even when no document is currently selected (idempotent)', () => {
    const onSelectVideo = vi.fn()
    render(
      <MaterialsTreePanel
        apiBaseUrl=""
        session={baseSession}
        selectedVideo={null}
        setSelectedVideo={vi.fn()}
        setVideoId={vi.fn()}
        setVideoPlayerKey={vi.fn()}
        onSelectDocument={vi.fn()}
        onSelectVideo={onSelectVideo}
        onSelectLink={vi.fn()}
        selectedDocumentId={null}
        collapsed={false}
        onCollapsedChange={vi.fn()}
        hideHeader
      />,
    )
    fireEvent.click(screen.getByTestId('primary-video-item'))
    expect(onSelectVideo).toHaveBeenCalledTimes(1)
  })
})
