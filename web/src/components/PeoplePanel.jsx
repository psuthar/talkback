// SCRUM-424: People panel for speaker reconciliation (Option C). Maps
// raw transcript_segments speaker_labels (Zoom display names, Whisper
// "Speaker N", Meet email, Teams Azure AD name, …) to canonical persons
// scoped to one session.
//
// Reads + writes via the SCRUM-424 API:
//   GET    /api/sessions/{id}/people
//   POST   /api/sessions/{id}/people/aliases
//   DELETE /api/sessions/{id}/people/aliases/{aliasID}
//
// The same canonical_person_id across multiple aliases means "this is the
// same person". The panel surfaces (a) unmapped labels as Person N
// placeholders, (b) mapped groups with the canonical display name +
// optional email, (c) a Merge action to map two-or-more labels into the
// same canonical, (d) an Unmap action to split a label out of a group.
import { useCallback, useEffect, useMemo, useState } from 'react'

function totalSegments(labels) {
  let n = 0
  for (const l of labels) n += l.segment_count || 0
  return n
}

export function PeoplePanel({ sessionId, apiBaseUrl, userEmail }) {
  const [labels, setLabels] = useState([])
  const [aliases, setAliases] = useState([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(null)
  const [selected, setSelected] = useState(new Set())
  const [pendingMergeName, setPendingMergeName] = useState('')
  const [pendingMergeEmail, setPendingMergeEmail] = useState('')
  const [confirmingMerge, setConfirmingMerge] = useState(false)
  const [submitError, setSubmitError] = useState(null)

  const base = useMemo(() => (apiBaseUrl || '').replace(/\/$/, ''), [apiBaseUrl])

  const refresh = useCallback(async () => {
    if (!base || !sessionId) return
    setLoading(true)
    setError(null)
    try {
      const headers = { 'Accept': 'application/json' }
      if (userEmail) headers['X-Creator-Identity'] = userEmail
      const res = await fetch(`${base}/api/sessions/${sessionId}/people`, {
        credentials: 'include',
        headers,
      })
      if (!res.ok) throw new Error(`people GET: ${res.status}`)
      const body = await res.json()
      setLabels(Array.isArray(body.labels) ? body.labels : [])
      setAliases(Array.isArray(body.aliases) ? body.aliases : [])
    } catch (err) {
      setError(err)
    } finally {
      setLoading(false)
    }
  }, [base, sessionId, userEmail])

  useEffect(() => {
    refresh()
  }, [refresh])

  // Map: source_label -> alias row (if any). When the same label has a
  // recording-scoped row AND a session-wide row, recording-scoped wins
  // (matches SCRUM-404 ResolveCanonical precedence).
  const aliasByLabel = useMemo(() => {
    const m = new Map()
    for (const a of aliases) {
      const prev = m.get(a.source_label)
      if (!prev || a.source_recording_id) m.set(a.source_label, a)
    }
    return m
  }, [aliases])

  // Group labels by canonical_person_id. Unmapped labels get their own
  // "Person N" synthetic group.
  const groups = useMemo(() => {
    const totals = totalSegments(labels)
    const out = new Map() // canonical id (or `unmapped:<label>`) -> group
    let unmappedIndex = 0
    for (const l of labels) {
      const a = aliasByLabel.get(l.source_label)
      const groupKey = a ? a.canonical_person_id : `unmapped:${l.source_label}`
      const displayName = a ? a.canonical_display_name : `Person ${++unmappedIndex}`
      const email = a ? a.canonical_email : null
      const aliasID = a ? a.id : null
      let entry = out.get(groupKey)
      if (!entry) {
        entry = {
          key: groupKey,
          canonicalPersonID: a ? a.canonical_person_id : null,
          displayName,
          email,
          labels: [],
          aliasIDs: [],
          segments: 0,
        }
        out.set(groupKey, entry)
      }
      // Merge: prefer the first non-null email / display name seen across
      // any alias in the group so the rendered card always shows the
      // fullest available canonical info.
      if (a && a.canonical_email && !entry.email) entry.email = a.canonical_email
      if (a && a.canonical_display_name && (entry.displayName.startsWith('Person ') || !entry.displayName)) {
        entry.displayName = a.canonical_display_name
      }
      entry.labels.push(l)
      if (aliasID) entry.aliasIDs.push(aliasID)
      entry.segments += l.segment_count || 0
      entry.totalSegments = totals
    }
    return Array.from(out.values())
  }, [labels, aliasByLabel])

  const toggle = (label) => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(label)) next.delete(label)
      else next.add(label)
      return next
    })
  }

  const beginMerge = () => {
    if (selected.size < 2) return
    setSubmitError(null)
    setPendingMergeName('')
    setPendingMergeEmail('')
    setConfirmingMerge(true)
  }

  const performMerge = useCallback(async () => {
    if (!base || !sessionId || pendingMergeName.trim() === '') return
    setSubmitError(null)
    const headers = { 'Content-Type': 'application/json', 'Accept': 'application/json' }
    if (userEmail) headers['X-Creator-Identity'] = userEmail
    let canonicalPersonID
    for (const label of selected) {
      const body = {
        source_label: label,
        canonical_display_name: pendingMergeName.trim(),
      }
      if (pendingMergeEmail.trim()) body.canonical_email = pendingMergeEmail.trim()
      if (canonicalPersonID) body.canonical_person_id = canonicalPersonID
      try {
        const res = await fetch(`${base}/api/sessions/${sessionId}/people/aliases`, {
          method: 'POST',
          credentials: 'include',
          headers,
          body: JSON.stringify(body),
        })
        const respBody = await res.json().catch(() => ({}))
        if (!res.ok) {
          setSubmitError(new Error(respBody.message || `HTTP ${res.status}`))
          return
        }
        if (!canonicalPersonID) canonicalPersonID = respBody.canonical_person_id
      } catch (err) {
        setSubmitError(err)
        return
      }
    }
    setConfirmingMerge(false)
    setSelected(new Set())
    await refresh()
  }, [base, sessionId, selected, pendingMergeName, pendingMergeEmail, userEmail, refresh])

  const cancelMerge = () => setConfirmingMerge(false)

  const unmapGroup = useCallback(async (group) => {
    if (!base || !sessionId || group.aliasIDs.length === 0) return
    setSubmitError(null)
    const headers = { 'Accept': 'application/json' }
    if (userEmail) headers['X-Creator-Identity'] = userEmail
    for (const aliasID of group.aliasIDs) {
      try {
        const res = await fetch(`${base}/api/sessions/${sessionId}/people/aliases/${aliasID}`, {
          method: 'DELETE',
          credentials: 'include',
          headers,
        })
        if (!res.ok && res.status !== 204) {
          const respBody = await res.json().catch(() => ({}))
          setSubmitError(new Error(respBody.message || `HTTP ${res.status}`))
          return
        }
      } catch (err) {
        setSubmitError(err)
        return
      }
    }
    await refresh()
  }, [base, sessionId, userEmail, refresh])

  return (
    <section data-testid="people-panel" role="region" aria-label="People">
      <header>
        <h2>People in this session</h2>
        <button data-testid="people-panel-refresh" onClick={refresh} disabled={loading}>
          {loading ? 'Loading…' : 'Refresh'}
        </button>
      </header>
      {loading && <p data-testid="people-panel-loading">Loading…</p>}
      {error && <p data-testid="people-panel-error">Failed to load: {String(error)}</p>}
      {!loading && !error && labels.length === 0 && (
        <p data-testid="people-panel-empty">No speakers detected yet. Add a recording to see people.</p>
      )}
      {!loading && !error && labels.length > 0 && (
        <ul data-testid="people-panel-groups">
          {groups.map((g) => {
            const pct = g.totalSegments > 0 ? Math.round((g.segments / g.totalSegments) * 100) : 0
            return (
              <li key={g.key} data-testid={`people-group-${g.key}`}>
                <div data-testid={`people-group-name-${g.key}`}>{g.displayName}</div>
                {g.email && <div data-testid={`people-group-email-${g.key}`}>{g.email}</div>}
                <div data-testid={`people-group-airtime-${g.key}`}>{pct}% of session airtime</div>
                <ul data-testid={`people-group-labels-${g.key}`}>
                  {g.labels.map((l) => (
                    <li key={`${g.key}:${l.source_label}`} data-testid={`people-label-${l.source_label}`}>
                      <input
                        type="checkbox"
                        data-testid={`people-label-checkbox-${l.source_label}`}
                        checked={selected.has(l.source_label)}
                        onChange={() => toggle(l.source_label)}
                        aria-label={`Select ${l.source_label}`}
                      />
                      <span data-testid={`people-label-name-${l.source_label}`}>{l.source_label}</span>
                      <span data-testid={`people-label-count-${l.source_label}`}>{l.segment_count} segs</span>
                    </li>
                  ))}
                </ul>
                {g.aliasIDs.length > 0 && (
                  <button data-testid={`people-group-unmap-${g.key}`} onClick={() => unmapGroup(g)}>
                    Unmap
                  </button>
                )}
              </li>
            )
          })}
        </ul>
      )}

      <footer>
        <button
          data-testid="people-panel-merge"
          onClick={beginMerge}
          disabled={selected.size < 2 || loading}
        >
          Mark as same person ({selected.size})
        </button>
        {submitError && (
          <p data-testid="people-panel-submit-error">{String(submitError)}</p>
        )}
      </footer>

      {confirmingMerge && (
        <div data-testid="people-merge-confirm" role="dialog" aria-label="Confirm merge">
          <p>Merging {selected.size} speaker labels into one person.</p>
          <ul>
            {Array.from(selected).map((label) => (
              <li key={label} data-testid={`people-merge-confirm-label-${label}`}>{label}</li>
            ))}
          </ul>
          <label>
            Display name
            <input
              type="text"
              data-testid="people-merge-display-name"
              value={pendingMergeName}
              onChange={(e) => setPendingMergeName(e.target.value)}
            />
          </label>
          <label>
            Email (optional)
            <input
              type="email"
              data-testid="people-merge-email"
              value={pendingMergeEmail}
              onChange={(e) => setPendingMergeEmail(e.target.value)}
            />
          </label>
          <button
            data-testid="people-merge-submit"
            onClick={performMerge}
            disabled={pendingMergeName.trim() === ''}
          >
            Merge
          </button>
          <button data-testid="people-merge-cancel" onClick={cancelMerge}>
            Cancel
          </button>
        </div>
      )}
    </section>
  )
}
