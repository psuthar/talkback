import React, { useEffect, useRef, useState } from 'react'
import { patchSessionPrimary, SessionPrimaryError } from '../api/sessionPrimary'

/**
 * SessionPrimaryRow (SCRUM-294) — row-level wrapper that owns the
 * inline Primary badge + the right-click context menu used to set or
 * clear the session primary.
 *
 * Replaces SCRUM-275/285/290's per-row sub-line ("Primary" pill on its
 * own line, "Make primary" text link beneath each eligible row).
 *
 * Render contract — `children` is a render-fn given:
 *   - `badge`: ReactNode rendered inline inside the row when this row is
 *     the current session primary; null otherwise. The row component is
 *     responsible for placing it (TreeItem accepts a `primaryBadge` prop).
 *   - `rowHandlers`: handlers to spread on the row's outer container so
 *     right-click / Shift+F10 / ContextMenu key / long-press all open the
 *     menu. Empty when the row is not interactive.
 *   - `menuNode`: the context menu portal (null when closed).
 *   - `errorNode`: the inline error region (null when no error).
 *
 * Interactive rules:
 *   - When `isCurrentPrimary` and `onSuccess` are both set → menu offers
 *     "Clear primary" (PATCH {kind:""}).
 *   - When `canSetPrimary` and `onSuccess` are both set → menu offers
 *     "Make primary" (PATCH {kind, id}).
 *   - When `onSuccess` is missing (read-only / participant), the row is
 *     non-interactive: badge still renders for the current primary, no
 *     menu, no handlers.
 *
 * A11y:
 *   - Badge keeps `role="status"` so SR users hear primary state.
 *   - Menu items carry aria-labels including the item title via
 *     `itemLabel` so announcements are unambiguous across many rows.
 *   - Error region uses role="alert" + aria-live="polite".
 */
export function SessionPrimaryRow({
	apiBaseUrl,
	sessionId,
	kind,
	id,
	isCurrentPrimary = false,
	canSetPrimary = false,
	onSuccess,
	disabled = false,
	itemLabel,
	children,
}) {
	const [pending, setPending] = useState(false)
	const [error, setError] = useState(null)
	const [menuOpen, setMenuOpen] = useState(false)
	const menuRef = useRef(null)
	const longPressTimerRef = useRef(null)

	const labelSuffix = itemLabel ? ` (${itemLabel})` : ''
	const ariaSetLabel = `Make session primary${labelSuffix}`
	const ariaClearLabel = `Clear session primary${labelSuffix}`

	const interactive = !!onSuccess && (isCurrentPrimary || canSetPrimary)

	useEffect(() => {
		if (!menuOpen) return
		const onDocClick = (e) => {
			if (menuRef.current && !menuRef.current.contains(e.target)) {
				setMenuOpen(false)
			}
		}
		const onKey = (e) => {
			if (e.key === 'Escape') setMenuOpen(false)
		}
		document.addEventListener('mousedown', onDocClick)
		document.addEventListener('keydown', onKey)
		return () => {
			document.removeEventListener('mousedown', onDocClick)
			document.removeEventListener('keydown', onKey)
		}
	}, [menuOpen])

	const cancelLongPress = () => {
		if (longPressTimerRef.current) {
			clearTimeout(longPressTimerRef.current)
			longPressTimerRef.current = null
		}
	}

	const openMenu = () => {
		if (!interactive || disabled) return
		setMenuOpen(true)
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

	const onPointerDown = (e) => {
		// Long-press is for touch/pen only; mouse uses the contextmenu event.
		if (e.pointerType === 'mouse') return
		cancelLongPress()
		longPressTimerRef.current = setTimeout(openMenu, 500)
	}

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
			setMenuOpen(false)
		}
	}

	const onClickMenuItem = isCurrentPrimary
		? () => patchPrimary({ kind: '' })
		: () => patchPrimary({ kind, id })

	const badge = isCurrentPrimary ? (
		<span
			data-testid="primary-badge"
			role="status"
			aria-label={`Session primary${labelSuffix}`}
			style={{
				fontSize: 11,
				padding: '2px 6px',
				borderRadius: 3,
				backgroundColor: 'var(--color-primary-mid, #1976d2)',
				color: 'white',
				flexShrink: 0,
				lineHeight: 1.4,
			}}
		>
			Primary
		</span>
	) : null

	const rowHandlers = interactive
		? {
			onContextMenu,
			onKeyDown,
			onPointerDown,
			onPointerUp: cancelLongPress,
			onPointerMove: cancelLongPress,
			onPointerCancel: cancelLongPress,
		}
		: {}

	const menuItem = isCurrentPrimary
		? {
			testId: 'clear-primary-btn',
			label: pending ? 'Clearing…' : 'Clear primary',
			ariaLabel: ariaClearLabel,
		}
		: {
			testId: 'make-primary-btn',
			label: pending ? 'Setting…' : 'Make primary',
			ariaLabel: ariaSetLabel,
		}

	const menuNode = menuOpen ? (
		<div
			ref={menuRef}
			role="menu"
			data-testid="primary-context-menu"
			style={{
				position: 'absolute',
				marginTop: 2,
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
				data-testid={menuItem.testId}
				aria-label={menuItem.ariaLabel}
				onClick={onClickMenuItem}
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
				{menuItem.label}
			</button>
		</div>
	) : null

	const errorNode = error ? (
		<div style={{ padding: '0 8px 4px 24px' }}>
			<span
				data-testid="set-primary-error"
				role="alert"
				aria-live="polite"
				style={{ fontSize: 11, color: 'var(--color-danger-dark, #b00020)' }}
			>
				{error.message}
			</span>
		</div>
	) : null

	return children({ badge, rowHandlers, menuNode, errorNode })
}

// Map the SCRUM-272 error codes to recoverable user-facing copy.
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

export default SessionPrimaryRow
