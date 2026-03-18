import { useState } from 'react'
import { getMaterialIcon } from '../utils/materialIcons'

function isMaterialImage(m) {
  const ct = (m.content_type || '').toLowerCase()
  const fn = (m.filename || '').toLowerCase()
  return ct.startsWith('image/') || ['.jpg', '.jpeg', '.png', '.gif', '.webp', '.bmp', '.svg'].some(e => fn.endsWith(e))
}

function materialStatusDisplay(m) {
  return isMaterialImage(m) ? 'N/A' : m.text_status
}

export function MaterialsList({ materials, sessionId, apiBaseUrl, refetchSession }) {
  const [deletingId, setDeletingId] = useState(null)
  const [deleteError, setDeleteError] = useState(null)
  // apiBaseUrl can be '' for same-origin-relative requests; treat only null/undefined as missing.
  const canDelete = Boolean(sessionId && apiBaseUrl != null && refetchSession)

  const deleteMaterial = async (material) => {
    const materialId = material?.id != null ? String(material.id) : null
    if (!sessionId || !materialId || apiBaseUrl == null || !refetchSession) return
    setDeleteError(null)
    setDeletingId(materialId)
    try {
      const base = (apiBaseUrl || '').replace(/\/$/, '')
      const res = await fetch(`${base}/sessions/${sessionId}/materials/${materialId}`, { method: 'DELETE' })
      if (!res.ok) {
        const text = await res.text()
        throw new Error(text || `HTTP ${res.status}`)
      }
      await refetchSession()
    } catch (err) {
      setDeleteError(err?.message || 'Delete failed')
      console.error('Delete material failed:', err)
    } finally {
      setDeletingId(null)
    }
  }

  if (!materials || materials.length === 0) {
    return null
  }

  return (
    <div className="section" style={{ marginBottom: '20px', backgroundColor: '#f9f9f9', border: '1px solid #ddd' }}>
      <h2>Materials ({materials.length})</h2>
      {deleteError && (
        <div style={{ padding: '8px 12px', marginBottom: '12px', backgroundColor: '#ffebee', color: '#c62828', borderRadius: '4px', fontSize: '13px' }}>
          {deleteError}
          <button type="button" onClick={() => setDeleteError(null)} style={{ marginLeft: '8px', padding: '2px 8px', fontSize: '12px' }}>Dismiss</button>
        </div>
      )}
      <div>
        {materials.map((material, idx) => {
          const status = materialStatusDisplay(material)
          return (
          <div key={material.id || idx} style={{
            marginBottom: '15px',
            padding: '15px',
            border: '1px solid #ddd',
            borderRadius: '5px',
            backgroundColor: '#fff',
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: 'flex-start',
            gap: '12px',
            flexWrap: 'wrap'
          }}>
            <div style={{ flex: 1, minWidth: 0 }}>
              <div style={{ fontWeight: 'bold', marginBottom: '5px' }}>
                {getMaterialIcon(material)} {material.filename || 'Untitled'}
              </div>
              <div style={{ fontSize: '12px', color: '#666', marginBottom: '10px' }}>
                Kind: <strong>{material.kind}</strong> |
                Type: <strong>{material.content_type}</strong> |
                Status: <span style={{
                  color: status === 'N/A' ? '#666' : status === 'ready' ? '#4CAF50' :
                         (material.text_status === 'pending' || material.text_status === 'processing') ? '#ff9800' : material.text_status === 'failed' ? '#f44336' : '#666',
                  fontWeight: 'bold'
                }}>{status}</span>
              </div>
              {material.storage_url && (
                <div style={{ fontSize: '11px', color: '#999', marginBottom: '5px' }}>
                  Storage: <code>{material.storage_url}</code>
                </div>
              )}
              {(material.extracted_text || material.kind === 'video' || material.text_status === 'pending' || material.text_status === 'processing' || material.text_status === 'failed') && (
                <div style={{
                  marginTop: '10px',
                  padding: '10px',
                  backgroundColor: material.text_status === 'failed' ? '#ffebee' : '#f5f5f5',
                  borderRadius: '3px',
                  fontSize: '13px',
                  maxHeight: '150px',
                  overflow: 'auto',
                  border: '1px solid #e0e0e0'
                }}>
                  <strong>{material.kind === 'video' ? 'Transcript:' : 'Extracted Text Preview:'}</strong>
                  <div style={{ marginTop: '5px', fontStyle: material.extracted_text ? 'italic' : 'normal', color: material.text_status === 'failed' ? '#c62828' : '#555' }}>
                    {material.extracted_text
                      ? (material.extracted_text.length > 500
                        ? material.extracted_text.substring(0, 500) + '...'
                        : material.extracted_text)
                      : material.text_status === 'failed'
                        ? (material.error_message || 'Extraction failed.')
                        : (material.text_status === 'pending' || material.text_status === 'processing' ? 'Processing…' : 'No transcript yet.')}
                  </div>
                </div>
              )}
            </div>
            {canDelete && (
              <button
                type="button"
                onClick={(e) => { e.preventDefault(); e.stopPropagation(); deleteMaterial(material) }}
                disabled={deletingId === (material.id != null ? String(material.id) : null)}
                style={{
                  padding: '6px 12px',
                  fontSize: '12px',
                  border: '1px solid #f44336',
                  color: '#f44336',
                  background: '#fff',
                  borderRadius: '4px',
                  cursor: deletingId === material.id ? 'not-allowed' : 'pointer',
                  flexShrink: 0
                }}
              >
                {deletingId === (material.id != null ? String(material.id) : null) ? '…' : 'Delete'}
              </button>
            )}
          </div>
          )
        })}
      </div>
    </div>
  )
}
