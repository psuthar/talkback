import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { SetPrimaryButton } from '../components/SetPrimaryButton'

vi.mock('../api/sessionPrimary', async () => {
	const actual = await vi.importActual('../api/sessionPrimary')
	return {
		...actual,
		patchSessionPrimary: vi.fn(),
	}
})

import { patchSessionPrimary, SessionPrimaryError } from '../api/sessionPrimary'

describe('SetPrimaryButton (SCRUM-275)', () => {
	it('renders Primary badge when row is the current primary (no button)', () => {
		render(
			<SetPrimaryButton
				apiBaseUrl=""
				sessionId="s1"
				kind="document"
				id="m1"
				isCurrentPrimary={true}
			/>,
		)
		expect(screen.getByTestId('primary-badge')).toBeInTheDocument()
		expect(screen.queryByTestId('set-primary-btn')).toBeNull()
	})

	it('calls onSuccess and the API on Make-primary click', async () => {
		patchSessionPrimary.mockResolvedValueOnce({ ok: true })
		const onSuccess = vi.fn()
		render(
			<SetPrimaryButton
				apiBaseUrl="https://api"
				sessionId="s1"
				kind="document"
				id="m1"
				onSuccess={onSuccess}
			/>,
		)
		fireEvent.click(screen.getByTestId('set-primary-btn'))
		await waitFor(() => expect(onSuccess).toHaveBeenCalledTimes(1))
		expect(patchSessionPrimary).toHaveBeenCalledWith(
			'https://api',
			's1',
			{ kind: 'document', id: 'm1' },
		)
	})

	it('renders inline error with mapped copy on PRIMARY_MATERIAL_DELETED', async () => {
		patchSessionPrimary.mockRejectedValueOnce(
			new SessionPrimaryError(400, 'PRIMARY_MATERIAL_DELETED', 'material has been deleted'),
		)
		render(
			<SetPrimaryButton
				apiBaseUrl=""
				sessionId="s1"
				kind="document"
				id="m1"
			/>,
		)
		fireEvent.click(screen.getByTestId('set-primary-btn'))
		await waitFor(() => expect(screen.getByTestId('set-primary-error')).toBeInTheDocument())
		expect(screen.getByTestId('set-primary-error').textContent).toMatch(/deleted/i)
	})

	it('renders mapped copy on PRIMARY_NOT_READY (SCRUM-281)', async () => {
		patchSessionPrimary.mockRejectedValueOnce(
			new SessionPrimaryError(400, 'PRIMARY_NOT_READY', 'primary material is not ready (text_status=processing)'),
		)
		render(
			<SetPrimaryButton
				apiBaseUrl=""
				sessionId="s1"
				kind="document"
				id="m1"
			/>,
		)
		fireEvent.click(screen.getByTestId('set-primary-btn'))
		await waitFor(() => expect(screen.getByTestId('set-primary-error')).toBeInTheDocument())
		expect(screen.getByTestId('set-primary-error').textContent).toMatch(/still processing/i)
	})

	it('shows the 403 message when the API returns 403', async () => {
		patchSessionPrimary.mockRejectedValueOnce(
			new SessionPrimaryError(403, '', 'forbidden'),
		)
		render(<SetPrimaryButton apiBaseUrl="" sessionId="s1" kind="document" id="m1" />)
		fireEvent.click(screen.getByTestId('set-primary-btn'))
		await waitFor(() => expect(screen.getByTestId('set-primary-error')).toBeInTheDocument())
		expect(screen.getByTestId('set-primary-error').textContent).toMatch(/Only the session creator/i)
	})

	it('falls back to a generic network message on non-API errors', async () => {
		patchSessionPrimary.mockRejectedValueOnce(new Error('boom'))
		render(<SetPrimaryButton apiBaseUrl="" sessionId="s1" kind="document" id="m1" />)
		fireEvent.click(screen.getByTestId('set-primary-btn'))
		await waitFor(() => expect(screen.getByTestId('set-primary-error')).toBeInTheDocument())
		expect(screen.getByTestId('set-primary-error').textContent).toMatch(/Network error/i)
	})

	it('disables the button while pending so duplicate calls are prevented', async () => {
		let resolve
		patchSessionPrimary.mockImplementationOnce(() => new Promise((r) => { resolve = r }))
		render(<SetPrimaryButton apiBaseUrl="" sessionId="s1" kind="document" id="m1" />)
		const btn = screen.getByTestId('set-primary-btn')
		fireEvent.click(btn)
		expect(btn).toBeDisabled()
		expect(btn.textContent).toMatch(/Setting/i)
		resolve({ ok: true })
		await waitFor(() => expect(screen.getByTestId('set-primary-btn')).not.toBeDisabled())
	})
})
