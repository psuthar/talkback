import { useEffect, useMemo, useRef, useState } from 'react'

/**
 * SCRUM-354: Creator can start a template-driven session import and track
 * status to completion.
 * SCRUM-356: Per-element failure UX — summary callout, separate failed and
 * succeeded sections, friendly error messages, URL surfaced inline.
 *
 * The page polls GET /api/import-jobs/:id every 2 seconds while the job
 * is queued or running. On terminal state, polling stops.
 */
export function ImportTemplatePage({ apiBaseUrl, authUser }) {
  const [descriptor, setDescriptor] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [submitErrors, setSubmitErrors] = useState(null) // ValidationError[] from POST
  const [job, setJob] = useState(null) // ImportJob from GET
  const [parsedDescriptor, setParsedDescriptor] = useState(null) // cached at submit time
  const pollRef = useRef(null)

  const apiBase = (apiBaseUrl || '').replace(/\/$/, '')

  // Map of element id → element record from the parsed descriptor. Used to
  // surface the URL/title alongside the per-element status (the import job
  // doesn't carry that detail back from the server).
  const elementsByID = useMemo(() => {
    const m = {}
    if (parsedDescriptor && Array.isArray(parsedDescriptor.elements)) {
      for (const el of parsedDescriptor.elements) {
        if (el && el.id) m[el.id] = el
      }
    }
    return m
  }, [parsedDescriptor])

  useEffect(() => {
    return () => {
      if (pollRef.current) {
        clearInterval(pollRef.current)
        pollRef.current = null
      }
    }
  }, [])

  async function pollOnce(jobId) {
    try {
      const resp = await fetch(`${apiBase}/api/import-jobs/${jobId}`, { credentials: 'include' })
      if (!resp.ok) return
      const j = await resp.json()
      setJob(j)
      if (j.state === 'succeeded' || j.state === 'partial' || j.state === 'failed') {
        if (pollRef.current) {
          clearInterval(pollRef.current)
          pollRef.current = null
        }
      }
    } catch (_) {
      // transient — keep polling
    }
  }

  async function handleSubmit() {
    if (!descriptor.trim()) {
      setSubmitErrors([{ reason_code: 'missing_required', message: 'Paste a template descriptor first' }])
      return
    }
    setSubmitting(true)
    setSubmitErrors(null)
    setJob(null)
    // Parse the descriptor locally so the failure UX can show URLs alongside
    // element ids. Validation is server-side authoritative; this parse is
    // best-effort for display only.
    try {
      setParsedDescriptor(JSON.parse(descriptor))
    } catch (_) {
      setParsedDescriptor(null)
    }
    try {
      const resp = await fetch(`${apiBase}/api/import-jobs`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: descriptor,
      })
      const data = await resp.json().catch(() => null)
      if (!resp.ok) {
        setSubmitErrors((data && data.errors) || [{ reason_code: 'submit_failed', message: `HTTP ${resp.status}` }])
        return
      }
      const jobId = data && data.job_id
      if (!jobId) {
        setSubmitErrors([{ reason_code: 'submit_failed', message: 'no job id returned' }])
        return
      }
      await pollOnce(jobId)
      pollRef.current = setInterval(() => pollOnce(jobId), 2000)
    } catch (err) {
      setSubmitErrors([{ reason_code: 'submit_failed', message: String(err) }])
    } finally {
      setSubmitting(false)
    }
  }

  const isTerminal = job && (job.state === 'succeeded' || job.state === 'partial' || job.state === 'failed')

  // Partition elements by status for the failure UX. SCRUM-356 acceptance
  // criterion: failed items must be visually distinct and their reasons
  // surfaced.
  const elementSplit = useMemo(() => {
    const failed = []
    const succeeded = []
    const queued = []
    if (job && job.elements_state) {
      for (const [id, st] of Object.entries(job.elements_state)) {
        const row = { id, ...st, descriptor: elementsByID[id] || null }
        if (st.status === 'failed') failed.push(row)
        else if (st.status === 'succeeded') succeeded.push(row)
        else queued.push(row)
      }
    }
    return { failed, succeeded, queued }
  }, [job, elementsByID])

  return (
    <div data-testid="import-template-page" style={{ maxWidth: 900, margin: '0 auto', padding: '24px' }}>
      <h1>Import session from template</h1>
      <p style={{ color: '#666' }}>
        Paste a v1 template descriptor JSON. The descriptor will be validated, every URL
        preflight-checked, then a session created from the imported content.
      </p>
      {!authUser && (
        <p style={{ color: '#b00', fontWeight: 600 }}>
          You must be signed in as a creator or admin to import a template.
        </p>
      )}
      <textarea
        data-testid="import-template-descriptor"
        value={descriptor}
        onChange={(e) => setDescriptor(e.target.value)}
        rows={16}
        spellCheck={false}
        style={{ width: '100%', fontFamily: 'monospace', fontSize: 13, padding: 12, boxSizing: 'border-box' }}
        placeholder='{"version": 1, "title": "...", "elements": [...]}'
      />
      <div style={{ marginTop: 12 }}>
        <button
          data-testid="import-template-submit"
          onClick={handleSubmit}
          disabled={submitting || !authUser}
          style={{ padding: '8px 16px', fontSize: 14 }}
        >
          {submitting ? 'Submitting…' : 'Start import'}
        </button>
      </div>

      {submitErrors && submitErrors.length > 0 && (
        <div data-testid="import-template-errors" style={{ marginTop: 16, padding: 12, background: '#fef2f2', border: '1px solid #fecaca', borderRadius: 4 }}>
          <strong>Validation errors</strong>
          <ul style={{ marginTop: 8 }}>
            {submitErrors.map((e, i) => (
              <li key={i} data-testid="import-template-error-item">
                <ErrorLine err={e} />
              </li>
            ))}
          </ul>
        </div>
      )}

      {job && (
        <div data-testid="import-template-job-state" style={{ marginTop: 16, padding: 12, border: '1px solid #ddd', borderRadius: 4 }}>
          <p>
            Job <code>{job.id}</code> — state: <strong data-testid="import-template-job-status">{job.state}</strong>
          </p>
          {job.session_id && (
            <p>
              Session: <a href={`/?session=${job.session_id}&mode=view`} data-testid="import-template-session-link">{job.session_id}</a>
            </p>
          )}
          {job.error_message && (
            <p style={{ color: '#b00' }}>Error: {job.error_message}</p>
          )}

          {/* SCRUM-356: failure summary callout above the per-element list. */}
          {isTerminal && (elementSplit.failed.length > 0 || elementSplit.succeeded.length > 0) && (
            <FailureSummary
              failed={elementSplit.failed.length}
              succeeded={elementSplit.succeeded.length}
              total={elementSplit.failed.length + elementSplit.succeeded.length + elementSplit.queued.length}
            />
          )}

          {/* Failed-elements section (rendered first to draw attention). */}
          {elementSplit.failed.length > 0 && (
            <ElementSection
              title={`Failed (${elementSplit.failed.length})`}
              tone="error"
              testid="import-template-failed-section"
              rows={elementSplit.failed}
            />
          )}

          {/* Succeeded-elements section. */}
          {elementSplit.succeeded.length > 0 && (
            <ElementSection
              title={`Imported (${elementSplit.succeeded.length})`}
              tone="ok"
              testid="import-template-succeeded-section"
              rows={elementSplit.succeeded}
            />
          )}

          {/* Queued / running rows during a live job. */}
          {elementSplit.queued.length > 0 && (
            <ElementSection
              title={`In progress (${elementSplit.queued.length})`}
              tone="muted"
              testid="import-template-queued-section"
              rows={elementSplit.queued}
            />
          )}

          {!isTerminal && <p style={{ color: '#666', marginTop: 8 }}>Polling every 2 seconds…</p>}
        </div>
      )}
    </div>
  )
}

