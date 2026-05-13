import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MaterialsTreePanel } from '../components/MaterialsTreePanel'

const mockUpdateVideoDisplayTitle = vi.fn().mockResolvedValue(undefined)

vi.mock('../api/materials', () => ({
  getMaterialSlides: vi.fn().mockResolvedValue({ slides: [] }),
  updateVideoDisplayTitle: (...args) => mockUpdateVideoDisplayTitle(...args),
}))

const noop = vi.fn()
const baseProps = {
  apiBaseUrl: 'http://api',
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
  canManage: true,
}

function makeSession(videoOverrides = {}) {
  const video = {
    id: 'vs-1',
    provider: 'other',
    original_url: null,
    stored_video_object_key: null,
    display_title: null,
    transcript_status: 'ready',
    ...videoOverrides,
  }
  return {
    id: 'sess-1',
    video_sources: [video],
    materials: [],
    links: [],
    unread_material_ids: [],
    primary_video: video,
    additional_videos: [],
    material_slides_ready: {},
    material_slides_status: {},
  }
}

describe('Video display title — videoDisplayTitle()', () => {
  it('shows display_title when set', () => {
    render(<MaterialsTreePanel {...baseProps} session={makeSession({ display_title: 'My Custom Title' })} />)
    expect(screen.getByText('My Custom Title')).toBeTruthy()
  })

  it('falls back to provider when no display_title or URL', () => {
    render(<MaterialsTreePanel {...baseProps} session={makeSession({ provider: 'zoom', display_title: null })} />)
    expect(screen.getByText('zoom')).toBeTruthy()
  })

  it('falls back to filename from original_url when no display_title', () => {
    render(<MaterialsTreePanel {...baseProps} session={makeSession({ original_url: 'https://cdn.example.com/path/session.mp4', display_title: null })} />)
    expect(screen.getByText('session.mp4')).toBeTruthy()
  })
})

describe('Video display title — inline editing (creator)', () => {
  beforeEach(() => { mockUpdateVideoDisplayTitle.mockClear() })

  it('shows Edit title button when canManage is true', () => {
    render(<MaterialsTreePanel {...baseProps} canManage={true} session={makeSession()} />)
    expect(screen.getByTestId('edit-video-title-btn')).toBeTruthy()
  })

  it('does not show Edit title button when canManage is false', () => {
    render(<MaterialsTreePanel {...baseProps} canManage={false} session={makeSession()} />)
    expect(screen.queryByTestId('edit-video-title-btn')).toBeNull()
  })

  it('shows input when Edit title is clicked', () => {
    render(<MaterialsTreePanel {...baseProps} session={makeSession()} />)
    fireEvent.click(screen.getByTestId('edit-video-title-btn'))
    expect(screen.getByTestId('video-title-input')).toBeTruthy()
  })

  it('calls updateVideoDisplayTitle and hides input on Save', async () => {
    mockUpdateVideoDisplayTitle.mockResolvedValue(undefined)
    render(<MaterialsTreePanel {...baseProps} session={makeSession()} />)
    fireEvent.click(screen.getByTestId('edit-video-title-btn'))
    const input = screen.getByTestId('video-title-input')
    fireEvent.change(input, { target: { value: 'New Title' } })
    fireEvent.click(screen.getByText('Save'))
    await waitFor(() => expect(mockUpdateVideoDisplayTitle).toHaveBeenCalledWith('http://api', 'sess-1', 'vs-1', 'New Title'))
    await waitFor(() => expect(screen.queryByTestId('video-title-input')).toBeNull())
  })

  it('cancels editing on Cancel without saving', () => {
    render(<MaterialsTreePanel {...baseProps} session={makeSession()} />)
    fireEvent.click(screen.getByTestId('edit-video-title-btn'))
    fireEvent.click(screen.getByText('Cancel'))
    expect(screen.queryByTestId('video-title-input')).toBeNull()
    expect(mockUpdateVideoDisplayTitle).not.toHaveBeenCalled()
  })

  // SCRUM-400: prior to this fix, the save catch block was `catch (_) {}` which
  // silently swallowed the backend's 400 and collapsed the edit row, leaving
  // the user no signal that the rename failed.
  it('surfaces the error and keeps the edit row open when save fails (SCRUM-400)', async () => {
    mockUpdateVideoDisplayTitle.mockRejectedValueOnce(new Error('Failed to update display title: 400'))
    render(<MaterialsTreePanel {...baseProps} session={makeSession()} />)
    fireEvent.click(screen.getByTestId('edit-video-title-btn'))
    const input = screen.getByTestId('video-title-input')
    fireEvent.change(input, { target: { value: 'Doomed Rename' } })
    fireEvent.click(screen.getByText('Save'))
    await waitFor(() => expect(mockUpdateVideoDisplayTitle).toHaveBeenCalled())
    // Edit row stays open so the user can retry.
    expect(screen.getByTestId('video-title-input')).toBeTruthy()
    // Error message visible inline.
    const err = await screen.findByTestId('video-title-error')
    expect(err.textContent).toMatch(/Failed to update display title/)
  })

  it('clears the prior error when reopening the editor (SCRUM-400)', async () => {
    mockUpdateVideoDisplayTitle.mockRejectedValueOnce(new Error('Failed to update display title: 400'))
    render(<MaterialsTreePanel {...baseProps} session={makeSession()} />)
    fireEvent.click(screen.getByTestId('edit-video-title-btn'))
    fireEvent.change(screen.getByTestId('video-title-input'), { target: { value: 'Doomed' } })
    fireEvent.click(screen.getByText('Save'))
    await screen.findByTestId('video-title-error')
    // Cancel out then reopen — error should be gone.
    fireEvent.click(screen.getByText('Cancel'))
    fireEvent.click(screen.getByTestId('edit-video-title-btn'))
    expect(screen.queryByTestId('video-title-error')).toBeNull()
  })
})
