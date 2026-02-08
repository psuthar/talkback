import { getMaterialIcon } from '../utils/materialIcons'

function isMaterialImage(m) {
  const ct = (m.content_type || '').toLowerCase()
  const fn = (m.filename || '').toLowerCase()
  return ct.startsWith('image/') || ['.jpg', '.jpeg', '.png', '.gif', '.webp', '.bmp', '.svg'].some(e => fn.endsWith(e))
}

function materialStatusDisplay(m) {
  return isMaterialImage(m) ? 'N/A' : m.text_status
}

export function MaterialsList({ materials }) {
  if (!materials || materials.length === 0) {
    return null
  }

  return (
    <div className="section" style={{ marginBottom: '20px', backgroundColor: '#f9f9f9', border: '1px solid #ddd' }}>
      <h2>Materials ({materials.length})</h2>
      <div>
        {materials.map((material, idx) => {
          const status = materialStatusDisplay(material)
          return (
          <div key={material.id || idx} style={{
            marginBottom: '15px',
            padding: '15px',
            border: '1px solid #ddd',
            borderRadius: '5px',
            backgroundColor: '#fff'
          }}>
            <div style={{ fontWeight: 'bold', marginBottom: '5px' }}>
              {getMaterialIcon(material)} {material.filename || 'Untitled'}
            </div>
            <div style={{ fontSize: '12px', color: '#666', marginBottom: '10px' }}>
              Kind: <strong>{material.kind}</strong> |
              Type: <strong>{material.content_type}</strong> |
              Status: <span style={{
                color: status === 'N/A' ? '#666' : status === 'ready' ? '#4CAF50' :
                       material.text_status === 'pending' ? '#ff9800' : '#f44336',
                fontWeight: 'bold'
              }}>{status}</span>
            </div>
            {material.storage_url && (
              <div style={{ fontSize: '11px', color: '#999', marginBottom: '5px' }}>
                Storage: <code>{material.storage_url}</code>
              </div>
            )}
            {material.extracted_text && (
              <div style={{ 
                marginTop: '10px', 
                padding: '10px', 
                backgroundColor: '#f5f5f5', 
                borderRadius: '3px',
                fontSize: '13px',
                maxHeight: '150px',
                overflow: 'auto',
                border: '1px solid #e0e0e0'
              }}>
                <strong>Extracted Text Preview:</strong>
                <div style={{ marginTop: '5px', fontStyle: 'italic', color: '#555' }}>
                  {material.extracted_text.length > 500 
                    ? material.extracted_text.substring(0, 500) + '...' 
                    : material.extracted_text}
                </div>
              </div>
            )}
          </div>
          )
        })}
      </div>
    </div>
  )
}
