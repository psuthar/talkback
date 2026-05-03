import React, { useEffect, useRef, useState } from 'react'
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
			<PrimaryBadgeWithContextMenu
				labelSuffix={labelSuffix}
				ariaClearLabel={ariaClearLabel}
				onSuccess={onSuccess}
				disabled={disabled}
				pending={pending}
				error={error}
				patchPrimary={patchPrimary}
				style={style}
			/>
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

/**
 * PrimaryBadgeWithContextMenu (SCRUM-290) — the badge form of SetPrimaryButton
 * with a right-click context menu replacing SCRUM-285's inline Clear text-
 * button. Three invocation paths:
 *
 *   1. Right-click on the badge (mouse / trackpad two-finger).
 *   2. Focus the badge + press the context-menu key or Shift+F10 (keyboard).
 *   3. Long-press on the badge for ~500ms (touch / pen).
 *
 * The menu is a single item: "Clear primary". Activating it PATCHes
 * {kind:""} via the same code path SCRUM-285 already shipped. Outside
 * click and Escape close the menu and return focus to the badge.
 *
 * When `onSuccess` is omitted (read-only context), the badge renders
 * non-interactively — no menu, no tabIndex, no handlers.
 */
function PrimaryBadgeWithContextMenu({
	labelSuffix,
	ariaClearLabel,
	onSuccess,
	disabled,
	pending,
	error,
	patchPrimary,
	style,
}) {
	const interactive = !!onSuccess
	const [menuOpen, setMenuOpen] = useState(false)
	const badgeRef = useRef(null)
	const menuRef = useRef(null)
	const longPressTimerRef = useRef(null)

	useEffect(() => {
		if (!menuOpen) return
		const onDocClick = (e) => {
			if (menuRef.current && !menuRef.current.contains(e.target)) {
				setMenuOpen(false)
			}
		}
		const onKey = (e) => {
			if (e.key === 'Escape') {
				setMenuOpen(false)
				badgeRef.current?.focus()
			}
		}
		document.addEventListener('mousedown', onDocClick)
		document.addEventListener('keydown', onKey)
		return () => {
			document.removeEventListener('mousedown', onDocClick)
			document.removeEventListener('keydown', onKey)
		}
	}, [menuOpen])

	const openMenu = () => {
		if (!interactive || disabled) return
		setMenuOpen(true)
	}

	const cancelLongPress = () => {
		if (longPressTimerRef.current) {
			clearTimeout(longPressTimerRef.current)
			longPressTimerRef.current = null
		}
	}

	const onPointerDown = (e) => {
		// Long-press is for touch/pen only; mouse uses the contextmenu event.
		if (e.pointerType === 'mouse') return
		cancelLongPress()
		longPressTimerRef.current = setTimeout(() => {
			openMenu()
		}, 500)
	}

	const onContextMenu = (e) => {
		if (!interactive) return
		e.preventDefault()
		openMenu()
	}

	const onKeyDown = (e) => {
		if (!interactive) return
		if ((e.key === 'F10' && e.shiftKey) || e.key === 'ContextMenu') {
			e.preventDefault()
			openMenu()
		}
	}

	const onClearClick = () => {
		setMenuOpen(false)
		patchPrimary({ kind: '' })
	}

	return (
		<span style={{ display: 'inline-flex', alignItems: 'center', gap: 6, position: 'relative', ...style }}>
			<span
				ref={badgeRef}
				data-testid="primary-badge"
				role="status"
				aria-label={`Session primary${labelSuffix}`}
				tabIndex={interactive ? 0 : undefined}
				onContextMenu={onContextMenu}
				onKeyDown={onKeyDown}
				onPointerDown={onPointerDown}
				onPointerUp={cancelLongPress}
				onPointerMove={cancelLongPress}
				onPointerCancel={cancelLongPress}
				style={{
					fontSize: 11,
					padding: '2px 6px',
					marginLeft: 6,
					borderRadius: 3,
					backgroundColor: 'var(--color-primary-mid, #1976d2)',
					color: 'white',
					cursor: interactive ? 'context-menu' : 'default',
					outline: 'none',
				}}
			>
				Primary
			</span>
			{menuOpen && (
				<div
					ref={menuRef}
					role="menu"
					data-testid="primary-context-menu"
					style={{
						position: 'absolute',
						top: '100%',
						left: 0,
						marginTop: 4,
						background: '#fff',
						border: '1px solid #ccc',
						borderRadius: 4,
						boxShadow: '0 2px 8px rgba(0,0,0,0.12)',
						zIndex: 100,
						minWidth: 140,
					}}
				>
					<button
						type="button"
						role="menuitem"
						data-testid="clear-primary-btn"
						aria-label={ariaClearLabel}
						onClick={onClearClick}
						disabled={pending || disabled}
						style={{
							display: 'block',
							width: '100%',
							textAlign: 'left',
							fontSize: 12,
							padding: '6px 10px',
							background: 'none',
							border: 'none',
							cursor: pending || disabled ? 'not-allowed' : 'pointer',
							color: '#333',
						}}
					>
						{pending ? 'Clearing…' : 'Clear primary'}
					</button>
				</div>
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

export default SetPrimaryButton
