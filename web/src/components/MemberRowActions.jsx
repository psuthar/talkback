import { useCallback, useEffect, useRef, useState } from 'react'
import styles from './MemberRowActions.module.css'
import { ROLE_VALUES, roleLabel, ROLE_PARTICIPANT, ROLE_CREATOR } from '../utils/roleLabels'

// MemberRowActions renders a single per-row dropdown trigger for the creator's
// Members panel. Pending rows expose Resend / Copy link / Revoke (with inline
// confirm for Revoke). Accepted rows expose Set as <role> for each session
// role, with the current role disabled and marked, and an inline confirm only
// when demoting a creator to participant.
export function MemberRowActions({
  invitation,
  apiBaseUrl,
  currentUserEmail,
  onFeedback,
  onChanged,
  onChangeRole,
}) {
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  // mode: 'menu' | 'revokeConfirm' | 'demoteCreatorConfirm'
  const [mode, setMode] = useState('menu')
  const [pendingRole, setPendingRole] = useState(null)
  const triggerRef = useRef(null)
  const menuRef = useRef(null)

  const status = invitation?.status
  const currentRole = invitation?.invited_role || ROLE_PARTICIPANT
  const targetUserId = invitation?.accepted_by_user_id || null
  const invitedEmail = (invitation?.invited_email || '').toLowerCase()
  const isSelfRow =
    !!currentUserEmail && invitedEmail === currentUserEmail.toLowerCase()

  const close = useCallback(() => {
    setOpen(false)
    setMode('menu')
    setPendingRole(null)
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
    if (!open || mode !== 'menu' || !menuRef.current) return
    const items = menuRef.current.querySelectorAll('[role="menuitem"]:not([disabled])')
    items[0]?.focus()
  }, [open, mode])

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

  const fireRoleChange = async (newRole) => {
    if (busy || !targetUserId) {
      if (!targetUserId) {
        onFeedback?.({ type: 'error', message: 'Cannot change role: missing user id.' })
      }
      return
    }
    setBusy(true)
    try {
      const result = await onChangeRole?.(targetUserId, newRole)
      if (result && result.ok === false) {
        onFeedback?.({ type: 'error', message: result.error || 'Failed to update role' })
      } else {
        onFeedback?.({ type: 'success', message: `Role updated to ${roleLabel(newRole)}.` })
        onChanged?.()
      }
    } finally {
      setBusy(false)
      close()
    }
  }

  const handleRoleSelect = (newRole) => {
    if (newRole === currentRole) return
    // Inline confirm only when demoting a creator to participant. Promotions
    // (participant → decision_maker, etc.) and Decision Maker swaps fire
    // immediately.
    if (currentRole === ROLE_CREATOR && newRole === ROLE_PARTICIPANT) {
      setPendingRole(newRole)
      setMode('demoteCreatorConfirm')
      return
    }
    fireRoleChange(newRole)
  }

  if (isSelfRow) return null
  if (status !== 'pending' && status !== 'accepted') return null

  const isPending = status === 'pending'

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
          setMode('menu')
          setPendingRole(null)
        }}
        disabled={busy}
        className={styles.trigger}
      >
        ⋯
      </button>
      {open && mode === 'menu' && isPending && (
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
              onClick={() => setMode('revokeConfirm')}
              disabled={busy}
              className={styles.menuItemDanger}
            >
              Revoke invitation
            </button>
          </li>
        </ul>
      )}
      {open && mode === 'menu' && !isPending && (
        <ul
          ref={menuRef}
          role="menu"
          aria-label="Member actions"
          className={styles.menu}
          data-testid="member-row-menu"
          onKeyDown={handleMenuKeyDown}
        >
          {ROLE_VALUES.map((role) => {
            const isCurrent = role === currentRole
            return (
              <li role="none" key={role}>
                <button
                  role="menuitem"
                  type="button"
                  data-testid={`member-action-role-${role}`}
                  data-selected={isCurrent ? 'true' : 'false'}
                  onClick={() => handleRoleSelect(role)}
                  disabled={busy || isCurrent}
                  aria-disabled={isCurrent ? 'true' : 'false'}
                  className={styles.menuItem}
                >
                  <span aria-hidden="true" className={styles.menuItemMark}>{isCurrent ? '✓' : ''}</span>
                  <span>Set as {roleLabel(role)}</span>
                </button>
              </li>
            )
          })}
        </ul>
      )}
      {open && mode === 'revokeConfirm' && (
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
                onClick={() => setMode('menu')}
                disabled={busy}
                className={styles.confirmCancel}
              >
                Cancel
              </button>
            </div>
          </div>
        </div>
      )}
      {open && mode === 'demoteCreatorConfirm' && pendingRole && (
        <div
          ref={menuRef}
          className={styles.menu}
          data-testid="member-row-role-confirm"
          onKeyDown={(e) => {
            if (e.key === 'Escape') close()
          }}
        >
          <div className={styles.confirmBlock}>
            <span>
              Change {invitation.invited_email}'s role to {roleLabel(pendingRole)}? They will lose creator permissions.
            </span>
            <div className={styles.confirmActions}>
              <button
                type="button"
                data-testid="member-action-role-confirm"
                onClick={() => fireRoleChange(pendingRole)}
                disabled={busy}
                className={styles.confirmDanger}
              >
                {busy ? 'Updating…' : 'Change role'}
              </button>
              <button
                type="button"
                data-testid="member-action-role-cancel"
                onClick={() => {
                  setMode('menu')
                  setPendingRole(null)
                }}
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
