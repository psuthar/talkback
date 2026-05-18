// SCRUM-421: recordings picker — multi-select + confirm + attach. Drives
// the "Browse <Platform> recordings" CTA from SCRUM-420's
// PlatformConnectionTile across Zoom (SCRUM-420), Meet (SCRUM-422), and
// Teams (SCRUM-423).
//
// SCRUM-462 (modal redesign):
//   1. Renders as a centered modal overlay with a semi-transparent
//      backdrop instead of expanding the sidebar inline. Esc closes;
//      backdrop click closes; aria-modal=true; initial focus lands on
//      the close affordance.
//   2. Replaces the full-width "Close" button with a small × icon button
//      in the upper-right corner of the dialog card.
//   3. Drops the "With transcript" checkbox filter. Each result row
//      now shows a small CC-style transcript icon when has_transcript
//      is true.
//   4. Drops the "Apply filters" button. Filter changes (search title,
//      date range, custom from/to) re-run the list query automatically
//      after a 300ms debounce, so the user doesn't hammer the platform
//      API on every keystroke.
//
// The data flow (fetch, multi-select, confirm dialog, attach POST,
// 429 cap handling, already-imported / oversized disabling) is unchanged
// from SCRUM-421.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

const MAX_DURATION_MINUTES = 240 // 4 hours; mirrors SCRUM-421 spec default
const FILTER_DEBOUNCE_MS = 300

const PLATFORM_META = {
  zoom: {
    label: 'Zoom',
    listPath: (q) => `/api/zoom/recordings${q ? `?${q}` : ''}`,
    attachPath: (sessionId) => `/api/sessions/${sessionId}/import/zoom`,
    buildAttachBody: (r) => ({ meeting_uuid: r.meeting_uuid, instance_uuid: r.instance_uuid || r.meeting_uuid }),
  },
  google_meet: {
    label: 'Google Meet',
    listPath: (q) => `/api/google-meet/recordings${q ? `?${q}` : ''}`,
    attachPath: (sessionId) => `/api/sessions/${sessionId}/import/google-meet`,
    buildAttachBody: (r) => ({ conference_record: r.meeting_uuid, recording: r.instance_uuid || r.meeting_uuid }),
  },
  teams: {
    label: 'Microsoft Teams',
    listPath: (q) => `/api/teams/recordings${q ? `?${q}` : ''}`,
    attachPath: (sessionId) => `/api/sessions/${sessionId}/import/teams`,
    buildAttachBody: (r) => ({ meeting_id: r.meeting_uuid, recording_id: r.instance_uuid || r.meeting_uuid }),
  },
}

const DATE_RANGES = [
  { key: '7', label: 'Last 7 days', days: 7 },
  { key: '30', label: 'Last 30 days', days: 30 },
  { key: '90', label: 'Last 90 days', days: 90 },
  { key: 'custom', label: 'Custom' },
]

function isoOrEmpty(daysAgo) {
  const d = new Date()
  d.setDate(d.getDate() - daysAgo)
  return d.toISOString().slice(0, 10)
}

function recordingExternalID(r) {
  // Match SCRUM-403's external_recording_id semantics (instance_uuid first,
  // falling back to meeting_uuid). The parent passes a Set of already-
  // imported external_recording_ids built from currentSession.video_sources.
  return r.instance_uuid || r.meeting_uuid || ''
}

function recordingIsImported(r, importedSet) {
  const id = recordingExternalID(r)
  return id && importedSet.has(id)
}

function recordingIsOversized(r) {
  return typeof r.duration_minutes === 'number' && r.duration_minutes > MAX_DURATION_MINUTES
}

