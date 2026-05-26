// SCRUM-579 (Slice 2 of SCRUM-577): admin telemetry surface for
// /api/admin/llm-stats. Sibling to <AdminUsers>; mounts in App.jsx
// under the admin view (?mode=admin + global_role=admin).
//
// What this section answers (priority order):
//  1. What's getting refused right now? (top refusal codes by count)
//  2. Is any guardrail firing way more than expected? (decisions + sites)
//  3. Are we leaking telemetry? (dropped_telemetry_rows = 0 means healthy)
//  4. Is latency healthy? (p95_latency_ms)
//
// Visual shell mirrors the existing AdminUsers / Sessions collapsible
// cards (same border, #e8e8e8 header, rotating ▶ chevron) so the three
// admin sections feel like one family. Charts are pure HTML/CSS bars +
// tables + big-number cards — no chart library install (~70KB saved).
//
// SCRUM-580 (Slice 3) extends this file with a 5th "Token usage" card
// + a Models table once the SCRUM-578 API extension exposes
// `total_input_tokens` / `total_output_tokens` / `by_model`.
import { useState, useEffect, useCallback } from 'react'

// Site enum from internal/guardrails/types.go + qa.go call sites. Pinned
// in code so an empty window still renders the full table (a missing
// site is itself a signal — "qa_grounding_judge has zero rows this
// week" usually means the judge isn't being invoked, which is a bug).
const KNOWN_SITES = [
  'qa_ask',
  'qa_ask_retry_citation',
  'qa_ask_retry_grounding',
  'qa_grounding_judge',
  'action_items',
  'action_items_retry_schema',
  'question_polish',
  'obsworker',
]

// Decision enum from docs/guardrails/log-shape.md. Pinned so the table
// row order is stable across runs and missing keys render as 0.
const DECISIONS = ['allowed', 'refused', 'redacted']

const DAYS_OPTIONS = [1, 7, 30]

function formatThousands(n) {
  if (n === null || n === undefined) return '—'
  return Number(n).toLocaleString('en-US')
}

function formatPercent(num, den) {
  if (!den || den === 0) return '0%'
  return `${((num / den) * 100).toFixed(1)}%`
}

function formatMs(n) {
  if (n === null || n === undefined) return '—'
  return `${Math.round(n).toLocaleString('en-US')} ms`
}

