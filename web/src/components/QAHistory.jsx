import { useState, useEffect, useRef } from 'react'

function CitationBadge({ citation, onClick }) {
  const label = citation.label || (citation.citation_id ? `[${citation.citation_id}]` : citation.source_type)
  const canNavigate = citation.anchor?.start_ms != null ||
    citation?.source_type === 'material' ||
    (citation.navigation && (citation.navigation.type === 'video' || citation.navigation.type === 'pdf' || citation.navigation.type === 'doc'))
  return (
    <button
      type="button"
      onClick={onClick}
      title={citation.excerpt || citation.snippet || 'View source'}
      style={{
        padding: '4px 10px',
        fontSize: '12px',
        backgroundColor: canNavigate ? '#e3f2fd' : '#f5f5f5',
        color: canNavigate ? '#1976D2' : '#555',
        border: `1px solid ${canNavigate ? '#90caf9' : '#e0e0e0'}`,
        borderRadius: '4px',
        cursor: 'pointer',
        fontWeight: 500
      }}
    >
      {citation.citation_id ? `[${citation.citation_id}]` : ''} {label}
    </button>
  )
}

// Find the root question ID for any question (itself if root, or walk up via parent_question_id).
function findRootId(questions, questionId) {
  const byId = Object.fromEntries((questions || []).map((q) => [q.id, q]))
  let q = byId[questionId]
  while (q?.parent_question_id) q = byId[q.parent_question_id]
  return q?.id ?? questionId
}

// Build thread tree: roots have no parent_question_id; replies grouped under parent.
// Top-level questions: newest first (descending by created_at). Nested replies: chronological (ascending).
function buildThreadTree(questions) {
  if (!questions || questions.length === 0) return { roots: [], byParent: {} }
  const byParent = {}
  for (const q of questions) {
    const pid = q.parent_question_id ?? 'root'
    if (!byParent[pid]) byParent[pid] = []
    byParent[pid].push(q)
  }
  const roots = (byParent.root || []).sort((a, b) => new Date(b.created_at) - new Date(a.created_at))
  for (const k of Object.keys(byParent)) {
    if (k !== 'root') byParent[k].sort((a, b) => new Date(a.created_at) - new Date(b.created_at))
  }
  return { roots, byParent }
}

function QACard({ q, onCitationClick, onReply, depth = 0, collapsed = false }) {
  const isReply = depth > 0
  return (
    <div
      key={q.id}
      style={{
        marginBottom: '15px',
        marginLeft: isReply ? '20px' : 0,
        padding: isReply ? '10px 12px' : '15px',
        border: '1px solid #ddd',
        borderRadius: '5px',
        backgroundColor: q.answer ? '#f9f9f9' : '#fff',
        borderLeft: isReply ? '3px solid #90caf9' : undefined
      }}
    >
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: '8px' }}>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ fontWeight: 'bold', marginBottom: '5px', color: '#333', display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap' }}>
            <span>Q: {q.question_text}</span>
            <span style={{ fontSize: '12px', fontWeight: 'normal', color: '#666' }}>
              {q.asked_by ? (
                <>— asked by <strong>{q.asked_by}</strong></>
              ) : (
                <>— asked by <span style={{ color: '#999' }}>—</span></>
              )}
            </span>
          </div>
          <div style={{ fontSize: '11px', color: '#999', marginTop: '5px' }}>
            Asked: {new Date(q.created_at).toLocaleString()}
            {q.video_time_seconds !== null && q.video_time_seconds !== undefined && (
              <span style={{ marginLeft: '10px', color: '#2196F3', fontWeight: 'bold' }}>
                | At {Math.floor(q.video_time_seconds / 60)}:{(q.video_time_seconds % 60).toString().padStart(2, '0')}
              </span>
            )}
          </div>
        </div>
        {onReply && (
          <button
            type="button"
            onClick={() => onReply(q)}
            style={{ flexShrink: 0, padding: '4px 10px', fontSize: '12px' }}
          >
            Reply
          </button>
        )}
      </div>
      {!collapsed && q.answer ? (
        <div style={{ marginTop: '10px', paddingLeft: '10px', borderLeft: '3px solid #4CAF50' }}>
          <div style={{ marginBottom: '5px' }}><strong>A:</strong> {q.answer.answer_text}</div>
          <div style={{ fontSize: '12px', color: '#666', marginTop: '5px' }}>
            Status: <span style={{
              color: q.answer.answer_status === 'answered' ? '#4CAF50' :
                q.answer.answer_status === 'not_covered' ? '#ff9800' : '#f44336',
              fontWeight: 'bold'
            }}>{q.answer.answer_status}</span> |
            Confidence: {q.answer.confidence ? (q.answer.confidence * 100).toFixed(0) + '%' : 'N/A'}
            {q.answer.confirmed && (
              <span style={{
                marginLeft: '10px',
                color: '#4CAF50',
                fontWeight: 'bold',
                fontSize: '13px'
              }}>
                ✓ Confirmed by Creator
              </span>
            )}
          </div>
          {q.answer.citations && q.answer.citations.length > 0 && (
            <div className="citations" style={{ marginTop: '10px' }}>
              <strong>Citations:</strong>
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: '6px', marginTop: '6px' }}>
                {q.answer.citations.map((citation, cidx) => (
                  <CitationBadge
                    key={cidx}
                    citation={citation}
                    onClick={() => onCitationClick?.(citation)}
                  />
                ))}
              </div>
            </div>
          )}
        </div>
      ) : !collapsed ? (
        <div style={{ marginTop: '10px', padding: '10px', backgroundColor: q._pending ? '#e3f2fd' : '#f5f5f5', borderRadius: '3px', fontSize: '13px', display: 'flex', alignItems: 'center', gap: '8px' }}>
          <span className="spinner" style={{ width: 16, height: 16, flexShrink: 0 }} aria-hidden />
          <span>{q._pending ? 'Getting an answer…' : 'Processing…'}</span>
        </div>
      ) : null}
    </div>
  )
}

