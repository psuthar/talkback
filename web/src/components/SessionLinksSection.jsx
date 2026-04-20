import { useState } from 'react'

export function SessionLinksSection({ sessionId, links = [], apiBaseUrl, refetchSession }) {
  const [urlInput, setUrlInput] = useState('')
  const [adding, setAdding] = useState(false)
  const [addError, setAddError] = useState(null)
  const [deletingId, setDeletingId] = useState(null)
  const [deleteError, setDeleteError] = useState(null)

  const base = (apiBaseUrl || '').replace(/\/$/, '')
  const linksUrl = `${base}/api/sessions/${sessionId}/links`

  const addLink = async () => {
    const url = (urlInput || '').trim()
    if (!url || !sessionId) return
    setAddError(null)
    setAdding(true)
    try {
      const res = await fetch(linksUrl, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ url })
      })
      const data = await res.json().catch(() => ({}))
      if (res.ok) {
        setUrlInput('')
        if (typeof refetchSession === 'function') await refetchSession()
      } else {
        setAddError(data.error || `HTTP ${res.status}`)
        if (res.status === 429 && data.max_links != null) {
          setAddError(`Session links limit reached (max ${data.max_links}).`)
        }
      }
    } catch (err) {
      setAddError(err?.message || 'Failed to add link')
    } finally {
      setAdding(false)
    }
  }

  const deleteLink = async (linkId) => {
    if (!sessionId || !linkId) return
    setDeleteError(null)
    setDeletingId(linkId)
    try {
      const res = await fetch(`${linksUrl}/${linkId}`, { method: 'DELETE', credentials: 'include' })
      if (!res.ok) {
        const text = await res.text()
        throw new Error(text || `HTTP ${res.status}`)
      }
      if (typeof refetchSession === 'function') await refetchSession()
    } catch (err) {
      setDeleteError(err?.message || 'Delete failed')
    } finally {
      setDeletingId(null)
    }
  }

  return (
    <div className="section session-links-section" style={{ marginBottom: '12px', backgroundColor: '#f9f9f9', border: '1px solid #ddd' }}>
      <h2>Links {Array.isArray(links) && links.length > 0 ? `(${links.length})` : ''}</h2>
      <p style={{ fontSize: '13px', color: '#666', marginBottom: '6px' }}>
        Add URLs to include their content in session Q&A. Each link is verified and indexed for citations.
      </p>
      <div style={{ marginBottom: '8px', display: 'flex', gap: '8px', flexWrap: 'wrap', alignItems: 'center' }}>
        <input
          type="url"
          placeholder="https://..."
          value={urlInput}
          onChange={(e) => setUrlInput(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && addLink()}
          style={{ flex: '1', minWidth: '200px', padding: '8px 12px', fontSize: '14px', borderRadius: '4px', border: '1px solid #ccc' }}
          disabled={adding}
        />
        <button
          type="button"
          onClick={addLink}
          disabled={adding || !urlInput.trim()}
          style={{ padding: '8px 16px', fontSize: '14px', borderRadius: '4px', border: 'none', backgroundColor: 'var(--color-primary)', color: '#fff', cursor: adding ? 'not-allowed' : 'pointer' }}
        >
          {adding ? 'Adding…' : 'Add link'}
        </button>
      </div>
      {addError && (
        <div style={{ padding: '8px 12px', marginBottom: '12px', backgroundColor: '#ffebee', color: 'var(--color-danger-dark)', borderRadius: '4px', fontSize: '13px' }}>
          {addError}
          <button type="button" onClick={() => setAddError(null)} style={{ marginLeft: '8px', padding: '2px 8px', fontSize: '12px' }}>Dismiss</button>
        </div>
      )}
      {deleteError && (
        <div style={{ padding: '8px 12px', marginBottom: '12px', backgroundColor: '#ffebee', color: 'var(--color-danger-dark)', borderRadius: '4px', fontSize: '13px' }}>
          {deleteError}
          <button type="button" onClick={() => setDeleteError(null)} style={{ marginLeft: '8px', padding: '2px 8px', fontSize: '12px' }}>Dismiss</button>
        </div>
      )}
      {Array.isArray(links) && links.length > 0 && (
        <div>
          {links.map((link) => (
            <div
              key={link.id}
              style={{
                marginBottom: '10px',
                padding: '12px',
                border: '1px solid #ddd',
                borderRadius: '5px',
                backgroundColor: '#fff',
                display: 'flex',
                justifyContent: 'space-between',
                alignItems: 'flex-start',
                gap: '12px',
                flexWrap: 'wrap'
              }}
            >
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ fontWeight: 'bold', marginBottom: '4px' }}>
                  {link.title || link.url || 'Untitled'}
                </div>
                <div style={{ fontSize: '12px', color: '#666', wordBreak: 'break-all' }}>
                  {link.url}
                </div>
                <div style={{ fontSize: '12px', marginTop: '4px', display: 'flex', alignItems: 'center', gap: '6px' }}>
                  {link.status === 'verified' && (
                    <span style={{ fontSize: '14px', color: 'var(--color-success-mid)' }} title="Verified" aria-hidden>✓</span>
                  )}
                  {(link.status === 'pending' || link.status === 'processing') && (
                    <span style={{ fontSize: '14px', color: '#ed6c02' }} title="Processing">Processing…</span>
                  )}
                  {link.status === 'failed' && (
                    <>
                      <span style={{ fontSize: '14px', color: 'var(--color-danger-dark)' }} title="Failed" aria-hidden>✕</span>
                      {link.error_message && (
                        <span style={{ color: 'var(--color-danger-dark)', marginLeft: '4px' }} title={link.error_message}>
                          {link.error_message.length > 60 ? link.error_message.slice(0, 60) + '…' : link.error_message}
                        </span>
                      )}
                    </>
                  )}
                </div>
              </div>
              <button
                type="button"
                onClick={() => deleteLink(link.id)}
                disabled={deletingId === link.id}
                style={{ padding: '4px 10px', fontSize: '12px', color: 'var(--color-danger-dark)', border: '1px solid #c62828', borderRadius: '4px', background: 'transparent', cursor: deletingId === link.id ? 'not-allowed' : 'pointer' }}
              >
                {deletingId === link.id ? 'Deleting…' : 'Delete'}
              </button>
            </div>
          ))}
        </div>
      )}
      {(!links || links.length === 0) && (
        <div style={{ fontSize: '13px', color: '#888' }}>No links added yet.</div>
      )}
    </div>
  )
}
