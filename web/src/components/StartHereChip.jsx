// SCRUM-484: inline "Start here →" cue rendered next to the Primary badge on
// the session's primary material row. Controlled — parent owns visibility.
const CHIP_STYLE = {
  display: 'inline-flex', alignItems: 'center', gap: 4, fontSize: 11,
  padding: '2px 4px 2px 6px', borderRadius: 3,
  backgroundColor: 'var(--color-primary-bg, #e3f2fd)',
  color: 'var(--color-primary-dark, #0d47a1)',
  border: '1px solid var(--color-primary-mid, #1976d2)',
  flexShrink: 0, lineHeight: 1.4, fontWeight: 600,
}
const DISMISS_STYLE = {
  background: 'none', border: 'none', color: 'inherit',
  cursor: 'pointer', padding: '0 2px', fontSize: 12, lineHeight: 1,
}

export function StartHereChip({ open, onDismiss }) {
  if (!open) return null
  return (
    <span data-testid="start-here-chip" role="status" aria-label="Start here" style={CHIP_STYLE}>
      Start here →
      <button
        type="button"
        data-testid="start-here-chip-dismiss"
        aria-label="Dismiss Start here"
        onClick={(e) => { e.stopPropagation(); onDismiss?.() }}
        style={DISMISS_STYLE}
      >×</button>
    </span>
  )
}
