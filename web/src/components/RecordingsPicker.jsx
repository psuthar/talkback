// SCRUM-421: side-sheet recordings picker. Drives the "Browse Zoom
// recordings" CTA from SCRUM-420's PlatformConnectionTile; SCRUM-XX10
// (Meet) and SCRUM-XX11 (Teams) reuse this same component with a
// different `platform` prop.
//
// The component lists the connected user's recordings from
// GET /api/{platform}/recordings, lets them multi-select, runs a
// confirmation step, then attaches each selected recording via
// POST /api/sessions/{sessionId}/import/{platform}. Already-imported
// recordings (matched by external_recording_id) and oversized recordings
// (> MAX_DURATION_MINUTES, default 240) are disabled. Cap-exceeded (429)
// shows a clear error and aborts the batch.
import { useCallback, useEffect, useMemo, useState } from 'react'

const MAX_DURATION_MINUTES = 240 // 4 hours; mirrors SCRUM-421 spec default

const PLATFORM_META = {
  zoom: {
    label: 'Zoom',
    listPath: (q) => `/api/zoom/recordings${q ? `?${q}` : ''}`,
    attachPath: (sessionId) => `/api/sessions/${sessionId}/import/zoom`,
    buildAttachBody: (r) => ({ meeting_uuid: r.meeting_uuid, instance_uuid: r.instance_uuid || r.meeting_uuid }),
    transcriptReadyCopy: 'Zoom transcript ready',
    transcriptMissingCopy: 'No native transcript — we’ll transcribe',
  },
  google_meet: {
    label: 'Google Meet',
    listPath: (q) => `/api/google-meet/recordings${q ? `?${q}` : ''}`,
    attachPath: (sessionId) => `/api/sessions/${sessionId}/import/google-meet`,
    buildAttachBody: (r) => ({ conference_record: r.meeting_uuid, recording: r.instance_uuid || r.meeting_uuid }),
    transcriptReadyCopy: 'Gemini transcript ready',
    transcriptMissingCopy: 'No native transcript — we’ll transcribe',
  },
  teams: {
    label: 'Microsoft Teams',
    listPath: (q) => `/api/teams/recordings${q ? `?${q}` : ''}`,
    attachPath: (sessionId) => `/api/sessions/${sessionId}/import/teams`,
    buildAttachBody: (r) => ({ meeting_id: r.meeting_uuid, recording_id: r.instance_uuid || r.meeting_uuid }),
    transcriptReadyCopy: 'Stream transcript ready',
    transcriptMissingCopy: 'No native transcript — we’ll transcribe',
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
  const [transcriptOnly, setTranscriptOnly] = useState(false)
  const [selected, setSelected] = useState(new Set())
  const [confirming, setConfirming] = useState(false)
  const [importing, setImporting] = useState(false)
  const [importErrors, setImportErrors] = useState([])

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
    if (transcriptOnly) params.set('has_transcript', 'true')
    if (query.trim()) params.set('q', query.trim())
    return params.toString()
  }, [rangeKey, customFrom, customTo, transcriptOnly, query])

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

  useEffect(() => {
    refresh()
    // intentionally only on mount and on explicit refresh — filters apply
    // via "Apply filters" button below, not on every keystroke, to avoid
    // hammering the Zoom API.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const visible = useMemo(() => {
    // The backend already filters by q + has_transcript + date range, so
    // visible == recordings. We keep this hook in case the picker wants
    // client-side instant-filter later without re-querying.
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

  return (
    <aside
      data-testid={`recordings-picker-${platform}`}
      role="dialog"
      aria-label={`${meta.label} recordings`}
    >
      <header data-testid="recordings-picker-header">
        <h2>{meta.label} recordings</h2>
        {accountEmail && (
          <p data-testid="recordings-picker-account">
            Connected: {accountEmail}{' '}
            <button data-testid="recordings-picker-switch-account" onClick={onSwitchAccount}>
              Switch account
            </button>
          </p>
        )}
        <button data-testid="recordings-picker-close" onClick={onClose}>Close</button>
      </header>

      <section data-testid="recordings-picker-filters">
        <input
          type="search"
          data-testid="recordings-picker-search"
          placeholder="Search title"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
        <select
          data-testid="recordings-picker-range"
          value={rangeKey}
          onChange={(e) => setRangeKey(e.target.value)}
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
            />
            <input
              type="date"
              data-testid="recordings-picker-to"
              value={customTo}
              onChange={(e) => setCustomTo(e.target.value)}
            />
          </>
        )}
        <label data-testid="recordings-picker-transcript-only-label">
          <input
            type="checkbox"
            data-testid="recordings-picker-transcript-only"
            checked={transcriptOnly}
            onChange={(e) => setTranscriptOnly(e.target.checked)}
          />
          With transcript
        </label>
        <button data-testid="recordings-picker-apply-filters" onClick={refresh} disabled={loading}>
          {loading ? 'Loading…' : 'Apply filters'}
        </button>
      </section>

      {loading && <p data-testid="recordings-picker-loading">Loading recordings…</p>}
      {loadError && (
        <p data-testid="recordings-picker-error">Failed to load: {String(loadError)}</p>
      )}
      {!loading && !loadError && visible.length === 0 && (
        <p data-testid="recordings-picker-empty">No recordings match your filters.</p>
      )}

      {!loading && !loadError && visible.length > 0 && (
        <ul data-testid="recordings-picker-list">
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
              >
                <input
                  type="checkbox"
                  data-testid={`recording-checkbox-${id}`}
                  checked={isSelected}
                  onChange={() => toggleOne(id)}
                  disabled={isDisabled}
                  aria-label={`Select ${r.meeting_topic}`}
                />
                <span data-testid={`recording-title-${id}`}>{r.meeting_topic || '(untitled)'}</span>
                <span data-testid={`recording-time-${id}`}>{r.start_time}</span>
                <span data-testid={`recording-duration-${id}`}>{r.duration_minutes} min</span>
                <span data-testid={`recording-transcript-${id}`}>
                  {r.has_transcript ? meta.transcriptReadyCopy : meta.transcriptMissingCopy}
                </span>
                {isImported && (
                  <span data-testid={`recording-already-imported-${id}`}>Already in this session</span>
                )}
                {isOversized && !isImported && (
                  <span data-testid={`recording-oversized-${id}`}>Over {MAX_DURATION_MINUTES / 60}h — contact support</span>
                )}
              </li>
            )
          })}
        </ul>
      )}

      <footer data-testid="recordings-picker-footer">
        <button
          data-testid="recordings-picker-import"
          onClick={beginImport}
          disabled={selected.size === 0 || importing}
        >
          {`Import ${selected.size} recording${selected.size === 1 ? '' : 's'}`}
        </button>
        <button data-testid="recordings-picker-cancel" onClick={onClose}>
          Cancel
        </button>
        {selectableIDs.length === 0 && visible.length > 0 && (
          <p data-testid="recordings-picker-none-selectable">
            All listed recordings are either already imported or oversized.
          </p>
        )}
      </footer>

      {confirming && (
        <div data-testid="recordings-picker-confirm" role="dialog" aria-label="Confirm import">
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
          >
            {importing ? 'Importing…' : `Import ${selected.size}`}
          </button>
          <button data-testid="recordings-picker-confirm-cancel" onClick={cancelConfirm} disabled={importing}>
            Back
          </button>
        </div>
      )}
    </aside>
  )
}
