import { useRef, useEffect } from 'react'

// Format milliseconds as M:SS (e.g. 0:05, 1:23)
function formatMmSs(ms) {
  const m = Math.floor(ms / 60000)
  const s = Math.floor((ms % 60000) / 1000)
  return `${m}:${s.toString().padStart(2, '0')}`
}

// Normalize segment to { startMs, endMs, text, speaker_label } (supports start_time/end_time in seconds or start_ms/end_ms)
function segmentToMs(seg) {
  const startMs = seg.start_ms != null ? Number(seg.start_ms) : (seg.start_time ?? 0) * 1000
  const endMs = seg.end_ms != null ? Number(seg.end_ms) : (seg.end_time ?? seg.start_time ?? 0) * 1000
  return { startMs, endMs, text: seg.text ?? '', speakerLabel: seg.speaker_label ?? seg.speakerLabel }
}

export function TranscriptViewer({ transcriptText, segments, highlightRangeMs, showTimestamps = true }) {
  const containerRef = useRef(null)
  const highlightRef = useRef(null)

  useEffect(() => {
    if (!highlightRangeMs || !highlightRef.current || !containerRef.current) return
    highlightRef.current.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
  }, [highlightRangeMs])

  if (!transcriptText && (!segments || segments.length === 0)) {
    return null
  }

  const highlightStartMs = highlightRangeMs?.startMs ?? 0
  const highlightEndMs = highlightRangeMs?.endMs ?? 0

  const content = segments && segments.length > 0
    ? (
        <div style={{ lineHeight: '1.6' }}>
          {segments.map((seg, idx) => {
            const { startMs: segStartMs, endMs: segEndMs, text, speakerLabel } = segmentToMs(seg)
            const highlight = highlightEndMs > highlightStartMs && segStartMs < highlightEndMs && segEndMs > highlightStartMs
            const line = (
              <div
                key={idx}
                ref={highlight ? highlightRef : undefined}
                style={{
                  marginBottom: '8px',
                  paddingBottom: '8px',
                  borderBottom: '1px solid #eee',
                  backgroundColor: highlight ? 'rgba(255,235,59,0.25)' : 'transparent',
                  padding: highlight ? '4px 0' : '0',
                  transition: 'background-color 0.2s ease'
                }}
              >
                {showTimestamps && (
                  <span style={{ fontSize: '12px', color: '#666', marginRight: '8px', whiteSpace: 'nowrap' }}>
                    {formatMmSs(segStartMs)}–{formatMmSs(segEndMs)}
                  </span>
                )}
                {speakerLabel && (
                  <span style={{ fontSize: '13px', fontWeight: 600, color: '#333', marginRight: '6px' }}>
                    {speakerLabel}:
                  </span>
                )}
                <span style={{ fontSize: '14px' }}>{text}</span>
              </div>
            )
            return line
          })}
        </div>
      )
    : (
        <div style={{
          fontStyle: 'italic',
          color: '#555',
          whiteSpace: 'pre-wrap',
          lineHeight: '1.6'
        }}>
          {transcriptText}
        </div>
      )

  return (
    <div
      ref={containerRef}
      style={{
        marginTop: '0',
        padding: '12px',
        backgroundColor: '#f5f5f5',
        borderRadius: '8px',
        fontSize: '14px',
        maxHeight: '300px',
        overflow: 'auto',
        border: '1px solid #e0e0e0'
      }}
    >
      <strong style={{ display: 'block', marginBottom: '8px' }}>Transcript:</strong>
      <div style={{ color: '#555' }}>
        {content}
      </div>
    </div>
  )
}
