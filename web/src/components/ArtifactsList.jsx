export function ArtifactsList({ artifacts, readOnly = true }) {
  if (!artifacts || artifacts.length === 0) {
    return null
  }

  return (
    <div className="section" style={{ marginBottom: '20px', backgroundColor: '#f0f8ff', border: '1px solid #2196F3' }}>
      <h2>Artifacts ({artifacts.length})</h2>
      <div>
        {artifacts.map((artifact, idx) => (
          <div key={artifact.id || idx} style={{ 
            marginBottom: '15px', 
            padding: '15px', 
            border: '1px solid #ddd', 
            borderRadius: '5px',
            backgroundColor: '#fff'
          }}>
            <div style={{ fontWeight: 'bold', fontSize: '16px', marginBottom: '5px' }}>
              {artifact.title}
            </div>
            {artifact.description && (
              <div style={{ fontSize: '14px', color: '#666', marginBottom: '10px' }}>
                {artifact.description}
              </div>
            )}
            <div style={{ fontSize: '12px', color: '#999' }}>
              Status: <span style={{ 
                color: artifact.status === 'ready' ? '#4CAF50' : '#999',
                fontWeight: 'bold'
              }}>{artifact.status}</span>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
