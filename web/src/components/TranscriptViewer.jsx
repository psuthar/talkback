export function TranscriptViewer({ transcriptText }) {
  if (!transcriptText) {
    return null
  }

  return (
    <div style={{ 
      marginTop: '20px', 
      padding: '15px', 
      backgroundColor: '#f5f5f5', 
      borderRadius: '8px',
      fontSize: '14px',
      maxHeight: '300px',
      overflow: 'auto',
      border: '1px solid #e0e0e0'
    }}>
      <strong style={{ display: 'block', marginBottom: '10px' }}>Transcript:</strong>
      <div style={{ 
        fontStyle: 'italic', 
        color: '#555', 
        whiteSpace: 'pre-wrap',
        lineHeight: '1.6'
      }}>
        {transcriptText}
      </div>
    </div>
  )
}