function FailureSummary({ failed, succeeded, total }) {
  const ok = failed === 0
  const tone = ok ? '#0a7d2c' : '#a14400'
  const bg = ok ? '#f0fdf4' : '#fff7ed'
  const border = ok ? '#bbf7d0' : '#fed7aa'
  return (
    <div
      data-testid="import-template-failure-summary"
      style={{
        marginTop: 12,
        marginBottom: 12,
        padding: 12,
        borderRadius: 4,
        background: bg,
        border: `1px solid ${border}`,
        color: tone,
        fontWeight: 600,
      }}
    >
      {ok ? (
        <>All {succeeded} of {total} elements imported successfully.</>
      ) : (
        <>{failed} of {total} elements failed to import. {succeeded} succeeded.</>
      )}
    </div>
  )
}

function ElementSection({ title, tone, testid, rows }) {
  return (
    <div data-testid={testid} style={{ marginTop: 12 }}>
      <h3 style={{ fontSize: 14, marginBottom: 6, color: tone === 'error' ? '#b00' : tone === 'muted' ? '#666' : '#0a7d2c' }}>{title}</h3>
      <table style={{ width: '100%', borderCollapse: 'collapse' }}>
        <thead>
          <tr style={{ background: '#f5f5f5' }}>
            <th style={cellStyle}>Element</th>
            <th style={cellStyle}>URL / Title</th>
            <th style={cellStyle}>Detail</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <ElementRow key={row.id} row={row} tone={tone} />
          ))}
        </tbody>
      </table>
    </div>
  )
}

