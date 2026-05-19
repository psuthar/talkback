/**
 * Inline "Start here →" cue rendered next to the Primary badge on the
 * session's primary material row. Controlled component — visibility and
 * dismissal are owned by the parent (typically ParticipantMode), which also
 * persists the shared participant-onboarding dismissal flag.
 *
 * Renders nothing when `open` is false, so it is safe to leave mounted on
 * every primary row; the parent decides when to flip `open` on/off.
 *
 * Props:
 *   open        — whether to render the chip
 *   onDismiss   — called when the user clicks the chip's × button
 */
export function StartHereChip({ open, onDismiss }) {
  if (!open) return null
  return (
    <span
      data-testid="start-here-chip"
      role="status"
      aria-label="Start here"
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 4,
        fontSize: 11,
        padding: '2px 4px 2px 6px',
        borderRadius: 3,
        backgroundColor: 'var(--color-primary-bg, #e3f2fd)',
        color: 'var(--color-primary-dark, #0d47a1)',
        border: '1px solid var(--color-primary-mid, #1976d2)',
        flexShrink: 0,
        lineHeight: 1.4,
        fontWeight: 600,
      }}
    >
      Start here →
      <button
        type="button"
        data-testid="start-here-chip-dismiss"
        aria-label="Dismiss Start here"
        onClick={(e) => { e.stopPropagation(); onDismiss?.() }}
        style={{
          background: 'none',
          border: 'none',
          color: 'inherit',
          cursor: 'pointer',
          padding: '0 2px',
          fontSize: 12,
          lineHeight: 1,
        }}
      >
        ×
      </button>
    </span>
  )
}
