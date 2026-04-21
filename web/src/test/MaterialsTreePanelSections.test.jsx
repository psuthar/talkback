import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MaterialsTreePanel } from '../components/MaterialsTreePanel'

vi.mock('../api/materials', () => ({ getMaterialSlides: vi.fn().mockResolvedValue({ slides: [] }) }))

const noop = vi.fn()
const baseProps = {
  apiBaseUrl: '',
  selectedVideo: null,
  setSelectedVideo: noop,
  setVideoId: noop,
  setVideoPlayerKey: noop,
  onSelectDocument: noop,
  onSelectVideo: noop,
  onSelectLink: noop,
  selectedDocumentId: null,
  collapsed: false,
  onCollapsedChange: noop,
  hideHeader: true,
}

function makeSession(overrides = {}) {
  return {
    id: 'sess-1',
    video_sources: [],
    materials: [],
    links: [],
    unread_material_ids: [],
    primary_video: null,
    additional_videos: [],
    material_slides_ready: {},
    material_slides_status: {},
    ...overrides,
  }
}

describe('MaterialsTreePanel — empty section suppression', () => {
  it('hides Additional Videos section when no additional videos', () => {
    render(<MaterialsTreePanel {...baseProps} session={makeSession()} />)
    expect(screen.queryByText('Additional Videos')).toBeNull()
  })

  it('hides Documents section when no documents', () => {
    render(<MaterialsTreePanel {...baseProps} session={makeSession()} />)
    expect(screen.queryByText('Documents')).toBeNull()
  })

  it('hides Images section when no images', () => {
    render(<MaterialsTreePanel {...baseProps} session={makeSession()} />)
    expect(screen.queryByText('Images')).toBeNull()
  })

  it('hides Links section when no links', () => {
    render(<MaterialsTreePanel {...baseProps} session={makeSession()} />)
    expect(screen.queryByText('Links')).toBeNull()
  })

  it('hides Transcript section when no transcripts', () => {
    render(<MaterialsTreePanel {...baseProps} session={makeSession({ video_sources: [{ id: 'v1', transcript_text: null }] })} />)
    expect(screen.queryByText('Transcript')).toBeNull()
  })

  it('shows Documents section when documents exist', () => {
    const session = makeSession({
      materials: [{ id: 'm1', filename: 'report.pdf', kind: 'document', content_type: 'application/pdf', text_status: 'ready' }],
    })
    render(<MaterialsTreePanel {...baseProps} session={session} />)
    expect(screen.getByText('Documents')).toBeTruthy()
  })

  it('shows Images section when images exist', () => {
    const session = makeSession({
      materials: [{ id: 'img1', filename: 'photo.jpg', kind: 'image', content_type: 'image/jpeg', text_status: 'ready' }],
    })
    render(<MaterialsTreePanel {...baseProps} session={session} />)
    expect(screen.getByText('Images')).toBeTruthy()
  })

  it('shows Additional Videos section when additional videos exist', () => {
    const session = makeSession({
      additional_videos: [{ id: 'v2', stored_video_object_key: 'sessions/1/extra.mp4', transcript_status: 'ready' }],
    })
    render(<MaterialsTreePanel {...baseProps} session={session} />)
    expect(screen.getByText('Additional Videos')).toBeTruthy()
  })

  it('shows no-materials message when all sections are empty', () => {
    render(<MaterialsTreePanel {...baseProps} session={makeSession()} />)
    expect(screen.getByTestId('no-materials-message')).toBeTruthy()
  })

  it('does not show no-materials message when documents exist', () => {
    const session = makeSession({
      materials: [{ id: 'm1', filename: 'doc.pdf', kind: 'document', content_type: 'application/pdf', text_status: 'ready' }],
    })
    render(<MaterialsTreePanel {...baseProps} session={session} />)
    expect(screen.queryByTestId('no-materials-message')).toBeNull()
  })

  it('does not render None rows for any section', () => {
    render(<MaterialsTreePanel {...baseProps} session={makeSession()} />)
    expect(screen.queryByText('None')).toBeNull()
  })
})
