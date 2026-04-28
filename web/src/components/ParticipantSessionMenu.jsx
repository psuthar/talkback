import { useEffect, useId, useRef, useState } from 'react'
import styles from './ParticipantSessionMenu.module.css'

/**
 * ParticipantSessionMenu — single hamburger menu inside the participant top bar
 * that consolidates identity, session navigation, debug toggle, and logout.
 *
 * Props
 * - authUser: { id, email, display_name, global_role } — current user, used for the identity row
 *   and to optionally show an Admin link.
 * - onShowAllSessions: optional () => void — opens the session list (parity with the legacy
 *   "Show All Sessions" button).
 * - onLogout: () => void | Promise<void> — sign out the user.
 * - debugMode / setDebugMode: boolean and setter for the debug toggle item.
 *
 * The trigger has an accessible name ("Session menu") so screen readers and keyboard users do
 * not see it as an icon-only control. Items group identity → navigation → debug → destructive
 * (logout) per the UX review.
 */
export function ParticipantSessionMenu({
  authUser,
  onShowAllSessions,
  participantViewHref,
  onLogout,
  debugMode,
  setDebugMode,
}) {
  const [open, setOpen] = useState(false)
  const triggerRef = useRef(null)
  const menuRef = useRef(null)
  const menuId = useId()

  useEffect(() => {
    if (!open) return

    const handlePointerDown = (event) => {
      if (triggerRef.current && triggerRef.current.contains(event.target)) return
      if (menuRef.current && menuRef.current.contains(event.target)) return
      setOpen(false)
    }
    const handleKeyDown = (event) => {
      if (event.key === 'Escape') {
        event.stopPropagation()
        setOpen(false)
        triggerRef.current?.focus()
      }
    }

    document.addEventListener('mousedown', handlePointerDown)
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('mousedown', handlePointerDown)
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [open])

  const closeAndRun = (fn) => async () => {
    setOpen(false)
    triggerRef.current?.focus()
    if (typeof fn === 'function') await fn()
  }

  const displayName = authUser?.display_name || authUser?.email || 'Signed in'
  const userId = authUser?.id || ''
  const isAdmin = authUser?.global_role === 'admin'

  return (
    <div className={styles.menuRoot}>
      <button
        ref={triggerRef}
        type="button"
        data-testid="participant-session-menu-btn"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={menuId}
        aria-label="Session menu"
        onClick={() => setOpen((v) => !v)}
        className={styles.trigger}
      >
        <span aria-hidden="true" className={styles.triggerIcon}>☰</span>
        <span className={styles.triggerLabel}>Session menu</span>
      </button>

      {open && (
        <div
          ref={menuRef}
          id={menuId}
          role="menu"
          aria-label="Session menu"
          className={styles.menu}
          data-testid="participant-session-menu"
        >
          <div role="presentation" className={styles.identityBlock}>
            <div className={styles.identityName}>
              {displayName}
              {isAdmin && (
                <span className={styles.identityAdminBadge} aria-label="Admin">Admin</span>
              )}
            </div>
            {authUser?.email && authUser.email !== displayName && (
              <div className={styles.identityEmail}>{authUser.email}</div>
            )}
            {userId && (
              <div className={styles.identityId}>
                <span className={styles.identityIdLabel}>ID:</span>{' '}
                <code className={styles.identityIdValue}>{userId}</code>
              </div>
            )}
          </div>

          {onShowAllSessions && (
            <button
              role="menuitem"
              type="button"
              data-testid="menu-show-all-sessions"
              onClick={closeAndRun(onShowAllSessions)}
              className={styles.menuItem}
            >
              Show all sessions
            </button>
          )}

          {participantViewHref && (
            <a
              role="menuitem"
              href={participantViewHref}
              target="_blank"
              rel="noopener noreferrer"
              data-testid="menu-view-as-participant"
              onClick={() => setOpen(false)}
              className={`${styles.menuItem} ${styles.menuItemLink}`}
            >
              View as Participant
            </a>
          )}

          {isAdmin && (
            <a
              role="menuitem"
              href="?mode=admin"
              data-testid="menu-admin-link"
              onClick={() => setOpen(false)}
              className={`${styles.menuItem} ${styles.menuItemLink}`}
            >
              Admin panel
            </a>
          )}

          {typeof setDebugMode === 'function' && (
            <button
              role="menuitemcheckbox"
              type="button"
              data-testid="menu-debug-toggle"
              aria-checked={!!debugMode}
              onClick={() => setDebugMode(!debugMode)}
              className={styles.menuItem}
            >
              <span className={styles.menuCheckMark} aria-hidden="true">{debugMode ? '☑' : '☐'}</span>
              <span>Debug panel</span>
            </button>
          )}

          {onLogout && (
            <button
              role="menuitem"
              type="button"
              data-testid="menu-logout"
              onClick={closeAndRun(onLogout)}
              className={`${styles.menuItem} ${styles.menuItemDestructive}`}
            >
              Log out
            </button>
          )}
        </div>
      )}
    </div>
  )
}
