function CitationBadge({ citation, onClick }) {
  const label = citation.label || (citation.citation_id ? `[${citation.citation_id}]` : citation.source_type)
  const canNavigate = citation.anchor?.start_ms != null || (citation.navigation && (citation.navigation.type === 'video' || citation.navigation.type === 'pdf' || citation.navigation.type === 'doc'))
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

export function QAHistory({ questions, readOnly = false, onCitationClick }) {
  if (!questions || questions.length === 0) {
    return (
      <div className="info">
        {readOnly ? 'No questions yet in this session.' : 'No questions yet in this session. Ask a question above to get started!'}
      </div>
    )
  }

  return (
    <div>
      {questions.map((q) => (
        <div key={q.id} style={{ 
          marginBottom: '15px', 
          padding: '15px', 
          border: '1px solid #ddd', 
          borderRadius: '5px',
          backgroundColor: q.answer ? '#f9f9f9' : '#fff'
        }}>
          <div style={{ fontWeight: 'bold', marginBottom: '5px', color: '#333' }}>
            Q: {q.question_text}
          </div>
          <div style={{ fontSize: '11px', color: '#999', marginTop: '5px' }}>
            Asked: {new Date(q.created_at).toLocaleString()}
            {q.video_time_seconds !== null && q.video_time_seconds !== undefined && (
              <span style={{ marginLeft: '10px', color: '#2196F3', fontWeight: 'bold' }}>
                | At {Math.floor(q.video_time_seconds / 60)}:{(q.video_time_seconds % 60).toString().padStart(2, '0')}
              </span>
            )}
            {q.asked_by && (
              <span style={{ marginLeft: '10px', color: '#666' }}>
                | By: {q.asked_by}
              </span>
            )}
          </div>
          {q.answer ? (
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
          ) : (
            <div style={{ marginTop: '10px', padding: '10px', backgroundColor: '#fff3cd', borderRadius: '3px', fontSize: '13px' }}>
              No answer yet
            </div>
          )}
        </div>
      ))}
    </div>
  )
}
