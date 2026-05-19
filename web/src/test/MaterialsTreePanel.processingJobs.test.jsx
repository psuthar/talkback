// SCRUM-468: pin the in-progress import placeholder rows in
// MaterialsTreePanel. Before this, the user saw a blank VIDEOS section
// for the entire ingest duration (3-10 min) after clicking Import.
import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MaterialsTreePanel } from '../components/MaterialsTreePanel'

const baseSession = (overrides = {}) => ({
  session: { id: 's-1' },
  video_sources: [],
  materials: [],
  links: [],
  unread_material_ids: [],
  processing_jobs: [],
  ...overrides,
})

describe('SCRUM-468: MaterialsTreePanel processing_jobs placeholder rows', () => {
  it('renders a placeholder row for each non-terminal processing job', () => {
    render(
      <MaterialsTreePanel
        session={baseSession({
          processing_jobs: [
            { id: 'job-1', source: 'zoom', state: 'queued', stage: 'fetch', created_at: '', updated_at: '' },
            { id: 'job-2', source: 'teams', state: 'downloading', stage: 'download', created_at: '', updated_at: '' },
            { id: 'job-3', source: 'google_meet', state: 'parsing', stage: 'parse', created_at: '', updated_at: '' },
          ],
        })}
        apiBaseUrl=""
      />
    )
    expect(screen.getByTestId('processing-job-row-job-1')).toBeTruthy()
    expect(screen.getByTestId('processing-job-row-job-2')).toBeTruthy()
    expect(screen.getByTestId('processing-job-row-job-3')).toBeTruthy()
    // Per-state copy.
    expect(screen.getByTestId('processing-job-row-job-1').textContent).toMatch(/Queued to import from Zoom/i)
    expect(screen.getByTestId('processing-job-row-job-2').textContent).toMatch(/Downloading from Microsoft Teams/i)
    expect(screen.getByTestId('processing-job-row-job-3').textContent).toMatch(/Indexing transcript/i)
    // SCRUM-483: soft duration copy (avoid the older "3–10 min" implementation detail).
    expect(screen.getByTestId('processing-job-row-job-1').textContent).toMatch(/this could take a few minutes/i)
    expect(screen.getByTestId('processing-job-row-job-1').textContent).not.toMatch(/3.10 min/i)
  })

  it('shows the VIDEOS section even when there are zero real videos but a job is in flight', () => {
    render(
      <MaterialsTreePanel
        session={baseSession({
          video_sources: [],
          processing_jobs: [
            { id: 'job-1', source: 'zoom', state: 'fetching', stage: 'fetch', created_at: '', updated_at: '' },
          ],
        })}
        apiBaseUrl=""
      />
    )
    // Verify the row is rendered (means the surrounding section is too).
    expect(screen.getByTestId('processing-job-row-job-1')).toBeTruthy()
  })

  it('renders placeholders ABOVE real video_source rows', () => {
    const { container } = render(
      <MaterialsTreePanel
        session={baseSession({
          video_sources: [{
            id: 'vs-1', provider: 'zoom', video_url: 'https://x', display_title: 'Existing video',
            transcript_status: 'ready',
          }],
          processing_jobs: [
            { id: 'job-1', source: 'teams', state: 'downloading', stage: 'download', created_at: '', updated_at: '' },
          ],
        })}
        apiBaseUrl=""
      />
    )
    const placeholder = screen.getByTestId('processing-job-row-job-1')
    // Find the real video row by data-testid (primary or video-item).
    const realRow = container.querySelector('[data-testid="primary-video-item"], [data-testid="video-item"]')
    expect(realRow).toBeTruthy()
    // DOM order: placeholder before real row.
    expect(placeholder.compareDocumentPosition(realRow) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('failed_transient job surfaces a "Retrying…" copy', () => {
    render(
      <MaterialsTreePanel
        session={baseSession({
          processing_jobs: [
            { id: 'job-flap', source: 'zoom', state: 'failed_transient', stage: 'fetch',
              last_error_message: 'Zoom API 502', created_at: '', updated_at: '' },
          ],
        })}
        apiBaseUrl=""
      />
    )
    const row = screen.getByTestId('processing-job-row-job-flap')
    expect(row.textContent).toMatch(/Retrying Zoom import/i)
    expect(screen.getByTestId('processing-job-row-job-flap-error').textContent).toBe('Zoom API 502')
  })

  it('renders nothing when there are no videos and no processing_jobs', () => {
    const { container } = render(
      <MaterialsTreePanel
        session={baseSession({ video_sources: [], processing_jobs: [] })}
        apiBaseUrl=""
      />
    )
    expect(container.querySelector('[data-testid^="processing-job-row-"]')).toBeNull()
  })
})
