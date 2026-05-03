import React, { useState } from 'react'
import { patchSessionPrimary, SessionPrimaryError } from '../api/sessionPrimary'

/**
 * SetPrimaryButton — creator-only affordance to anchor the session's
 * center pane on a specific material/link/video (SCRUM-275; clear control
 * + a11y added in SCRUM-285).
 *
 * Renders one of:
 *   - "Primary" badge + "Clear" button when this row is the session's primary
 *     AND the parent provided onSuccess (i.e. caller is a creator surface).
 *     Click "Clear" PATCHes {kind:""} to remove the explicit primary; the
 *     SCRUM-271 resolver then falls back to the legacy video-first behavior.
 *   - "Primary" badge only (no clear) when isCurrentPrimary but onSuccess is
 *     omitted — used by read-only surfaces.
 *   - "Make primary" button otherwise. Click calls the SCRUM-272 PATCH;
 *     on success calls onSuccess so the parent can refetch; on failure
 *     shows an inline error keyed off the API's structured `code` field.
 *
 * A11y:
 *   - The Make-primary and Clear buttons carry aria-labels that include the
 *     kind (and item title via the new `itemLabel` prop) so screen-reader
 *     announcements are unambiguous when many rows share the same visible
 *     copy.
 *   - The badge sets role="status" so SR users hear the primary state.
 *   - The inline error region is aria-live="polite" so reactive errors are
 *     announced without stealing focus.
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
	itemLabel,
}) {
	const [pending, setPending] = useState(false)
	const [error, setError] = useState(null)

	const labelSuffix = itemLabel ? ` (${itemLabel})` : ''
	const ariaSetLabel = `Make session primary${labelSuffix}`
	const ariaClearLabel = `Clear session primary${labelSuffix}`

	const patchPrimary = async (body) => {
		setPending(true)
		setError(null)
		try {
			await patchSessionPrimary(apiBaseUrl, sessionId, body)
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

	if (isCurrentPrimary) {
		return (
			<span style={{ display: 'inline-flex', alignItems: 'center', gap: 6, ...style }}>
				<span
					data-testid="primary-badge"
					role="status"
					aria-label={`Session primary${labelSuffix}`}
					style={{
						fontSize: 11,
						padding: '2px 6px',
						marginLeft: 6,
						borderRadius: 3,
						backgroundColor: 'var(--color-primary-mid, #1976d2)',
						color: 'white',
					}}
				>
					Primary
				</span>
				{onSuccess && (
					<button
						type="button"
						data-testid="clear-primary-btn"
						aria-label={ariaClearLabel}
						onClick={() => patchPrimary({ kind: '' })}
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
						{pending ? 'Clearing…' : 'Clear'}
					</button>
				)}
				{error && (
					<span
						data-testid="set-primary-error"
						role="alert"
						aria-live="polite"
						style={{ fontSize: 11, color: 'var(--color-danger-dark, #b00020)' }}
					>
						{error.message}
					</span>
				)}
			</span>
		)
	}

	return (
		<span style={{ display: 'inline-flex', alignItems: 'center', gap: 6, ...style }}>
			<button
				type="button"
				data-testid="set-primary-btn"
				aria-label={ariaSetLabel}
				onClick={() => patchPrimary({ kind, id })}
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
				<span
					data-testid="set-primary-error"
					role="alert"
					aria-live="polite"
					style={{ fontSize: 11, color: 'var(--color-danger-dark, #b00020)' }}
				>
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
