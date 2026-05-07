import { describe, it, expect, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { DocumentViewer } from '../components/DocumentViewer'

// SCRUM-333 — DocumentViewer dispatches CSV / XLS / XLSX materials with
// extracted_text (markdown) to the lazy-loaded SpreadsheetViewer instead
// of the legacy pre-wrap text fallback. These tests pin the dispatch
// matrix so a future tweak to the detector or render switch doesn't
// silently route around the renderer.

// SlideDeckViewer is async-fetch-heavy; not in scope for these tests.
vi.mock('../components/SlideDeckViewer', () => ({
	SlideDeckViewer: ({ materialId }) => <div data-testid="slide-deck-viewer-stub">slide-stub:{materialId}</div>,
}))

const csvMarkdown = '| name | role |\n| --- | --- |\n| Alex | Eng |'

function makeDoc(overrides = {}) {
	return {
		id: 'mat-1',
		artifact_id: 'art-1',
		filename: 'customers.csv',
		content_type: 'text/csv',
		extracted_text: csvMarkdown,
		...overrides,
	}
}

describe('DocumentViewer — SCRUM-333 spreadsheet dispatch', () => {
	it('routes a CSV with markdown extracted_text to SpreadsheetViewer', async () => {
		render(<DocumentViewer doc={makeDoc()} apiBaseUrl="" sessionId="s1" />)
		await waitFor(() => {
			expect(screen.getByTestId('spreadsheet-viewer')).toBeInTheDocument()
		})
		// Confirm the table actually rendered in the dispatched component:
		const viewer = screen.getByTestId('spreadsheet-viewer')
		expect(viewer.querySelector('table')).toBeTruthy()
	})

	it('routes XLSX by content_type even if filename suffix is missing', async () => {
		render(
			<DocumentViewer
				doc={makeDoc({
					filename: 'noext',
					content_type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
				})}
				apiBaseUrl=""
				sessionId="s1"
			/>,
		)
		await waitFor(() => {
			expect(screen.getByTestId('spreadsheet-viewer')).toBeInTheDocument()
		})
	})

	it('routes XLS by content_type', async () => {
		render(
			<DocumentViewer
				doc={makeDoc({ filename: 'legacy.xls', content_type: 'application/vnd.ms-excel' })}
				apiBaseUrl=""
				sessionId="s1"
			/>,
		)
		await waitFor(() => {
			expect(screen.getByTestId('spreadsheet-viewer')).toBeInTheDocument()
		})
	})

	it('does NOT route a PDF to SpreadsheetViewer', () => {
		render(
			<DocumentViewer
				doc={makeDoc({ filename: 'doc.pdf', content_type: 'application/pdf', extracted_text: 'unrelated text' })}
				apiBaseUrl=""
				sessionId="s1"
			/>,
		)
		expect(screen.queryByTestId('spreadsheet-viewer')).toBeNull()
	})

	it('does NOT route a DOCX to SpreadsheetViewer', () => {
		render(
			<DocumentViewer
				doc={makeDoc({
					filename: 'memo.docx',
					content_type: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
					extracted_text: 'plain text from docx',
				})}
				apiBaseUrl=""
				sessionId="s1"
			/>,
		)
		expect(screen.queryByTestId('spreadsheet-viewer')).toBeNull()
	})

	it('does NOT route an image to SpreadsheetViewer', () => {
		render(
			<DocumentViewer
				doc={makeDoc({ filename: 'shot.png', content_type: 'image/png', extracted_text: '# caption' })}
				apiBaseUrl=""
				sessionId="s1"
			/>,
		)
		expect(screen.queryByTestId('spreadsheet-viewer')).toBeNull()
	})

	it('falls back to text when CSV has no extracted_text yet (still pending)', () => {
		render(
			<DocumentViewer
				doc={makeDoc({ extracted_text: '' })}
				apiBaseUrl=""
				sessionId="s1"
			/>,
		)
		// Pending: SpreadsheetViewer is not rendered (because extracted_text is empty).
		expect(screen.queryByTestId('spreadsheet-viewer')).toBeNull()
		// The legacy "no extracted text" fallback covers the empty state for now.
	})
})