function ElementRow({ row, tone }) {
  const failed = row.status === 'failed'
  const url = row.descriptor && (row.descriptor.url || row.descriptor.embed_url || row.descriptor.video_url || row.descriptor.text_url || row.descriptor.segments_url)
  const title = row.descriptor && row.descriptor.title
  const friendly = friendlyMessage(row.error_code)
  return (
    <tr
      data-testid="import-template-element-row"
      style={{
        opacity: failed ? 0.7 : 1,
        background: failed ? '#fff5f5' : 'transparent',
        textDecoration: failed ? 'line-through' : 'none',
      }}
    >
      <td style={cellStyle}>
        <code>{row.id}</code>
        {row.descriptor && row.descriptor.kind ? (
          <span style={{ marginLeft: 8, fontSize: 11, color: '#666' }}>({row.descriptor.kind})</span>
        ) : null}
      </td>
      <td style={cellStyle}>
        {title ? <div style={{ fontWeight: 500 }}>{title}</div> : null}
        {url ? <div style={{ fontSize: 11, color: '#666', wordBreak: 'break-all' }}>{url}</div> : null}
      </td>
      <td style={cellStyle}>
        {failed ? (
          <div>
            <div style={{ color: '#b00', fontWeight: 600 }}>{friendly}</div>
            {row.error_code ? <code style={{ fontSize: 11 }}>{row.error_code}</code> : null}
            {row.message ? <div style={{ fontSize: 11, color: '#666', marginTop: 2 }}>{row.message}</div> : null}
          </div>
        ) : (
          <span style={{ color: tone === 'muted' ? '#666' : '#0a7d2c' }}>{row.status}</span>
        )}
      </td>
    </tr>
  )
}

function ErrorLine({ err }) {
  const friendly = friendlyMessage(err.reason_code)
  return (
    <span>
      <strong>{friendly}</strong>{' '}
      <code style={{ fontSize: 11 }}>{err.reason_code}</code>
      {err.element_id ? <> on <code>{err.element_id}</code></> : null}
      {err.declared_type ? <> [declared <code>{err.declared_type}</code></> : null}
      {err.observed_type ? <>, observed <code>{err.observed_type}</code>]</> : err.declared_type ? <>]</> : null}
      {err.message ? <>: {err.message}</> : null}
    </span>
  )
}

// friendlyMessage maps validator/preflight reason_codes to short
// human-readable strings for the failure UX. SCRUM-351 + SCRUM-352 catalogs.
function friendlyMessage(code) {
  switch (code) {
    case 'unknown_field': return 'Unrecognized field'
    case 'missing_required': return 'Required field missing'
    case 'version_unsupported': return 'Unsupported template version'
    case 'title_too_long': return 'Title too long'
    case 'element_id_invalid': return 'Element id format invalid'
    case 'element_id_duplicate': return 'Duplicate element id'
    case 'element_kind_unknown': return 'Unknown element kind'
    case 'material_content_type_unsupported': return 'Material content-type not allowed'
    case 'url_scheme_invalid': return 'URL must use https://'
    case 'transcript_source_duplicate': return 'Duplicate transcript source'
    case 'primary_ref_unknown': return 'Primary content reference points to a missing element'
    case 'primary_kind_mismatch': return 'Primary kind does not match referenced element'
    case 'video_source_url_missing': return 'Video source needs an embed_url or video_url'
    case 'transcript_url_missing': return 'Transcript needs a text_url or segments_url'
    case 'provider_unsupported': return 'Video provider not allowed'
    case 'descriptor_too_large': return 'Template descriptor too large'
    case 'too_many_elements': return 'Too many elements in template'
    // Preflight reason codes
    case 'url_unreachable': return 'Could not reach the URL'
    case 'content_type_mismatch': return 'Content-type does not match what was declared'
    case 'body_too_large': return 'Response body exceeds the size cap'
    case 'redirect_loop': return 'Too many redirects'
    case 'auth_required': return 'URL requires authentication (not supported)'
    case 'private_ip_blocked': return 'URL resolves to a private/internal address'
    case 'http_status_error': return 'Server returned an error status'
    case 'request_timeout': return 'Request timed out'
    default: return 'Failed to import'
  }
}

const cellStyle = { padding: '6px 8px', borderBottom: '1px solid #eee', textAlign: 'left', verticalAlign: 'top' }