function countReplies(byParent, rootId) {
  let n = 0
  const direct = byParent[rootId] || []
  n += direct.length
  for (const q of direct) n += countReplies(byParent, q.id)
  return n
}

function ThreadList({ roots, byParent, onCitationClick, onReply, depth = 0, expandedThreads = {}, onToggleThread }) {
  const isRootLevel = depth === 0
  return (
    <>
      {roots.map((q) => {
        const hasReplies = byParent[q.id] && byParent[q.id].length > 0
        const isExpandable = isRootLevel
        const isExpanded = !isExpandable || expandedThreads[q.id] === true
        const showReplies = !isRootLevel || isExpanded
        const replyCount = hasReplies ? countReplies(byParent, q.id) : 0

        return (
          <div key={q.id} style={{ marginBottom: isRootLevel ? '8px' : 0 }}>
            {isRootLevel ? (
              <div style={{ display: 'flex', alignItems: 'flex-start', gap: '4px' }}>
                <button
                  type="button"
                  onClick={() => onToggleThread?.(q.id)}
                  aria-label={isExpanded ? 'Collapse thread' : 'Expand thread'}
                  style={{
                    flexShrink: 0,
                    marginTop: '18px',
                    padding: '2px 6px',
                    fontSize: '12px',
                    background: 'none',
                    border: 'none',
                    cursor: 'pointer',
                    color: '#666'
                  }}
                >
                  {isExpanded ? '▼' : '▶'}
                </button>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <QACard q={q} onCitationClick={onCitationClick} onReply={onReply} depth={depth} collapsed={!isExpanded} />
                  {!isExpanded && hasReplies && (
                    <div style={{ fontSize: '12px', color: '#888', marginTop: '4px', marginLeft: '15px' }}>
                      {replyCount} {replyCount === 1 ? 'reply' : 'replies'}
                    </div>
                  )}
                </div>
              </div>
            ) : (
              <QACard q={q} onCitationClick={onCitationClick} onReply={onReply} depth={depth} />
            )}
            {showReplies && hasReplies && (
              <ThreadList
                roots={byParent[q.id]}
                byParent={byParent}
                onCitationClick={onCitationClick}
                onReply={onReply}
                depth={depth + 1}
                expandedThreads={expandedThreads}
                onToggleThread={onToggleThread}
              />
            )}
          </div>
        )
      })}
    </>
  )
}

export function QAHistory({ questions, readOnly = false, currentAskerName, onCitationClick, onReply }) {
  // Collapsed by default; only the asker (currentAskerName) sees their own questions auto-expanded.
  const [expandedThreads, setExpandedThreads] = useState({})
  const prevQuestionIdsRef = useRef(new Set())

  // Initial load: expand only threads where the root question was asked by current user. New questions: expand only if asked by current user.
  useEffect(() => {
    if (!questions || questions.length === 0) return
    const prevIds = prevQuestionIdsRef.current
    const currentIds = new Set(questions.map((q) => q.id))
    const newIds = questions.filter((q) => !prevIds.has(q.id))
    const { roots } = buildThreadTree(questions)

    if (prevIds.size === 0) {
      // Initial load: expand only "my" root questions (asked_by === currentAskerName)
      if (currentAskerName) {
        setExpandedThreads((prev) => {
          const next = { ...prev }
          for (const r of roots) {
            if (r.asked_by === currentAskerName) next[r.id] = true
          }
          return next
        })
      }
    } else if (newIds.length > 0) {
      // New questions appeared: expand only if the new question was asked by current user
      setExpandedThreads((prev) => {
        let next = { ...prev }
        for (const q of newIds) {
          if (currentAskerName && q.asked_by === currentAskerName) {
            const rootId = q.parent_question_id ? findRootId(questions, q.parent_question_id) : q.id
            next[rootId] = true
          }
        }
        return next
      })
    }

    prevQuestionIdsRef.current = currentIds
  }, [questions, currentAskerName])

  if (!questions || questions.length === 0) {
    return (
      <div className="info">
        {readOnly ? 'No questions yet in this session.' : 'No questions yet in this session. Ask a question above to get started!'}
      </div>
    )
  }

  const { roots, byParent } = buildThreadTree(questions)
  const handleToggleThread = (rootId) => {
    setExpandedThreads((prev) => ({ ...prev, [rootId]: !prev[rootId] }))
  }

  return (
    <div>
      <ThreadList
        roots={roots}
        byParent={byParent}
        onCitationClick={onCitationClick}
        onReply={readOnly ? undefined : onReply}
        expandedThreads={expandedThreads}
        onToggleThread={handleToggleThread}
      />
    </div>
  )
}
