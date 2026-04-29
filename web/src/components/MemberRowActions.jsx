import { useCallback, useEffect, useRef, useState } from 'react'
import styles from './MemberRowActions.module.css'

// MemberRowActions renders a single per-row dropdown trigger for the creator's
// Members panel. Pending invitations expose Resend / Copy link / Revoke (with
// inline confirm for Revoke). Accepted-row variants are introduced in
// SCRUM-213 — this component is the host for that menu.
export function MemberRowActions({
  invitation,
  apiBaseUrl,
  currentUserEmail,
  onFeedback,
  onChanged,
}) {
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [confirmRevoke, setConfirmRevoke] = useState(false)
  const triggerRef = useRef(null)
  const menuRef = useRef(null)

  const status = invitation?.status
  const invitedEmail = (invitation?.invited_email || '').toLowerCase()
  const isSelfRow =
    !!currentUserEmail && invitedEmail === currentUserEmail.toLowerCase()

  const close = useCallback(() => {
    setOpen(false)
    setConfirmRevoke(false)
  }, [])

  useEffect(() => {
    if (!open) return
    const onPointerDown = (event) => {
      const t = triggerRef.current
      const m = menuRef.current
      if (t && t.contains(event.target)) return
      if (m && m.contains(event.target)) return
      close()
    }
    const onKey = (event) => {
      if (event.key === 'Escape') {
        event.stopPropagation()
        close()
        triggerRef.current?.focus()
      }
    }
    document.addEventListener('mousedown', onPointerDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onPointerDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [open, close])

  useEffect(() => {
    if (!open || !menuRef.current) return
    const items = menuRef.current.querySelectorAll('[role="menuitem"]:not([disabled])')
    items[0]?.focus()
  }, [open])

  const handleMenuKeyDown = (event) => {
    if (!menuRef.current) return
    const items = Array.from(
      menuRef.current.querySelectorAll('[role="menuitem"]:not([disabled])')
    )
    if (items.length === 0) return
    const currentIdx = items.indexOf(document.activeElement)
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      items[currentIdx < 0 ? 0 : (currentIdx + 1) % items.length]?.focus()
    } else if (event.key === 'ArrowUp') {
      event.preventDefault()
      items[currentIdx <= 0 ? items.length - 1 : currentIdx - 1]?.focus()
    } else if (event.key === 'Home') {
      event.preventDefault()
      items[0]?.focus()
    } else if (event.key === 'End') {
      event.preventDefault()
      items[items.length - 1]?.focus()
    }
  }

  const base = (apiBaseUrl || '').replace(/\/$/, '')

  const handleResend = async () => {
    if (busy) return
    setBusy(true)
    try {
      const res = await fetch(`${base}/api/invitations/${invitation.id}/resend`, {
        method: 'POST',
        credentials: 'include',
      })
      const data = await res.json().catch(() => ({}))
      if (res.ok) {
        onFeedback?.({ type: 'success', message: 'New link ready.' })
        onChanged?.(data)
      } else {
        onFeedback?.({ type: 'error', message: data?.error || 'Failed to resend' })
      }
    } finally {
      setBusy(false)
      close()
    }
  }

  const handleCopyLink = async () => {
    if (busy) return
    setBusy(true)
    try {
      const res = await fetch(`${base}/api/invitations/${invitation.id}/link`, {
        method: 'GET',
        credentials: 'include',
      })
      const data = await res.json().catch(() => ({}))
      if (res.ok && data.accept_url) {
        try {
          await navigator.clipboard.writeText(data.accept_url)
          onFeedback?.({ type: 'success', message: 'Copied.' })
        } catch {
          onFeedback?.({ type: 'error', message: 'Could not copy to clipboard' })
        }
      } else {
        onFeedback?.({ type: 'error', message: data?.error || 'Failed to get link' })
      }
    } finally {
      setBusy(false)
      close()
    }
  }

  const handleRevokeConfirm = async () => {
    if (busy) return
    setBusy(true)
    try {
      const res = await fetch(`${base}/api/invitations/${invitation.id}/revoke`, {
        method: 'POST',
        credentials: 'include',
      })
      if (res.ok) {
        onFeedback?.({ type: 'success', message: 'Invitation revoked.' })
        onChanged?.()
      } else {
        const data = await res.json().catch(() => ({}))
        onFeedback?.({ type: 'error', message: data?.error || 'Failed to revoke' })
      }
    } finally {
      setBusy(false)
      close()
    }
  }

  if (isSelfRow) return null

  // Today the menu only carries pending-invite actions; SCRUM-213 will branch
  // here to render accepted-row role-change items.
  if (status !== 'pending') return null

  return (
    <div className={styles.wrap} data-testid="member-row-actions" data-invitation-id={invitation.id}>
      <button
        ref={triggerRef}
        type="button"
        data-testid="member-row-actions-trigger"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label="Member actions"
        onClick={() => {
          setOpen((v) => !v)
          setConfirmRevoke(false)
        }}
        disabled={busy}
        className={styles.trigger}
      >
        ⋯
      </button>
      {open && !confirmRevoke && (
        <ul
          ref={menuRef}
          role="menu"
          aria-label="Member actions"
          className={styles.menu}
          data-testid="member-row-menu"
          onKeyDown={handleMenuKeyDown}
        >
          <li role="none">
            <button
              role="menuitem"
              type="button"
              data-testid="member-action-resend"
              onClick={handleResend}
              disabled={busy}
              className={styles.menuItem}
            >
              Resend invitation
            </button>
          </li>
          <li role="none">
            <button
              role="menuitem"
              type="button"
              data-testid="member-action-copy-link"
              onClick={handleCopyLink}
              disabled={busy}
              className={styles.menuItem}
            >
              Copy invite link
            </button>
          </li>
          <li role="none">
            <button
              role="menuitem"
              type="button"
              data-testid="member-action-revoke"
              onClick={() => setConfirmRevoke(true)}
              disabled={busy}
              className={styles.menuItemDanger}
            >
              Revoke invitation
            </button>
          </li>
        </ul>
      )}
      {open && confirmRevoke && (
        <div
          ref={menuRef}
          className={styles.menu}
          data-testid="member-row-revoke-confirm"
          onKeyDown={(e) => {
            if (e.key === 'Escape') close()
          }}
        >
          <div className={styles.confirmBlock}>
            <span>
              Revoke invitation for {invitation.invited_email}? They will lose access immediately.
            </span>
            <div className={styles.confirmActions}>
              <button
                type="button"
                data-testid="member-action-revoke-confirm"
                onClick={handleRevokeConfirm}
                disabled={busy}
                className={styles.confirmDanger}
              >
                {busy ? 'Revoking…' : 'Revoke'}
              </button>
              <button
                type="button"
                data-testid="member-action-revoke-cancel"
                onClick={() => setConfirmRevoke(false)}
                disabled={busy}
                className={styles.confirmCancel}
              >
                Cancel
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
