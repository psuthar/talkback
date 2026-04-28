/**
 * PolishButton — single-purpose AI polish (wand) button reused across the
 * QAPanel ask flow and QAHistory reply flow so the affordance and labeling
 * stay consistent for both typed and voice-confirm sources. The active source
 * is determined by the parent's polish handler (App.polishQuestionText).
 */
export function PolishButton({
  onPolish,
  disabled = false,
  busy = false,
  className,
  size = 14,
  label = 'Polish question with AI',
  shortLabel,
  'data-testid': dataTestId,
}) {
  return (
    <button
      type="button"
      onClick={() => onPolish(true)}
      disabled={disabled}
      title={label}
      aria-label={label}
      className={className}
      data-testid={dataTestId}
    >
      {busy ? (
        <span className="spinner" style={{ width: size - 2, height: size - 2 }} aria-hidden />
      ) : (
        <svg
          width={size}
          height={size}
          viewBox="0 0 24 24"
          fill="currentColor"
          aria-hidden="true"
          focusable="false"
          style={{ display: 'block' }}
        >
          <path d="M12 1l2.753 8.247L23 12l-8.247 2.753L12 23l-2.753-8.247L1 12l8.247-2.753z" />
        </svg>
      )}
      {shortLabel ? <span style={{ marginLeft: 4 }}>{shortLabel}</span> : null}
    </button>
  )
}
