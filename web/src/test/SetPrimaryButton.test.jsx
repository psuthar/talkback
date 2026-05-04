import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { SessionPrimaryRow } from '../components/SetPrimaryButton'

vi.mock('../api/sessionPrimary', async () => {
	const actual = await vi.importActual('../api/sessionPrimary')
	return {
		...actual,
		patchSessionPrimary: vi.fn(),
	}
})

import { patchSessionPrimary, SessionPrimaryError } from '../api/sessionPrimary'

// Minimal harness: renders a focusable row that consumes SessionPrimaryRow's
// render-props. Mirrors how MaterialsTreePanel composes the wrapper around a
// row's outer container (right-click + keyboard fire on the row, badge sits
// inline inside it).
function Harness(props) {
	return (
		<SessionPrimaryRow {...props}>
			{({ badge, rowHandlers, menuNode, errorNode }) => (
				<>
					<div data-testid="row" tabIndex={0} {...rowHandlers}>
						<button data-testid="row-btn" type="button">item</button>
						{badge && <span data-testid="badge-slot">{badge}</span>}
					</div>
					{menuNode}
					{errorNode}
				</>
			)}
		</SessionPrimaryRow>
	)
}

describe('SessionPrimaryRow (SCRUM-294)', () => {
	it('renders the inline Primary badge when isCurrentPrimary, with role=status + aria-label', () => {
		render(
			<Harness
				apiBaseUrl=""
				sessionId="s1"
				kind="document"
				id="m1"
				isCurrentPrimary
				onSuccess={vi.fn()}
				itemLabel="Spec.pdf"
			/>,
		)
		const badge = screen.getByTestId('primary-badge')
		expect(badge.getAttribute('role')).toBe('status')
		expect(badge.getAttribute('aria-label')).toBe('Session primary (Spec.pdf)')
	})

	it('renders no badge and no menu when not primary and not eligible to set', () => {
		render(
			<Harness
				apiBaseUrl=""
				sessionId="s1"
				kind="document"
				id="m1"
				isCurrentPrimary={false}
				canSetPrimary={false}
				onSuccess={vi.fn()}
			/>,
		)
		expect(screen.queryByTestId('primary-badge')).toBeNull()
		expect(screen.queryByTestId('primary-context-menu')).toBeNull()
		fireEvent.contextMenu(screen.getByTestId('row'))
		expect(screen.queryByTestId('primary-context-menu')).toBeNull()
	})

	it('right-click on a non-primary eligible row opens menu with Make primary; click PATCHes {kind,id}', async () => {
		patchSessionPrimary.mockResolvedValueOnce(undefined)
		const onSuccess = vi.fn()
		render(
			<Harness
				apiBaseUrl="https://api"
				sessionId="s1"
				kind="document"
				id="m1"
				canSetPrimary
				onSuccess={onSuccess}
				itemLabel="Spec.pdf"
			/>,
		)
		const row = screen.getByTestId('row')
		expect(screen.queryByTestId('primary-context-menu')).toBeNull()

		fireEvent.contextMenu(row)
		const menu = screen.getByTestId('primary-context-menu')
		expect(menu.getAttribute('role')).toBe('menu')

		const make = screen.getByTestId('make-primary-btn')
		expect(make.getAttribute('role')).toBe('menuitem')
		expect(make.getAttribute('aria-label')).toBe('Make session primary (Spec.pdf)')

		fireEvent.click(make)
		await waitFor(() => expect(patchSessionPrimary).toHaveBeenCalled())
		expect(patchSessionPrimary).toHaveBeenCalledWith('https://api', 's1', { kind: 'document', id: 'm1' })
		expect(onSuccess).toHaveBeenCalled()
		// Menu auto-closes after success.
		await waitFor(() => expect(screen.queryByTestId('primary-context-menu')).toBeNull())
	})

	it('right-click on a primary row opens menu with Clear primary; click PATCHes {kind:""}', async () => {
		patchSessionPrimary.mockResolvedValueOnce(undefined)
		const onSuccess = vi.fn()
		render(
			<Harness
				apiBaseUrl=""
				sessionId="s1"
				kind="document"
				id="m1"
				isCurrentPrimary
				onSuccess={onSuccess}
				itemLabel="Spec.pdf"
			/>,
		)
		fireEvent.contextMenu(screen.getByTestId('row'))
		const clear = screen.getByTestId('clear-primary-btn')
		expect(clear.getAttribute('aria-label')).toBe('Clear session primary (Spec.pdf)')
		fireEvent.click(clear)
		await waitFor(() => expect(patchSessionPrimary).toHaveBeenCalled())
		expect(patchSessionPrimary).toHaveBeenCalledWith('', 's1', { kind: '' })
		expect(onSuccess).toHaveBeenCalled()
	})

	it('Shift+F10 on a focused row opens the menu (keyboard parity, SCRUM-290 carry-forward)', () => {
		render(
			<Harness
				apiBaseUrl=""
				sessionId="s1"
				kind="document"
				id="m1"
				canSetPrimary
				onSuccess={vi.fn()}
			/>,
		)
		fireEvent.keyDown(screen.getByTestId('row'), { key: 'F10', shiftKey: true })
		expect(screen.getByTestId('primary-context-menu')).toBeInTheDocument()
		expect(screen.getByTestId('make-primary-btn')).toBeInTheDocument()
	})

	it('ContextMenu key on a focused row opens the menu', () => {
		render(
			<Harness
				apiBaseUrl=""
				sessionId="s1"
				kind="document"
				id="m1"
				isCurrentPrimary
				onSuccess={vi.fn()}
			/>,
		)
		fireEvent.keyDown(screen.getByTestId('row'), { key: 'ContextMenu' })
		expect(screen.getByTestId('primary-context-menu')).toBeInTheDocument()
		expect(screen.getByTestId('clear-primary-btn')).toBeInTheDocument()
	})

	it('Escape closes the open menu', () => {
		render(
			<Harness
				apiBaseUrl=""
				sessionId="s1"
				kind="document"
				id="m1"
				canSetPrimary
				onSuccess={vi.fn()}
			/>,
		)
		fireEvent.contextMenu(screen.getByTestId('row'))
		expect(screen.getByTestId('primary-context-menu')).toBeInTheDocument()
		fireEvent.keyDown(document, { key: 'Escape' })
		expect(screen.queryByTestId('primary-context-menu')).toBeNull()
	})

	it('outside click closes the open menu without firing PATCH', () => {
		patchSessionPrimary.mockClear()
		render(
			<>
				<div data-testid="outside">elsewhere</div>
				<Harness
					apiBaseUrl=""
					sessionId="s1"
					kind="document"
					id="m1"
					canSetPrimary
					onSuccess={vi.fn()}
				/>
			</>,
		)
		fireEvent.contextMenu(screen.getByTestId('row'))
		expect(screen.getByTestId('primary-context-menu')).toBeInTheDocument()
		fireEvent.mouseDown(screen.getByTestId('outside'))
		expect(screen.queryByTestId('primary-context-menu')).toBeNull()
		expect(patchSessionPrimary).not.toHaveBeenCalled()
	})

	it('read-only context (isCurrentPrimary, no onSuccess) renders badge but no menu on right-click', () => {
		render(
			<Harness
				apiBaseUrl=""
				sessionId="s1"
				kind="document"
				id="m1"
				isCurrentPrimary
			/>,
		)
		expect(screen.getByTestId('primary-badge')).toBeInTheDocument()
		fireEvent.contextMenu(screen.getByTestId('row'))
		expect(screen.queryByTestId('primary-context-menu')).toBeNull()
	})

	it('error region uses role=alert + aria-live=polite and shows mapped copy', async () => {
		patchSessionPrimary.mockRejectedValueOnce(
			new SessionPrimaryError(400, 'PRIMARY_NOT_READY', 'still processing'),
		)
		render(
			<Harness
				apiBaseUrl=""
				sessionId="s1"
				kind="document"
				id="m1"
				canSetPrimary
				onSuccess={vi.fn()}
			/>,
		)
		fireEvent.contextMenu(screen.getByTestId('row'))
		fireEvent.click(screen.getByTestId('make-primary-btn'))
		await waitFor(() => expect(screen.getByTestId('set-primary-error')).toBeInTheDocument())
		const errEl = screen.getByTestId('set-primary-error')
		expect(errEl.getAttribute('role')).toBe('alert')
		expect(errEl.getAttribute('aria-live')).toBe('polite')
		expect(errEl.textContent).toMatch(/still processing/i)
	})

	it('renders mapped copy on PRIMARY_MATERIAL_DELETED', async () => {
		patchSessionPrimary.mockRejectedValueOnce(
			new SessionPrimaryError(400, 'PRIMARY_MATERIAL_DELETED', 'material has been deleted'),
		)
		render(
			<Harness
				apiBaseUrl=""
				sessionId="s1"
				kind="document"
				id="m1"
				canSetPrimary
				onSuccess={vi.fn()}
			/>,
		)
		fireEvent.contextMenu(screen.getByTestId('row'))
		fireEvent.click(screen.getByTestId('make-primary-btn'))
		await waitFor(() => expect(screen.getByTestId('set-primary-error')).toBeInTheDocument())
		expect(screen.getByTestId('set-primary-error').textContent).toMatch(/deleted/i)
	})

	it('shows the 403 message when the API returns 403', async () => {
		patchSessionPrimary.mockRejectedValueOnce(new SessionPrimaryError(403, '', 'forbidden'))
		render(
			<Harness
				apiBaseUrl=""
				sessionId="s1"
				kind="document"
				id="m1"
				canSetPrimary
				onSuccess={vi.fn()}
			/>,
		)
		fireEvent.contextMenu(screen.getByTestId('row'))
		fireEvent.click(screen.getByTestId('make-primary-btn'))
		await waitFor(() => expect(screen.getByTestId('set-primary-error')).toBeInTheDocument())
		expect(screen.getByTestId('set-primary-error').textContent).toMatch(/Only the session creator/i)
	})

	it('falls back to a generic network message on non-API errors', async () => {
		patchSessionPrimary.mockRejectedValueOnce(new Error('boom'))
		render(
			<Harness
				apiBaseUrl=""
				sessionId="s1"
				kind="document"
				id="m1"
				canSetPrimary
				onSuccess={vi.fn()}
			/>,
		)
		fireEvent.contextMenu(screen.getByTestId('row'))
		fireEvent.click(screen.getByTestId('make-primary-btn'))
		await waitFor(() => expect(screen.getByTestId('set-primary-error')).toBeInTheDocument())
		expect(screen.getByTestId('set-primary-error').textContent).toMatch(/Network error/i)
	})

	it('disables the menu item while pending so duplicate calls are prevented', async () => {
		let resolve
		patchSessionPrimary.mockImplementationOnce(() => new Promise((r) => { resolve = r }))
		render(
			<Harness
				apiBaseUrl=""
				sessionId="s1"
				kind="document"
				id="m1"
				canSetPrimary
				onSuccess={vi.fn()}
			/>,
		)
		fireEvent.contextMenu(screen.getByTestId('row'))
		const make = screen.getByTestId('make-primary-btn')
		fireEvent.click(make)
		expect(make).toBeDisabled()
		expect(make.textContent).toMatch(/Setting/i)
		resolve({ ok: true })
		await waitFor(() => expect(screen.queryByTestId('primary-context-menu')).toBeNull())
	})
})
