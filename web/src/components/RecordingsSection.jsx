// SCRUM-426: Recordings section for the session-detail Materials tab.
// Renders one row per video_source with a source-platform badge,
// transcript-status pill, primary-recording indicator (★), and a kebab
// menu that fires Make primary (SCRUM-412 PATCH endpoint), Remove
// (SCRUM-426 DELETE endpoint), and Open original (deep-link).
//
// Sort: recording start time ascending (`created_at` on video_sources);
// uploaded MP4s sort by upload time within the group.
//
// In-progress imports collapse into a summary at the top of the section
// ("N imports in progress — view details"); clicking the summary expands
// each in-progress row so users can still see the per-recording state
// pill.
import { useCallback, useMemo, useState } from 'react'

const PLATFORM_LABELS = {
  zoom: 'Zoom',
  google_meet: 'Google Meet',
  teams: 'Microsoft Teams',
  other: 'Uploaded',
  loom: 'Loom',
}

function statusPill(vs) {
  // transcript_status is the canonical signal exposed today: missing /
  // pending / ready / failed. "Transcribing (fallback)" piggybacks off the
  // transcription_source field — when a Whisper job is in flight the
  // backend sets transcription_source=whisper and transcript_status=pending.
  if (vs.transcript_status === 'ready') return { label: 'Transcript ready', state: 'ready' }
  if (vs.transcript_status === 'failed') return { label: 'Transcript failed', state: 'failed' }
  if (vs.transcription_source === 'whisper' && vs.transcript_status === 'pending') {
    return { label: 'Transcribing (Whisper fallback)', state: 'transcribing' }
  }
  if (vs.transcript_status === 'pending') return { label: 'Transcribing', state: 'transcribing' }
  return { label: 'No transcript', state: 'missing' }
}

function platformLabel(provider) {
  return PLATFORM_LABELS[provider] || provider || 'Unknown'
}

function openOriginalURL(vs) {
  return vs.original_url || vs.video_url || null
}

function recordingsByStart(list) {
  return [...list].sort((a, b) => {
    const aT = a.created_at || ''
    const bT = b.created_at || ''
    if (aT < bT) return -1
    if (aT > bT) return 1
    return 0
  })
}

function isInProgress(vs) {
  return statusPill(vs).state === 'transcribing'
}

