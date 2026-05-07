import { describe, it, expect } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import { SpreadsheetViewer } from '../components/SpreadsheetViewer'

// SCRUM-333 — SpreadsheetViewer renders the markdown that the markitdown
// sidecar emits for CSV / XLS / XLSX. These tests pin the rendered shape
// (HTML <table>, sticky <thead>, ## section headings) so a future change
// to the renderer doesn't silently regress the user-facing UI.

describe('SpreadsheetViewer', () => {
	const csvMarkdown = '| name | role |\n| --- | --- |\n| Alex | Eng |\n| Sara | PM |'

	it('renders a CSV-style markdown table as <table> with header + body rows', () => {
		const { container } = render(<SpreadsheetViewer markdown={csvMarkdown} />)
		const viewer = screen.getByTestId('spreadsheet-viewer')
		const table = container.querySelector('table')
		expect(table).toBeInTheDocument()
		// Header cells:
		expect(within(table).getByText('name')).toBeInTheDocument()
		expect(within(table).getByText('role')).toBeInTheDocument()
		// Body cells:
		expect(within(table).getByText('Alex')).toBeInTheDocument()
		expect(within(table).getByText('Eng')).toBeInTheDocument()
		expect(within(table).getByText('Sara')).toBeInTheDocument()
		expect(within(table).getByText('PM')).toBeInTheDocument()
		// Two body rows:
		const bodyRows = table.querySelectorAll('tbody tr')
		expect(bodyRows).toHaveLength(2)
		// thead + tbody both present:
		expect(table.querySelector('thead')).toBeInTheDocument()
		expect(table.querySelector('tbody')).toBeInTheDocument()
		expect(viewer).toContainElement(table)
	})

	it('renders multi-sheet XLSX-style markdown with two ## headings + two tables', () => {
		const xlsxMarkdown = [
			'## Sheet1',
			'',
			'| a | b |',
			'| --- | --- |',
			'| 1 | 2 |',
			'',
			'## Sheet2',
			'',
			'| x | y |',
			'| --- | --- |',
			'| 3 | 4 |',
		].join('\n')

		const { container } = render(<SpreadsheetViewer markdown={xlsxMarkdown} />)

		// Two h2 headings:
		const h2s = container.querySelectorAll('h2')
		expect(h2s).toHaveLength(2)
		expect(h2s[0].textContent).toContain('Sheet1')
		expect(h2s[1].textContent).toContain('Sheet2')

		// Two tables stacked:
		const tables = container.querySelectorAll('table')
		expect(tables).toHaveLength(2)
		expect(within(tables[0]).getByText('a')).toBeInTheDocument()
		expect(within(tables[0]).getByText('b')).toBeInTheDocument()
		expect(within(tables[0]).getByText('1')).toBeInTheDocument()
		expect(within(tables[1]).getByText('x')).toBeInTheDocument()
		expect(within(tables[1]).getByText('y')).toBeInTheDocument()
	})

	it('shows a graceful empty state when markdown is empty / whitespace', () => {
		render(<SpreadsheetViewer markdown="" />)
		expect(screen.getByTestId('spreadsheet-viewer-empty')).toBeInTheDocument()
		expect(screen.queryByTestId('spreadsheet-viewer')).toBeNull()
	})

	it('treats whitespace-only markdown as empty', () => {
		// JSX attributes do NOT interpret \n escape sequences; pass via {} so the
		// runtime string contains real whitespace characters and exercises the
		// component's `.trim()` early-return.
		render(<SpreadsheetViewer markdown={'   \n\n  '} />)
		expect(screen.getByTestId('spreadsheet-viewer-empty')).toBeInTheDocument()
	})

	it('treats null / undefined markdown as empty without throwing', () => {
		render(<SpreadsheetViewer markdown={null} />)
		expect(screen.getByTestId('spreadsheet-viewer-empty')).toBeInTheDocument()
	})

	it('renders non-table markdown content gracefully (paragraphs, no table)', () => {
		// Adversarial input: the sidecar produced something the user could
		// paste back into the chat, but it has no markdown table syntax.
		const text = 'This file appears to be empty.\n\nNo rows were detected.'
		const { container } = render(<SpreadsheetViewer markdown={text} />)
		expect(screen.getByTestId('spreadsheet-viewer')).toBeInTheDocument()
		// No table:
		expect(container.querySelector('table')).toBeNull()
		// Paragraph text rendered:
		expect(screen.getByText(/This file appears to be empty/i)).toBeInTheDocument()
	})

	it('preserves inline links inside table cells (remark-gfm output)', () => {
		const md = '| label | href |\n| --- | --- |\n| Site | [example](https://example.com) |'
		render(<SpreadsheetViewer markdown={md} />)
		const link = screen.getByRole('link', { name: 'example' })
		expect(link).toHaveAttribute('href', 'https://example.com')
	})
})
