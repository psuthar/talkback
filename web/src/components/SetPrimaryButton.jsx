import React, { useState } from 'react'
import { patchSessionPrimary, SessionPrimaryError } from '../api/sessionPrimary'

/**
 * SetPrimaryButton — creator-only affordance to anchor the session's
 * center pane on a specific material/link/video (SCRUM-275).
 *
 * Renders one of:
 *   - "Primary" (disabled badge) when this row is already the session's primary.
 *   - "Make primary" (button) otherwise. Click calls the SCRUM-272 PATCH
 *     endpoint via patchSessionPrimary; on success calls onSuccess so the
 *     parent can refetch the session; on failure shows an inline error
 *     message keyed off the API's structured `code` field.
 *
 * Acceptance-criteria mapping (SCRUM-275):
 *   - "Invalid selections show a recoverable error without corrupting
 *     session state" — errors are caught, displayed inline, and the parent
 *     state is not mutated until onSuccess fires.
 *   - "Non-creators cannot change primary" — handler-side ACL (SCRUM-272)
 *     returns 403; this button surfaces it via the same error path. Callers
 *     should also gate visibility on a creator-mode flag.
 */
export function SetPrimaryButton({
	apiBaseUrl,
	sessionId,
	kind,
	id,
	isCurrentPrimary = false,
	onSuccess,
	disabled = false,
	style,
}) {
	const [pending, setPending] = useState(false)
	const [error, setError] = useState(null)

	if (isCurrentPrimary) {
		return (
			<span
				data-testid="primary-badge"
				style={{
					fontSize: 11,
					padding: '2px 6px',
					marginLeft: 6,
					borderRadius: 3,
					backgroundColor: 'var(--color-primary-mid, #1976d2)',
					color: 'white',
					...style,
				}}
			>
				Primary
			</span>
		)
	}

	const onClick = async () => {
		setPending(true)
		setError(null)
		try {
			await patchSessionPrimary(apiBaseUrl, sessionId, { kind, id })
			onSuccess?.()
		} catch (e) {
			if (e instanceof SessionPrimaryError) {
				setError({ code: e.code, message: messageForCode(e) })
			} else {
				setError({ code: 'NETWORK', message: 'Network error — please try again.' })
			}
		} finally {
			setPending(false)
		}
	}

	return (
		<span style={{ display: 'inline-flex', alignItems: 'center', gap: 6, ...style }}>
			<button
				type="button"
				data-testid="set-primary-btn"
				onClick={onClick}
				disabled={pending || disabled}
				style={{
					fontSize: 11,
					color: '#666',
					background: 'none',
					border: 'none',
					padding: 0,
					cursor: pending || disabled ? 'not-allowed' : 'pointer',
					textDecoration: 'underline',
				}}
			>
				{pending ? 'Setting…' : 'Make primary'}
			</button>
			{error && (
				<span data-testid="set-primary-error" style={{ fontSize: 11, color: 'var(--color-danger-dark, #b00020)' }}>
					{error.message}
				</span>
			)}
		</span>
	)
}

// Map the SCRUM-272 error codes to recoverable user-facing copy. Unknown
// codes fall back to the API's free-form message (already present on the
// SessionPrimaryError instance).
function messageForCode(err) {
	switch (err.code) {
		case 'PRIMARY_MATERIAL_DELETED': return 'This material was deleted. Pick another.'
		case 'PRIMARY_MATERIAL_NOT_FOUND':
		case 'PRIMARY_VIDEO_NOT_FOUND':
		case 'PRIMARY_LINK_NOT_FOUND':
			return 'Could not find that item — it may have been removed.'
		case 'PRIMARY_MATERIAL_WRONG_SESSION':
		case 'PRIMARY_VIDEO_WRONG_SESSION':
		case 'PRIMARY_LINK_WRONG_SESSION':
			return 'That item belongs to a different session.'
		case 'PRIMARY_BAD_KIND': return 'Unsupported primary type.'
		case 'PRIMARY_MISSING_ID': return 'Missing target — please retry.'
		case 'PRIMARY_NOT_READY': return 'This item is still processing — try again once it finishes.'
		default:
			if (err.status === 403) return 'Only the session creator can change the primary.'
			return err.message || 'Could not update primary.'
	}
}

export default SetPrimaryButton
