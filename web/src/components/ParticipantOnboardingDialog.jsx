import { useEffect, useId, useRef } from 'react'
import styles from './ParticipantOnboardingDialog.module.css'

/**
 * First-join guidance for participant mode (Review → Ask → Decide).
 * Controlled by parent; dismissal persistence is handled in ParticipantMode + participantStorage.
 */
export function ParticipantOnboardingDialog({ open, onDismiss }) {
  const titleId = useId()
  const dismissBtnRef = useRef(null)
  const dialogRef = useRef(null)

  useEffect(() => {
    if (!open) return
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.body.style.overflow = prev
    }
  }, [open])

  useEffect(() => {
    if (!open) return
    dismissBtnRef.current?.focus()
  }, [open])

  useEffect(() => {
    if (!open) return
    const onKeyDown = (e) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        onDismiss?.()
      }
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [open, onDismiss])

  if (!open) return null

  return (
    <div className={styles.backdrop} role="presentation">
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        data-testid="participant-onboarding-dialog"
        className={styles.dialog}
      >
        <h2 id={titleId} className={styles.title}>
          How this session works
        </h2>
        <div className={styles.body}>
          <p>For the best outcome, follow this flow:</p>
          <ul>
            <li>
              <strong>Review</strong> the materials in the left panel.
            </li>
            <li>
              <strong>Ask</strong> clarifying questions in <strong>Q&A</strong> when something is unclear.
            </li>
            <li>
              <strong>Decide</strong> by submitting your stance on the primary decision — use the decision bar at the top of the page when you’re ready.
            </li>
          </ul>
        </div>
        <div className={styles.actions}>
          <button
            ref={dismissBtnRef}
            type="button"
            className={styles.primaryBtn}
            data-testid="participant-onboarding-dismiss"
            onClick={() => onDismiss?.()}
          >
            Got it
          </button>
        </div>
      </div>
    </div>
  )
}