// SCRUM-462: inline CC-style icon used per-row when a recording's
// transcript is available. Replaces the SCRUM-421 verbose copy
// ("Zoom transcript ready" / "No native transcript — we'll transcribe").
function TranscriptIcon({ id }) {
  return (
    <svg
      data-testid={`recording-transcript-icon-${id}`}
      role="img"
      aria-label="Transcript available"
      width="14"
      height="14"
      viewBox="0 0 16 16"
      style={{ verticalAlign: 'middle' }}
    >
      <title>Transcript available</title>
      <rect x="1" y="3" width="14" height="10" rx="2" ry="2" fill="none" stroke="currentColor" strokeWidth="1.2" />
      <path d="M5 8.5h2M9 8.5h2" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
    </svg>
  )
}

export function RecordingsPicker({
  platform,
  sessionId,
  apiBaseUrl,
  accountEmail,
  importedExternalIds = [],
  onClose,
  onSwitchAccount,
  onImported,
  userEmail,
}) {
  const meta = PLATFORM_META[platform] || PLATFORM_META.zoom
  const importedSet = useMemo(() => new Set(importedExternalIds), [importedExternalIds])

  const [recordings, setRecordings] = useState([])
  const [loading, setLoading] = useState(false)
  const [loadError, setLoadError] = useState(null)
  const [query, setQuery] = useState('')
  const [rangeKey, setRangeKey] = useState('30')
  const [customFrom, setCustomFrom] = useState('')
  const [customTo, setCustomTo] = useState('')
  const [selected, setSelected] = useState(new Set())
  const [confirming, setConfirming] = useState(false)
  const [importing, setImporting] = useState(false)
  const [importErrors, setImportErrors] = useState([])

  const closeBtnRef = useRef(null)
  const base = useMemo(() => (apiBaseUrl || '').replace(/\/$/, ''), [apiBaseUrl])

  const buildListQuery = useCallback(() => {
    const params = new URLSearchParams()
    const range = DATE_RANGES.find((r) => r.key === rangeKey)
    if (range && range.days) {
      params.set('from', isoOrEmpty(range.days))
      params.set('to', isoOrEmpty(0))
    } else if (rangeKey === 'custom' && customFrom && customTo) {
      params.set('from', customFrom)
      params.set('to', customTo)
    }
    if (query.trim()) params.set('q', query.trim())
    return params.toString()
  }, [rangeKey, customFrom, customTo, query])

  const refresh = useCallback(async () => {
    if (!base) return
    setLoading(true)
    setLoadError(null)
    try {
      const headers = { 'Accept': 'application/json' }
      if (userEmail) headers['X-Creator-Identity'] = userEmail
      const res = await fetch(`${base}${meta.listPath(buildListQuery())}`, {
        credentials: 'include',
        headers,
      })
      if (!res.ok) {
        throw new Error(`list ${platform}: ${res.status}`)
      }
      const body = await res.json()
      setRecordings(Array.isArray(body.items) ? body.items : [])
    } catch (err) {
      setLoadError(err)
    } finally {
      setLoading(false)
    }
  }, [base, meta, platform, userEmail, buildListQuery])

  // SCRUM-462: auto-refresh on filter change with a 300ms debounce so
  // typing into the search field doesn't hammer the upstream API on
  // every keystroke. `refresh` already depends on every filter input,
  // so a single effect on [refresh] covers all of them.
  useEffect(() => {
    const t = setTimeout(refresh, FILTER_DEBOUNCE_MS)
    return () => clearTimeout(t)
  }, [refresh])

  // SCRUM-462: Esc-to-close — attach to document so it fires regardless
  // of which dialog control currently has focus. Also focus the close
  // button on mount so the user can immediately Esc / Enter / Space out
  // of the picker.
  useEffect(() => {
    const onKey = (e) => {
      if (e.key === 'Escape' && typeof onClose === 'function') onClose()
    }
    document.addEventListener('keydown', onKey)
    if (closeBtnRef.current) closeBtnRef.current.focus()
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  const visible = useMemo(() => {
    // The backend filters by q + date range; visible == recordings. Kept
    // as a hook so a future client-side filter pass can slot in here
    // without re-querying.
    return recordings
  }, [recordings])

  const selectableIDs = useMemo(() => {
    const ids = []
    for (const r of visible) {
      if (recordingIsImported(r, importedSet) || recordingIsOversized(r)) continue
      ids.push(recordingExternalID(r))
    }
    return ids
  }, [visible, importedSet])

  const toggleOne = (id) => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const beginImport = () => {
    if (selected.size === 0) return
    setImportErrors([])
    setConfirming(true)
  }

  const cancelConfirm = () => setConfirming(false)

  const performImport = useCallback(async () => {
    if (!base || !sessionId) return
    setImporting(true)
    const errors = []
    const headers = { 'Content-Type': 'application/json', 'Accept': 'application/json' }
    if (userEmail) headers['X-Creator-Identity'] = userEmail
    const selectedRecordings = visible.filter((r) => selected.has(recordingExternalID(r)))
    const importedJobs = []
    for (const rec of selectedRecordings) {
      try {
        const res = await fetch(`${base}${meta.attachPath(sessionId)}`, {
          method: 'POST',
          credentials: 'include',
          headers,
          body: JSON.stringify(meta.buildAttachBody(rec)),
        })
        const body = await res.json().catch(() => ({}))
        if (res.status === 429) {
          errors.push({ recording: rec, message: `Cap exceeded (${body.cap || ''} max). Remove a recording from this session to add another.` })
          break // stop the batch
        }
        if (!res.ok && res.status !== 202 && res.status !== 200) {
          errors.push({ recording: rec, message: body.message || `HTTP ${res.status}` })
          continue
        }
        importedJobs.push(body)
      } catch (err) {
        errors.push({ recording: rec, message: err.message || String(err) })
      }
    }
    setImporting(false)
    setImportErrors(errors)
    if (errors.length === 0) {
      setConfirming(false)
      setSelected(new Set())
      if (typeof onImported === 'function') onImported(importedJobs)
      if (typeof onClose === 'function') onClose()
    }
  }, [base, sessionId, meta, selected, visible, userEmail, onImported, onClose])

  // SCRUM-462: backdrop click handler — only close when the click lands
  // on the overlay itself (e.target === e.currentTarget), not when a
  // descendant element bubbles up to the overlay.
  const onBackdropClick = (e) => {
    if (e.target === e.currentTarget && typeof onClose === 'function') onClose()
  }

  return (
    <div
      data-testid={`recordings-picker-${platform}`}
      role="dialog"
      aria-modal="true"
      aria-label={`${meta.label} recordings`}
      style={overlayStyle}
      onClick={onBackdropClick}
    >
      <div style={dialogStyle} data-testid="recordings-picker-dialog">
        <button
          ref={closeBtnRef}
          data-testid="recordings-picker-close"
          aria-label="Close recordings picker"
          onClick={onClose}
          style={closeXStyle}
        >
          ×
        </button>

        <header data-testid="recordings-picker-header" style={headerStyle}>
          <h2 style={{ margin: 0, fontSize: '18px' }}>{meta.label} recordings</h2>
          {accountEmail && (
            <p data-testid="recordings-picker-account" style={accountStyle}>
              Connected: {accountEmail}{' '}
              <button data-testid="recordings-picker-switch-account" onClick={onSwitchAccount} style={inlineLinkStyle}>
                Switch account
              </button>
            </p>
          )}
        </header>

        <section data-testid="recordings-picker-filters" style={filtersStyle}>
          <input
            type="search"
            data-testid="recordings-picker-search"
            placeholder="Search title"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            style={inputStyle}
          />
          <select
            data-testid="recordings-picker-range"
            value={rangeKey}
            onChange={(e) => setRangeKey(e.target.value)}
            style={inputStyle}
          >
            {DATE_RANGES.map((r) => (
              <option key={r.key} value={r.key}>{r.label}</option>
            ))}
          </select>
          {rangeKey === 'custom' && (
            <>
              <input
                type="date"
                data-testid="recordings-picker-from"
                value={customFrom}
                onChange={(e) => setCustomFrom(e.target.value)}
                style={inputStyle}
              />
              <input
                type="date"
                data-testid="recordings-picker-to"
                value={customTo}
                onChange={(e) => setCustomTo(e.target.value)}
                style={inputStyle}
              />
            </>
          )}
          {loading && (
            <span data-testid="recordings-picker-loading" style={mutedStyle}>Loading…</span>
          )}
        </section>

        {loadError && (
          <p data-testid="recordings-picker-error" style={errorStyle}>Failed to load: {String(loadError)}</p>
        )}
        {!loading && !loadError && visible.length === 0 && (
          <p data-testid="recordings-picker-empty" style={mutedStyle}>No recordings match your filters.</p>
        )}

        {visible.length > 0 && (
          <ul data-testid="recordings-picker-list" style={listStyle}>
            {visible.map((r) => {
              const id = recordingExternalID(r)
              const isImported = recordingIsImported(r, importedSet)
              const isOversized = recordingIsOversized(r)
              const isDisabled = isImported || isOversized
              const isSelected = selected.has(id)
              return (
                <li
                  key={id || `${r.meeting_topic}:${r.start_time}`}
                  data-testid={`recording-row-${id}`}
                  data-state={isImported ? 'imported' : isOversized ? 'oversized' : isSelected ? 'selected' : 'available'}
                  aria-disabled={isDisabled}
                  style={rowStyle(isDisabled)}
                >
                  <input
                    type="checkbox"
                    data-testid={`recording-checkbox-${id}`}
                    checked={isSelected}
                    onChange={() => toggleOne(id)}
                    disabled={isDisabled}
                    aria-label={`Select ${r.meeting_topic}`}
                  />
                  <span data-testid={`recording-title-${id}`} style={titleStyle}>
                    {r.meeting_topic || '(untitled)'}
                    {r.has_transcript && <TranscriptIcon id={id} />}
                  </span>
                  <span data-testid={`recording-time-${id}`} style={mutedSmallStyle}>{r.start_time}</span>
                  <span data-testid={`recording-duration-${id}`} style={mutedSmallStyle}>{r.duration_minutes} min</span>
                  {isImported && (
                    <span data-testid={`recording-already-imported-${id}`} style={mutedSmallStyle}>Already in this session</span>
                  )}
                  {isOversized && !isImported && (
                    <span data-testid={`recording-oversized-${id}`} style={mutedSmallStyle}>Over {MAX_DURATION_MINUTES / 60}h — contact support</span>
                  )}
                </li>
              )
            })}
          </ul>
        )}

        <footer data-testid="recordings-picker-footer" style={footerStyle}>
          <button
            data-testid="recordings-picker-import"
            onClick={beginImport}
            disabled={selected.size === 0 || importing}
            style={primaryButtonStyle}
          >
            {`Import ${selected.size} recording${selected.size === 1 ? '' : 's'}`}
          </button>
          <button data-testid="recordings-picker-cancel" onClick={onClose} style={secondaryButtonStyle}>
            Cancel
          </button>
          {selectableIDs.length === 0 && visible.length > 0 && (
            <p data-testid="recordings-picker-none-selectable" style={mutedStyle}>
              All listed recordings are either already imported or oversized.
            </p>
          )}
        </footer>

        {confirming && (
          <div data-testid="recordings-picker-confirm" role="dialog" aria-label="Confirm import" style={confirmStyle}>
            <p>Import {selected.size} recording{selected.size === 1 ? '' : 's'}? Each takes ~3–10 min to ingest.</p>
            <ul data-testid="recordings-picker-confirm-list">
              {Array.from(selected).map((id) => {
                const rec = visible.find((r) => recordingExternalID(r) === id)
                return (
                  <li key={id} data-testid={`recordings-picker-confirm-row-${id}`}>
                    {rec ? rec.meeting_topic : id}
                  </li>
                )
              })}
            </ul>
            {importErrors.length > 0 && (
              <ul data-testid="recordings-picker-import-errors">
                {importErrors.map((e, i) => (
                  <li key={i} data-testid={`recordings-picker-import-error-${i}`}>
                    {(e.recording?.meeting_topic || '(unknown)') + ': ' + e.message}
                  </li>
                ))}
              </ul>
            )}
            <button
              data-testid="recordings-picker-confirm-button"
              onClick={performImport}
              disabled={importing}
              style={primaryButtonStyle}
            >
              {importing ? 'Importing…' : `Import ${selected.size}`}
            </button>
            <button data-testid="recordings-picker-confirm-cancel" onClick={cancelConfirm} disabled={importing} style={secondaryButtonStyle}>
              Back
            </button>
          </div>
        )}
      </div>
    </div>
  )
}

// SCRUM-462: inline modal styles. Kept here so the redesign is fully
// self-contained — no new CSS module file to wire up. Trade-off: less
// theming flexibility; revisit if/when the design system lands.
const overlayStyle = {
  position: 'fixed',
  inset: 0,
  zIndex: 9999,
  backgroundColor: 'rgba(0, 0, 0, 0.45)',
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  padding: '24px',
}
const dialogStyle = {
  position: 'relative',
  backgroundColor: '#fff',
  borderRadius: '8px',
  boxShadow: '0 12px 32px rgba(0,0,0,0.18)',
  width: 'min(640px, 95vw)',
  maxHeight: '85vh',
  overflowY: 'auto',
  padding: '20px 24px',
  display: 'flex',
  flexDirection: 'column',
  gap: '12px',
}
const closeXStyle = {
  position: 'absolute',
  top: '8px',
  right: '10px',
  width: '28px',
  height: '28px',
  border: 'none',
  background: 'transparent',
  fontSize: '22px',
  lineHeight: '1',
  cursor: 'pointer',
  color: '#555',
  borderRadius: '4px',
}
const headerStyle = { display: 'flex', flexDirection: 'column', gap: '4px', paddingRight: '32px' }
const accountStyle = { fontSize: '12px', color: '#666', margin: 0 }
const inlineLinkStyle = { background: 'none', border: 'none', color: '#1976d2', cursor: 'pointer', padding: 0, fontSize: '12px' }
const filtersStyle = { display: 'flex', gap: '8px', alignItems: 'center', flexWrap: 'wrap' }
const inputStyle = { padding: '6px 8px', fontSize: '13px', border: '1px solid #ccc', borderRadius: '4px' }
const mutedStyle = { fontSize: '12px', color: '#666', margin: 0 }
const mutedSmallStyle = { fontSize: '11px', color: '#888' }
const errorStyle = { fontSize: '13px', color: '#c62828', margin: 0 }
const listStyle = { listStyle: 'none', padding: 0, margin: 0, display: 'flex', flexDirection: 'column', gap: '4px', maxHeight: '40vh', overflowY: 'auto' }
const rowStyle = (disabled) => ({
  display: 'flex',
  alignItems: 'center',
  gap: '8px',
  padding: '6px 8px',
  borderRadius: '4px',
  backgroundColor: disabled ? '#f5f5f5' : 'transparent',
  opacity: disabled ? 0.6 : 1,
})
const titleStyle = { fontSize: '13px', display: 'inline-flex', alignItems: 'center', gap: '6px' }
const footerStyle = { display: 'flex', gap: '8px', alignItems: 'center' }
const primaryButtonStyle = {
  padding: '8px 14px', fontSize: '13px', fontWeight: 500, border: 'none', borderRadius: '4px',
  backgroundColor: '#1976d2', color: '#fff', cursor: 'pointer',
}
const secondaryButtonStyle = {
  padding: '8px 14px', fontSize: '13px', fontWeight: 500, border: '1px solid #ccc', borderRadius: '4px',
  backgroundColor: '#fff', color: '#444', cursor: 'pointer',
}
const confirmStyle = {
  borderTop: '1px solid #eee',
  paddingTop: '12px',
  marginTop: '4px',
}
