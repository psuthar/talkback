import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import styles from './SpreadsheetViewer.module.css'

/**
 * SpreadsheetViewer (SCRUM-333) — renders the markdown that the markitdown
 * sidecar produces from CSV / XLS / XLSX uploads (SCRUM-331 / SCRUM-332).
 *
 * Input shape: `markdown` is a string that may contain GitHub-flavored
 * markdown tables and `## SheetName` headings (one per sheet for
 * multi-sheet XLSX). For CSV the input is a single table; for XLSX the
 * input is a sequence of `## SheetName` headings each followed by a
 * table. This component renders both shapes correctly via react-markdown
 * + remark-gfm.
 *
 * UX choices for v1 (Notes from SCRUM-333):
 * - Sectioned headings (## SheetName) for multi-sheet XLSX. Tabs are a
 *   future improvement; for v1 the user scrolls vertically through
 *   stacked sections.
 * - Sticky thead per table so the header row stays visible while scrolling.
 * - Wide-table horizontal scroll: each table is wrapped in a scrolling
 *   container so wide content scrolls within the pane rather than
 *   overflowing the page.
 * - Cell-content truncation deferred — react-markdown's default rendering
 *   wraps long text within a cell. Add ellipsis + title tooltips later
 *   if real-world data shows wide cells creating excessive height.
 */
export function SpreadsheetViewer({ markdown }) {
	const text = (markdown || '').trim()
	if (!text) {
		return (
			<div data-testid="spreadsheet-viewer-empty" className={styles.empty}>
				No extracted content. The spreadsheet may still be processing or it may be empty.
			</div>
		)
	}

	return (
		<div data-testid="spreadsheet-viewer" className={styles.viewer}>
			<ReactMarkdown
				remarkPlugins={[remarkGfm]}
				components={{
					// Wrap each <table> in a scrolling container so wide content
					// horizontal-scrolls *within* the cell rather than the page.
					table: ({ node, ...props }) => (
						<div className={styles.tableScroll}>
							<table {...props} className={styles.table} />
						</div>
					),
					thead: ({ node, ...props }) => <thead {...props} className={styles.thead} />,
					th: ({ node, ...props }) => <th {...props} className={styles.th} />,
					td: ({ node, ...props }) => <td {...props} className={styles.td} />,
					h2: ({ node, ...props }) => <h2 {...props} className={styles.sheetHeading} />,
				}}
			>
				{text}
			</ReactMarkdown>
		</div>
	)
}

export default SpreadsheetViewer
