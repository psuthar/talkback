import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { SessionMaterialsTab } from '../components/SessionMaterialsTab'

// SCRUM-334 — when the materials/upload endpoint returns HTTP 413 with a
// structured spreadsheet_too_large body, SessionMaterialsTab renders a
// friendly inline error showing both the configured cap and the file's
// actual size. These tests stub global fetch with the exact response
// shape the Go handler returns.

const SESSION_ID = 'sess-1'

function tooLargeResponse({ maxBytes, actualBytes }) {
	return new Response(
		JSON.stringify({ error: 'spreadsheet_too_large', max_bytes: maxBytes, actual_bytes: actualBytes }),
		{ status: 413, headers: { 'Content-Type': 'application/json' } },
	)
}

function plainErrorResponse(text, status = 500) {
	return new Response(text, { status, headers: { 'Content-Type': 'text/plain' } })
}

describe('SessionMaterialsTab — SCRUM-334 spreadsheet too-large error UX', () => {
	const fetchMock = vi.fn()
	const originalFetch = globalThis.fetch

	beforeEach(() => {
		fetchMock.mockReset()
		globalThis.fetch = fetchMock
	})

	afterEach(() => {
		globalThis.fetch = originalFetch
	})

	it('shows a friendly inline error for a 413 spreadsheet_too_large response', async () => {
		fetchMock.mockResolvedValueOnce(tooLargeResponse({ maxBytes: 5 * 1024 * 1024, actualBytes: 6 * 1024 * 1024 }))

		const onError = vi.fn()
		render(
			<SessionMaterialsTab
				sessionId={SESSION_ID}
				materials={[]}
				videoSources={[]}
				apiBaseUrl=""
				refetchSession={vi.fn()}
				onUploading={vi.fn()}
				onUploadDone={vi.fn()}
				onError={onError}
			/>,
		)

		// Find the hidden file input (the styled label wraps it). Use
		// role-less query because the input is display:none.
		const fileInput = document.querySelector('input[type="file"]')
		expect(fileInput).toBeTruthy()

		const file = new File(['huge-spreadsheet'], 'customers.csv', { type: 'text/csv' })
		Object.defineProperty(file, 'size', { value: 6 * 1024 * 1024, configurable: false })
		fireEvent.change(fileInput, { target: { files: [file] } })

		await waitFor(() => {
			expect(fetchMock).toHaveBeenCalled()
		})

		await waitFor(() => {
			expect(screen.getByText(/spreadsheet too large/i)).toBeInTheDocument()
		})
		const message = screen.getByText(/spreadsheet too large/i).textContent
		expect(message).toMatch(/customers\.csv/i)
		expect(message).toMatch(/max 5\.0 MB/i)
		expect(message).toMatch(/this file is 6\.0 MB/i)

		// onError prop is also called with the same string for parent-side handling.
		expect(onError).toHaveBeenCalled()
		const errMsg = onError.mock.calls[0][0]
		expect(errMsg).toMatch(/spreadsheet too large/i)
	})

	it('falls back to plain-text error for non-413 responses', async () => {
		fetchMock.mockResolvedValueOnce(plainErrorResponse('Internal server error', 500))

		const onError = vi.fn()
		render(
			<SessionMaterialsTab
				sessionId={SESSION_ID}
				materials={[]}
				videoSources={[]}
				apiBaseUrl=""
				refetchSession={vi.fn()}
				onUploading={vi.fn()}
				onUploadDone={vi.fn()}
				onError={onError}
			/>,
		)

		const fileInput = document.querySelector('input[type="file"]')
		const file = new File(['x'], 'doc.pdf', { type: 'application/pdf' })
		fireEvent.change(fileInput, { target: { files: [file] } })

		await waitFor(() => {
			expect(screen.getByText(/Internal server error/i)).toBeInTheDocument()
		})
		// Should NOT include the spreadsheet-specific copy:
		expect(screen.queryByText(/spreadsheet too large/i)).toBeNull()
	})

	it('falls back to plain text on a 413 with a non-spreadsheet error code', async () => {
		// Defensive: a 413 from some other layer (proxy, multipart cap) shouldn't
		// be misrendered as the spreadsheet-specific message.
		fetchMock.mockResolvedValueOnce(
			new Response(JSON.stringify({ error: 'request_too_large', max_bytes: 1000 }), {
				status: 413,
				headers: { 'Content-Type': 'application/json' },
			}),
		)

		render(
			<SessionMaterialsTab
				sessionId={SESSION_ID}
				materials={[]}
				videoSources={[]}
				apiBaseUrl=""
				refetchSession={vi.fn()}
				onUploading={vi.fn()}
				onUploadDone={vi.fn()}
			/>,
		)

		const fileInput = document.querySelector('input[type="file"]')
		const file = new File(['x'], 'thing.csv', { type: 'text/csv' })
		fireEvent.change(fileInput, { target: { files: [file] } })

		await waitFor(() => {
			expect(fetchMock).toHaveBeenCalled()
		})
		// The error UI shows whatever text fell through — not the friendly copy.
		expect(screen.queryByText(/spreadsheet too large/i)).toBeNull()
	})
})
