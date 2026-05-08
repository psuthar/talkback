import { useEffect, useRef, useState } from 'react'

/**
 * SCRUM-354: Creator can start a template-driven session import and track
 * status to completion.
 *
 * Minimal first-class UX: paste a v1 template descriptor JSON, submit it,
 * and watch the per-element status until the job reaches a terminal state
 * (succeeded / partial / failed). On a successful or partial outcome a
 * link to the imported session is shown.
 *
 * The page polls GET /api/import-jobs/:id every 2 seconds while the job
 * is queued or running. On terminal state, polling stops.
 */
export function ImportTemplatePage({ apiBaseUrl, authUser }) {
  const [descriptor, setDescriptor] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [submitErrors, setSubmitErrors] = useState(null) // ValidationError[] from POST
  const [job, setJob] = useState(null) // ImportJob from GET
  const pollRef = useRef(null)

  const apiBase = (apiBaseUrl || '').replace(/\/$/, '')

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
      // Kick off polling.
      await pollOnce(jobId)
      pollRef.current = setInterval(() => pollOnce(jobId), 2000)
    } catch (err) {
      setSubmitErrors([{ reason_code: 'submit_failed', message: String(err) }])
    } finally {
      setSubmitting(false)
    }
  }

  const isTerminal = job && (job.state === 'succeeded' || job.state === 'partial' || job.state === 'failed')

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
                <code>{e.reason_code}</code>
                {e.element_id ? ` (${e.element_id})` : ''}: {e.message}
                {e.declared_type ? ` [declared: ${e.declared_type}` : ''}
                {e.observed_type ? `, observed: ${e.observed_type}]` : (e.declared_type ? ']' : '')}
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
          {job.elements_state && Object.keys(job.elements_state).length > 0 && (
            <table style={{ width: '100%', marginTop: 8, borderCollapse: 'collapse' }}>
              <thead>
                <tr style={{ background: '#f5f5f5' }}>
                  <th style={cellStyle}>Element</th>
                  <th style={cellStyle}>Status</th>
                  <th style={cellStyle}>Detail</th>
                </tr>
              </thead>
              <tbody>
                {Object.entries(job.elements_state).map(([id, st]) => (
                  <tr
                    key={id}
                    data-testid="import-template-element-row"
                    style={{
                      // SCRUM-356 hint: failed elements render grayed-out so the creator can see
                      // immediately which references didn't import. Background tint matches the
                      // colors used elsewhere in TalkBack for failure states.
                      opacity: st.status === 'failed' ? 0.55 : 1,
                      background: st.status === 'failed' ? '#fff5f5' : 'transparent',
                    }}
                  >
                    <td style={cellStyle}><code>{id}</code></td>
                    <td style={cellStyle}>{st.status}</td>
                    <td style={cellStyle}>
                      {st.error_code ? <code>{st.error_code}</code> : ''}
                      {st.message ? ` ${st.message}` : ''}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
          {!isTerminal && <p style={{ color: '#666', marginTop: 8 }}>Polling every 2 seconds…</p>}
        </div>
      )}
    </div>
  )
}

const cellStyle = { padding: '6px 8px', borderBottom: '1px solid #eee', textAlign: 'left' }