function formatSince(iso) {
  if (!iso) return ''
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

export function AdminGuardrailStats({
  apiBaseUrl,
  guardrailStatsExpanded: expandedProp = false,
  onGuardrailStatsExpandedChange,
}) {
  const isControlled = onGuardrailStatsExpandedChange != null
  const [expandedLocal, setExpandedLocal] = useState(false)
  const expanded = isControlled ? expandedProp : expandedLocal
  const setExpanded = isControlled ? onGuardrailStatsExpandedChange : setExpandedLocal

  const [days, setDays] = useState(7)
  const [stats, setStats] = useState(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const fetchStats = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const res = await fetch(`${apiBaseUrl}/api/admin/llm-stats?days=${days}`, {
        credentials: 'include',
      })
      if (!res.ok) {
        if (res.status === 403) {
          setError('Forbidden: admin access required')
          return
        }
        setError(`Failed to load: ${res.status}`)
        return
      }
      setStats(await res.json())
    } catch (e) {
      setError(e.message || 'Network error')
    } finally {
      setLoading(false)
    }
  }, [apiBaseUrl, days])

  // Fetch on first expand + on days change. Mirrors AdminUsers's
  // sessionsExpanded-gated effect — apiBaseUrl empty-string is valid
  // (relative /api), only null/undefined blocks the fetch.
  useEffect(() => {
    if (expanded && apiBaseUrl != null) fetchStats()
  }, [expanded, apiBaseUrl, fetchStats])

  const total = stats?.total_calls ?? 0
  const isEmpty = stats != null && total === 0
  const refused = stats?.by_decision?.refused ?? 0
  const dropped = stats?.dropped_telemetry_rows ?? 0
  const topRefusals = Array.isArray(stats?.top_refusal_codes) ? stats.top_refusal_codes : []
  const maxRefusalCount = topRefusals.reduce((m, r) => Math.max(m, r.count || 0), 0)
  const byDecision = stats?.by_decision || {}
  const bySite = stats?.by_site || {}

  return (
    <div
      style={{
        marginBottom: '20px',
        border: '1px solid #bbb',
        borderRadius: '8px',
        overflow: 'hidden',
        boxShadow: '0 1px 3px rgba(0,0,0,0.08)',
        margin: '0 0 20px 0',
      }}
    >
      <button
        type="button"
        onClick={() => setExpanded(!expanded)}
        style={{
          width: '100%',
          padding: '16px 20px',
          textAlign: 'left',
          fontSize: '18px',
          fontWeight: 700,
          border: 'none',
          background: '#e8e8e8',
          color: '#1a1a1a',
          cursor: 'pointer',
          display: 'flex',
          alignItems: 'center',
          gap: '10px',
        }}
      >
        <span
          style={{
            transform: expanded ? 'rotate(90deg)' : 'none',
            display: 'inline-block',
            fontSize: '14px',
          }}
        >
          ▶
        </span>
        Guardrail telemetry
      </button>
      {expanded && (
        <div style={{ padding: '16px', borderTop: '1px solid #ddd' }}>
          {/* Window selector + Refresh */}
          <div
            style={{
              display: 'flex',
              flexWrap: 'wrap',
              alignItems: 'center',
              gap: '12px',
              marginBottom: '16px',
            }}
          >
            <span style={{ fontWeight: 600 }}>Window:</span>
            <div role="group" aria-label="Days window" style={{ display: 'inline-flex', gap: '4px' }}>
              {DAYS_OPTIONS.map((d) => {
                const active = d === days
                return (
                  <button
                    key={d}
                    type="button"
                    onClick={() => setDays(d)}
                    style={{
                      padding: '6px 12px',
                      fontSize: '13px',
                      border: '1px solid #bbb',
                      borderRadius: '4px',
                      background: active ? '#1a73e8' : '#fff',
                      color: active ? '#fff' : '#1a1a1a',
                      cursor: 'pointer',
                      fontWeight: active ? 600 : 400,
                    }}
                  >
                    {d}d
                  </button>
                )
              })}
            </div>
            <span style={{ flex: 1, fontSize: '12px', color: '#555' }}>
              since {formatSince(stats?.since)}
            </span>
            <button
              type="button"
              onClick={fetchStats}
              disabled={loading}
              style={{ padding: '6px 12px', fontSize: '13px' }}
            >
              {loading ? 'Loading…' : '↻ Refresh'}
            </button>
          </div>

          {loading && <p>Loading guardrail telemetry…</p>}
          {error && <p className="error">{error}</p>}

          {!loading && !error && stats && (
            <>
              {/* Big-number cards */}
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: '12px', marginBottom: '20px' }}>
                <BigNumberCard label="Total calls" value={formatThousands(total)} />
                <BigNumberCard
                  label="p95 latency"
                  value={formatMs(stats.p95_latency_ms)}
                  subtitle={stats.p95_latency_ms == null ? 'no calls in window' : null}
                />
                <BigNumberCard
                  label="Dropped rows"
                  value={formatThousands(dropped)}
                  subtitle={dropped === 0 ? '(good)' : `(${dropped} dropped — buffer pressure)`}
                  subtitleColor={dropped === 0 ? '#0a7f2e' : '#b30000'}
                />
                <BigNumberCard
                  label="Refused"
                  value={formatThousands(refused)}
                  subtitle={`(${formatPercent(refused, total)} of total)`}
                />
              </div>

              {isEmpty && (
                <p className="info" style={{ marginTop: '8px', fontStyle: 'italic' }}>
                  No LLM calls recorded in this window.
                </p>
              )}

              {!isEmpty && (
                <>
                  {/* Top refusal codes */}
                  <h3 style={{ marginTop: '16px', marginBottom: '8px', fontSize: '15px' }}>
                    Top refusal codes (last {days}d)
                  </h3>
                  {topRefusals.length === 0 ? (
                    <p className="info">—</p>
                  ) : (
                    <div style={{ marginBottom: '20px' }}>
                      {topRefusals.map((r) => {
                        const pct = maxRefusalCount > 0 ? (r.count / maxRefusalCount) * 100 : 0
                        return (
                          <div
                            key={r.code}
                            style={{
                              display: 'flex',
                              alignItems: 'center',
                              gap: '8px',
                              marginBottom: '6px',
                            }}
                          >
                            <span
                              style={{
                                fontFamily: 'monospace',
                                fontSize: '12px',
                                minWidth: '200px',
                                color: '#333',
                              }}
                            >
                              {r.code}
                            </span>
                            <div
                              style={{
                                flex: 1,
                                background: '#f0f0f0',
                                height: '18px',
                                borderRadius: '2px',
                                overflow: 'hidden',
                                minWidth: '200px',
                              }}
                            >
                              <div
                                data-testid={`bar-${r.code}`}
                                style={{
                                  width: `${pct}%`,
                                  height: '100%',
                                  background: '#1a73e8',
                                }}
                              />
                            </div>
                            <span style={{ fontSize: '13px', minWidth: '40px', textAlign: 'right' }}>
                              {r.count}
                            </span>
                          </div>
                        )
                      })}
                    </div>
                  )}

                  {/* Decisions + Sites side-by-side */}
                  <div style={{ display: 'flex', flexWrap: 'wrap', gap: '24px' }}>
                    <div style={{ flex: '1 1 280px', minWidth: '260px' }}>
                      <h3 style={{ marginTop: 0, marginBottom: '8px', fontSize: '15px' }}>Decisions</h3>
                      <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '13px' }}>
                        <tbody>
                          {DECISIONS.map((d) => (
                            <tr key={d} style={{ borderBottom: '1px solid #eee' }}>
                              <td style={{ padding: '6px 8px', fontFamily: 'monospace' }}>{d}</td>
                              <td style={{ padding: '6px 8px', textAlign: 'right' }}>
                                {formatThousands(byDecision[d] ?? 0)}
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                    <div style={{ flex: '1 1 280px', minWidth: '260px' }}>
                      <h3 style={{ marginTop: 0, marginBottom: '8px', fontSize: '15px' }}>Sites</h3>
                      <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '13px' }}>
                        <tbody>
                          {KNOWN_SITES.map((s) => (
                            <tr key={s} style={{ borderBottom: '1px solid #eee' }}>
                              <td style={{ padding: '6px 8px', fontFamily: 'monospace' }}>{s}</td>
                              <td style={{ padding: '6px 8px', textAlign: 'right' }}>
                                {formatThousands(bySite[s] ?? 0)}
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  </div>
                </>
              )}
            </>
          )}
        </div>
      )}
    </div>
  )
}

function BigNumberCard({ label, value, subtitle, subtitleColor }) {
  return (
    <div
      style={{
        flex: '1 1 180px',
        minWidth: '160px',
        maxWidth: '240px',
        padding: '12px 16px',
        border: '1px solid #ddd',
        borderRadius: '6px',
        background: '#f9f9f9',
      }}
    >
      <div style={{ fontSize: '12px', color: '#555', fontWeight: 600, marginBottom: '4px' }}>{label}</div>
      <div style={{ fontSize: '24px', fontWeight: 700, color: '#1a1a1a' }}>{value}</div>
      {subtitle && (
        <div style={{ fontSize: '11px', color: subtitleColor || '#555', marginTop: '4px' }}>
          {subtitle}
        </div>
      )}
    </div>
  )
}