export function RecordingsSection({
  sessionId,
  apiBaseUrl,
  recordings,
  userEmail,
  onChanged,
}) {
  const sorted = useMemo(() => recordingsByStart(recordings || []), [recordings])
  const inProgress = sorted.filter(isInProgress)
  const [showInProgressDetail, setShowInProgressDetail] = useState(false)
  const [openKebab, setOpenKebab] = useState(null)
  const [busy, setBusy] = useState(false)
  const [actionError, setActionError] = useState(null)

  const base = useMemo(() => (apiBaseUrl || '').replace(/\/$/, ''), [apiBaseUrl])

  const refreshHeaders = useMemo(() => {
    const h = { 'Content-Type': 'application/json', 'Accept': 'application/json' }
    if (userEmail) h['X-Creator-Identity'] = userEmail
    return h
  }, [userEmail])

  const makePrimary = useCallback(async (vsID) => {
    if (!base || !sessionId) return
    setActionError(null)
    setBusy(true)
    try {
      const res = await fetch(`${base}/api/sessions/${sessionId}/primary-recording`, {
        method: 'PATCH',
        credentials: 'include',
        headers: refreshHeaders,
        body: JSON.stringify({ video_source_id: vsID }),
      })
      if (!res.ok) {
        const body = await res.json().catch(() => ({}))
        throw new Error(body.message || `HTTP ${res.status}`)
      }
      if (typeof onChanged === 'function') await onChanged()
    } catch (err) {
      setActionError(err)
    } finally {
      setBusy(false)
      setOpenKebab(null)
    }
  }, [base, sessionId, refreshHeaders, onChanged])

  const remove = useCallback(async (vsID) => {
    if (!base || !sessionId) return
    setActionError(null)
    setBusy(true)
    try {
      const res = await fetch(`${base}/api/sessions/${sessionId}/video-sources/${vsID}`, {
        method: 'DELETE',
        credentials: 'include',
        headers: refreshHeaders,
      })
      if (!res.ok && res.status !== 204) {
        const body = await res.json().catch(() => ({}))
        throw new Error(body.message || `HTTP ${res.status}`)
      }
      if (typeof onChanged === 'function') await onChanged()
    } catch (err) {
      setActionError(err)
    } finally {
      setBusy(false)
      setOpenKebab(null)
    }
  }, [base, sessionId, refreshHeaders, onChanged])

  if (sorted.length === 0) {
    return (
      <section data-testid="recordings-section" role="region" aria-label="Recordings">
        <header><h3>Recordings</h3></header>
        <p data-testid="recordings-section-empty">No recordings attached to this session yet.</p>
      </section>
    )
  }

  return (
    <section data-testid="recordings-section" role="region" aria-label="Recordings">
      <header><h3>Recordings ({sorted.length})</h3></header>
      {inProgress.length > 0 && !showInProgressDetail && (
        <button
          data-testid="recordings-in-progress-summary"
          onClick={() => setShowInProgressDetail(true)}
        >
          {inProgress.length} import{inProgress.length === 1 ? '' : 's'} in progress — view details
        </button>
      )}
      <ul data-testid="recordings-section-list">
        {sorted.map((vs) => {
          const pill = statusPill(vs)
          const isPrimary = vs.video_role === 'primary'
          const original = openOriginalURL(vs)
          return (
            <li
              key={vs.id}
              data-testid={`recording-row-${vs.id}`}
              data-state={pill.state}
              data-primary={isPrimary ? 'true' : 'false'}
            >
              <span data-testid={`recording-row-${vs.id}-source-badge`}>{platformLabel(vs.provider)}</span>
              <span data-testid={`recording-row-${vs.id}-title`}>{vs.display_title || vs.video_url || '(untitled)'}</span>
              {typeof vs.duration_seconds === 'number' && (
                <span data-testid={`recording-row-${vs.id}-duration`}>{Math.round(vs.duration_seconds / 60)} min</span>
              )}
              <span data-testid={`recording-row-${vs.id}-status-pill`}>{pill.label}</span>
              {isPrimary && (
                <span data-testid={`recording-row-${vs.id}-primary-indicator`} aria-label="Primary recording">★</span>
              )}
              <button
                data-testid={`recording-row-${vs.id}-kebab`}
                aria-label="Recording actions"
                aria-haspopup="menu"
                aria-expanded={openKebab === vs.id}
                onClick={() => setOpenKebab(openKebab === vs.id ? null : vs.id)}
                disabled={busy}
              >
                ⋯
              </button>
              {openKebab === vs.id && (
                <ul data-testid={`recording-row-${vs.id}-kebab-menu`} role="menu">
                  {!isPrimary && (
                    <li role="menuitem">
                      <button
                        data-testid={`recording-row-${vs.id}-make-primary`}
                        onClick={() => makePrimary(vs.id)}
                        disabled={busy}
                      >
                        Make primary
                      </button>
                    </li>
                  )}
                  <li role="menuitem">
                    <button
                      data-testid={`recording-row-${vs.id}-remove`}
                      onClick={() => remove(vs.id)}
                      disabled={busy}
                    >
                      Remove
                    </button>
                  </li>
                  {original && (
                    <li role="menuitem">
                      <a
                        data-testid={`recording-row-${vs.id}-open-original`}
                        href={original}
                        target="_blank"
                        rel="noopener noreferrer"
                      >
                        Open original
                      </a>
                    </li>
                  )}
                </ul>
              )}
            </li>
          )
        })}
      </ul>
      {actionError && (
        <p data-testid="recordings-section-error">{String(actionError)}</p>
      )}
    </section>
  )
}

// SourceBadge is exported separately so QAHistory / decisions citation
// chips can render the same per-recording badge alongside transcript
// citations (matches "Zoom · Standup 5/12 · 12:34" copy spec).
export function SourceBadge({ provider }) {
  return (
    <span data-testid={`source-badge-${provider}`} aria-label={`Source: ${platformLabel(provider)}`}>
      {platformLabel(provider)}
    </span>
  )
}
