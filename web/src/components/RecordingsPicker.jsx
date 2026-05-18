// SCRUM-421: recordings picker — multi-select + confirm + attach.
//
// SCRUM-462 (modal redesign): popup overlay + × close + inline transcript
// icon + auto-refetch on filter change with 300ms debounce.
//
// SCRUM-463 (unified import): segmented platform selector lives INSIDE
// the modal. The caller no longer hard-pins a platform; they open the
// picker via a single "Import meeting recording" entry point and the
// user chooses Zoom / Google Meet / Teams in the dialog. The initial
// recordings fetch is gated behind an explicit "Load recordings" button —
// no auto-fetch on open or on platform change. After a Load has fired
// once, the SCRUM-462 debounced auto-refetch resumes for subsequent
// filter edits. Last platform is remembered per-user in localStorage so
// the common case (same user, same platform week-over-week) is one click.
//
// The data flow (fetch, multi-select, confirm dialog, attach POST, 429
// cap handling, already-imported / oversized disabling, switch-account)
// is unchanged from SCRUM-421.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

const MAX_DURATION_MINUTES = 240 // 4 hours; mirrors SCRUM-421 spec default
const FILTER_DEBOUNCE_MS = 300
const LAST_PLATFORM_STORAGE_KEY = 'tb.import.lastPlatform'

const PLATFORM_META = {
  zoom: {
    label: 'Zoom',
    listPath: (q) => `/api/zoom/recordings${q ? `?${q}` : ''}`,
    attachPath: (sessionId) => `/api/sessions/${sessionId}/import/zoom`,
    buildAttachBody: (r) => ({ meeting_uuid: r.meeting_uuid, instance_uuid: r.instance_uuid || r.meeting_uuid }),
    connectPath: '/api/zoom/connect',
  },
  google_meet: {
    label: 'Google Meet',
    listPath: (q) => `/api/google-meet/recordings${q ? `?${q}` : ''}`,
    attachPath: (sessionId) => `/api/sessions/${sessionId}/import/google-meet`,
    buildAttachBody: (r) => ({ conference_record: r.meeting_uuid, recording: r.instance_uuid || r.meeting_uuid }),
    connectPath: '/api/google-meet/connect',
  },
  teams: {
    label: 'Microsoft Teams',
    listPath: (q) => `/api/teams/recordings${q ? `?${q}` : ''}`,
    attachPath: (sessionId) => `/api/sessions/${sessionId}/import/teams`,
    buildAttachBody: (r) => ({ meeting_id: r.meeting_uuid, recording_id: r.instance_uuid || r.meeting_uuid }),
    connectPath: '/api/teams/connect',
  },
}

const PLATFORM_ORDER = ['zoom', 'google_meet', 'teams']

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
  return r.instance_uuid || r.meeting_uuid || ''
}

function recordingIsImported(r, importedSet) {
  const id = recordingExternalID(r)
  return id && importedSet.has(id)
}

function recordingIsOversized(r) {
  return typeof r.duration_minutes === 'number' && r.duration_minutes > MAX_DURATION_MINUTES
}

// SCRUM-462: small CC-style transcript indicator rendered inline per row
// when has_transcript=true.
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

// SCRUM-463: pick the default platform when the modal opens. Order of
// preference: lastPlatform persisted from a prior session, then the
// first connected platform, then 'zoom' as the universal fallback.
function pickDefaultPlatform(integrations) {
  try {
    const stored = window.localStorage.getItem(LAST_PLATFORM_STORAGE_KEY)
    if (stored && PLATFORM_ORDER.includes(stored)) return stored
  } catch (_) { /* localStorage may be unavailable; fall through */ }
  if (integrations) {
    for (const p of PLATFORM_ORDER) {
      if (integrations[p]?.connected) return p
    }
  }
  return 'zoom'
}

function persistLastPlatform(platform) {
  try { window.localStorage.setItem(LAST_PLATFORM_STORAGE_KEY, platform) } catch (_) {}
}

