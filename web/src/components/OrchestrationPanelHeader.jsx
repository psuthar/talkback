/**
 * OrchestrationPanelHeader — header chrome for the creator's "AI Suggested
 * Next Actions" panel (SCRUM-195).
 *
 * Title is rendered as non-bold 12px secondary text. Refresh becomes an
 * icon-only button with aria-label="Refresh suggestions" and a tooltip; the
 * arrow icon is replaced by a spinner while loading.
 */
export function OrchestrationPanelHeader({ loading, disabled, onRefresh }) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '8px', marginBottom: '8px' }}>
      <span
        data-testid="orchestration-panel-title"
        style={{ fontSize: '12px', color: 'var(--color-text-secondary)', fontWeight: 500 }}
      >
        Suggested next actions
      </span>
      <button
        data-testid="orchestration-refresh-btn"
        type="button"
        onClick={onRefresh}
        disabled={!!disabled}
        aria-label="Refresh suggestions"
        title={loading ? 'Refreshing suggestions…' : 'Refresh suggestions'}
        style={{
          margin: 0,
          padding: '4px',
          width: '24px',
          height: '24px',
          display: 'inline-flex',
          alignItems: 'center',
          justifyContent: 'center',
          background: 'transparent',
          border: '1px solid transparent',
          borderRadius: '4px',
          color: 'var(--color-text-muted)',
          cursor: disabled ? 'not-allowed' : 'pointer',
          lineHeight: 1,
        }}
      >
        {loading ? (
          <span data-testid="orchestration-refresh-spinner" className="spinner" style={{ width: 12, height: 12 }} aria-hidden />
        ) : (
          <svg
            data-testid="orchestration-refresh-icon"
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
          >
            <polyline points="23 4 23 10 17 10" />
            <polyline points="1 20 1 14 7 14" />
            <path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15" />
          </svg>
        )}
      </button>
    </div>
  )
}