export function RecordingsPicker({
  sessionId,
  apiBaseUrl,
  integrations,        // SCRUM-463: { zoom, google_meet, teams } from useIntegrationsStatus
  importedExternalIds = [],
  onClose,
  onSwitchAccount,
  onConnect,           // SCRUM-463: opens OAuth popup for the disconnected platform
  onImported,
  userEmail,
  initialPlatform,     // optional override; rarely used in production
}) {
  const importedSet = useMemo(() => new Set(importedExternalIds), [importedExternalIds])

  const [platform, setPlatform] = useState(() => initialPlatform || pickDefaultPlatform(integrations))
  const meta = PLATFORM_META[platform] || PLATFORM_META.zoom

  const [recordings, setRecordings] = useState([])
  const [loading, setLoading] = useState(false)
  const [loadError, setLoadError] = useState(null)
  const [hasLoaded, setHasLoaded] = useState(false) // SCRUM-463: gate auto-refetch until first Load
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

  const connected = !!integrations?.[platform]?.connected
  const accountEmail = integrations?.[platform]?.account_email || null

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
    // SCRUM-464: empty base is valid — SPA runs same-origin in
    // production so apiBaseUrl is intentionally "". Don't bail on
    // falsy; fetch a relative path. Same SCRUM-459 pattern.
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

  // SCRUM-463: explicit Load button is required before any fetch fires.
  // Once hasLoaded is true, SCRUM-462's debounced auto-refetch resumes
  // for filter edits (search / range). Switching platforms resets
  // hasLoaded back to false so the user must press Load again — see the
  // platform-change handler below.
  const handleLoad = useCallback(() => {
    setHasLoaded(true)
    persistLastPlatform(platform)
    refresh()
  }, [platform, refresh])

  useEffect(() => {
    if (!hasLoaded) return
    const t = setTimeout(refresh, FILTER_DEBOUNCE_MS)
    return () => clearTimeout(t)
  }, [refresh, hasLoaded])

  // Esc-to-close + focus the close button on mount.
  useEffect(() => {
    const onKey = (e) => {
      if (e.key === 'Escape' && typeof onClose === 'function') onClose()
    }
    document.addEventListener('keydown', onKey)
    if (closeBtnRef.current) closeBtnRef.current.focus()
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  // SCRUM-463: switching platforms wipes everything specific to the
  // previously-viewed platform.
  const handlePlatformChange = (next) => {
    if (next === platform) return
    setPlatform(next)
    setRecordings([])
    setSelected(new Set())
    setLoadError(null)
    setQuery('')
    setRangeKey('30')
    setCustomFrom('')
    setCustomTo('')
    setHasLoaded(false)
  }

  const handleConnect = () => {
    if (typeof onConnect === 'function') onConnect(platform)
  }

  const visible = recordings
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
    // SCRUM-464: same-origin SPA gives apiBaseUrl="" → empty base.
    // Only bail when sessionId is missing.
    if (!sessionId) return
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
          break
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

  const onBackdropClick = (e) => {
    if (e.target === e.currentTarget && typeof onClose === 'function') onClose()
  }

  const loadDisabled = !connected || loading
  const loadLabel = loading ? 'Loading…' : 'Load recordings'

  return (
    <div
      data-testid="recordings-picker"
      role="dialog"
      aria-modal="true"
      aria-label="Import meeting recording"
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
          <h2 style={{ margin: 0, fontSize: '18px' }}>Import meeting recording</h2>
        </header>

        {/* SCRUM-463: segmented platform selector with status dots. */}
        <div data-testid="recordings-picker-platform-selector" role="tablist" style={selectorStyle}>
          {PLATFORM_ORDER.map((p) => {
            const isActive = p === platform
            const isConnected = !!integrations?.[p]?.connected
            return (
              <button
                key={p}
                role="tab"
                aria-selected={isActive}
                data-testid={`recordings-picker-platform-${p}`}
                data-active={isActive}
                data-connected={isConnected}
                onClick={() => handlePlatformChange(p)}
                style={segmentStyle(isActive)}
              >
                <span
                  data-testid={`recordings-picker-platform-${p}-status`}
                  data-status={isConnected ? 'connected' : 'disconnected'}
                  aria-label={isConnected ? 'Connected' : 'Not connected'}
                  style={statusDotStyle(isConnected)}
                />
                {PLATFORM_META[p].label}
              </button>
            )
          })}
        </div>

        {connected && accountEmail && (
          <p data-testid="recordings-picker-account" style={accountStyle}>
            Connected as {accountEmail}{' '}
            {typeof onSwitchAccount === 'function' && (
              <button data-testid="recordings-picker-switch-account" onClick={() => onSwitchAccount(platform)} style={inlineLinkStyle}>
                Switch account
              </button>
            )}
          </p>
        )}

        {/* SCRUM-463: disconnected variant — swap the middle of the
            modal for an inline Connect CTA. Filters + Load button are
            replaced; the list area is hidden. */}
        {!connected && (
          <div data-testid="recordings-picker-disconnected" style={disconnectedStyle}>
            <p style={{ margin: 0 }}>{meta.label} isn't connected yet.</p>
            <p style={mutedStyle}>Connect to browse and import your {meta.label} recordings.</p>
            <button
              data-testid={`recordings-picker-connect-${platform}`}
              onClick={handleConnect}
              style={primaryButtonStyle}
            >
              Connect {meta.label}
            </button>
          </div>
        )}

        {connected && (
          <>
            <section data-testid="recordings-picker-filters" style={filtersStyle}>
              <input
                type="search"
                data-testid="recordings-picker-search"
                placeholder="Search recordings..."
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
              <button
                data-testid="recordings-picker-load"
                onClick={handleLoad}
                disabled={loadDisabled}
                style={primaryButtonStyle}
              >
                {loadLabel}
              </button>
            </section>

            {loadError && (
              <div data-testid="recordings-picker-error" style={errorStyle}>
                Failed to load: {String(loadError)}{' '}
                <button data-testid="recordings-picker-retry" onClick={handleLoad} style={inlineLinkStyle}>
                  Retry
                </button>
              </div>
            )}

            {!hasLoaded && !loadError && (
              <p data-testid="recordings-picker-preload" style={mutedStyle}>
                Choose a date range and click Load recordings.
              </p>
            )}

            {hasLoaded && !loading && !loadError && visible.length === 0 && (
              <p data-testid="recordings-picker-empty" style={mutedStyle}>
                No recordings found for {meta.label} in this range. Try widening the date range.
              </p>
            )}

            {hasLoaded && visible.length > 0 && (
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
          </>
        )}

        <footer data-testid="recordings-picker-footer" style={footerStyle}>
          <button data-testid="recordings-picker-cancel" onClick={onClose} style={secondaryButtonStyle}>
            Cancel
          </button>
          <button
            data-testid="recordings-picker-import"
            onClick={beginImport}
            disabled={selected.size === 0 || importing || !connected}
            style={primaryButtonStyle}
          >
            {`Import ${selected.size} recording${selected.size === 1 ? '' : 's'}`}
          </button>
          {connected && hasLoaded && selectableIDs.length === 0 && visible.length > 0 && (
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

// Styles
const overlayStyle = {
  position: 'fixed', inset: 0, zIndex: 9999,
  backgroundColor: 'rgba(0, 0, 0, 0.45)',
  display: 'flex', alignItems: 'center', justifyContent: 'center',
  padding: '24px',
}
const dialogStyle = {
  position: 'relative', backgroundColor: '#fff', borderRadius: '8px',
  boxShadow: '0 12px 32px rgba(0,0,0,0.18)',
  width: 'min(640px, 95vw)', maxHeight: '85vh', overflowY: 'auto',
  padding: '20px 24px', display: 'flex', flexDirection: 'column', gap: '12px',
}
const closeXStyle = {
  position: 'absolute', top: '8px', right: '10px',
  width: '28px', height: '28px', border: 'none', background: 'transparent',
  fontSize: '22px', lineHeight: '1', cursor: 'pointer', color: '#555',
  borderRadius: '4px',
}
const headerStyle = { paddingRight: '32px' }
const selectorStyle = { display: 'flex', gap: '0', border: '1px solid #ddd', borderRadius: '6px', overflow: 'hidden', padding: 0 }
const segmentStyle = (active) => ({
  flex: 1,
  padding: '8px 12px',
  fontSize: '13px',
  fontWeight: active ? 600 : 400,
  border: 'none',
  borderRight: '1px solid #ddd',
  background: active ? '#e3f2fd' : '#fff',
  color: active ? '#1976d2' : '#444',
  cursor: 'pointer',
  display: 'inline-flex',
  alignItems: 'center',
  justifyContent: 'center',
  gap: '6px',
})
const statusDotStyle = (connected) => ({
  display: 'inline-block',
  width: '8px',
  height: '8px',
  borderRadius: '50%',
  backgroundColor: connected ? '#4caf50' : '#bbb',
})
const accountStyle = { fontSize: '12px', color: '#666', margin: 0 }
const inlineLinkStyle = { background: 'none', border: 'none', color: '#1976d2', cursor: 'pointer', padding: 0, fontSize: '12px' }
const disconnectedStyle = { display: 'flex', flexDirection: 'column', alignItems: 'center', gap: '8px', padding: '24px 12px', textAlign: 'center' }
const filtersStyle = { display: 'flex', gap: '8px', alignItems: 'center', flexWrap: 'wrap' }
const inputStyle = { padding: '6px 8px', fontSize: '13px', border: '1px solid #ccc', borderRadius: '4px' }
const mutedStyle = { fontSize: '12px', color: '#666', margin: 0 }
const mutedSmallStyle = { fontSize: '11px', color: '#888' }
const errorStyle = { fontSize: '13px', color: '#c62828', margin: 0 }
const listStyle = { listStyle: 'none', padding: 0, margin: 0, display: 'flex', flexDirection: 'column', gap: '4px', maxHeight: '40vh', overflowY: 'auto' }
const rowStyle = (disabled) => ({
  display: 'flex', alignItems: 'center', gap: '8px',
  padding: '6px 8px', borderRadius: '4px',
  backgroundColor: disabled ? '#f5f5f5' : 'transparent',
  opacity: disabled ? 0.6 : 1,
})
const titleStyle = { fontSize: '13px', display: 'inline-flex', alignItems: 'center', gap: '6px' }
const footerStyle = { display: 'flex', gap: '8px', alignItems: 'center', justifyContent: 'flex-end' }
const primaryButtonStyle = {
  padding: '8px 14px', fontSize: '13px', fontWeight: 500, border: 'none', borderRadius: '4px',
  backgroundColor: '#1976d2', color: '#fff', cursor: 'pointer',
}
const secondaryButtonStyle = {
  padding: '8px 14px', fontSize: '13px', fontWeight: 500, border: '1px solid #ccc', borderRadius: '4px',
  backgroundColor: '#fff', color: '#444', cursor: 'pointer',
}
const confirmStyle = { borderTop: '1px solid #eee', paddingTop: '12px', marginTop: '4px' }
