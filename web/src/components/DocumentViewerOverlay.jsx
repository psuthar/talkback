import { useEffect, useRef } from 'react'

function getFocusableElements(container) {
  if (!container) return []
  const selector = 'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])'
  return Array.from(container.querySelectorAll(selector)).filter(
    el => !el.disabled && el.offsetParent !== null
  )
}

export function DocumentViewerOverlay({ document: doc, open: isOpen, onClose }) {
  const overlayRef = useRef(null)
  const closeButtonRef = useRef(null)

  useEffect(() => {
    if (!isOpen) return
    const prevOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.body.style.overflow = prevOverflow
    }
  }, [isOpen])

  useEffect(() => {
    if (!isOpen) return
    closeButtonRef.current?.focus()
  }, [isOpen])

  useEffect(() => {
    if (!isOpen || !overlayRef.current) return
    const handleKeyDown = (e) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        onClose()
        return
      }
      if (e.key !== 'Tab') return
      const focusable = getFocusableElements(overlayRef.current)
      if (focusable.length === 0) return
      const first = focusable[0]
      const last = focusable[focusable.length - 1]
      if (e.shiftKey) {
        if (document.activeElement === first) {
          e.preventDefault()
          last.focus()
        }
      } else {
        if (document.activeElement === last) {
          e.preventDefault()
          first.focus()
        }
      }
    }
    const el = overlayRef.current
    el.addEventListener('keydown', handleKeyDown)
    return () => el.removeEventListener('keydown', handleKeyDown)
  }, [isOpen, onClose])

  if (!isOpen) return null

  const isTranscript = doc?.type === 'transcript'
  const title = isTranscript ? (doc?.title || 'Transcript') : (doc?.filename || doc?.title || 'Document')
  const meta = isTranscript ? 'Transcript' : (doc?.content_type || '')
  const bodyText = isTranscript ? (doc?.text || '') : (doc?.extracted_text ?? '')
  const isPdf = !isTranscript && (doc?.content_type || '').toLowerCase().includes('pdf')
  const storageUrl = !isTranscript && doc?.storage_url

  return (
    <div
      className="document-viewer-overlay-backdrop"
      role="presentation"
      onClick={(e) => e.target === e.currentTarget && onClose()}
    >
      <div
        ref={overlayRef}
        className="document-viewer-overlay"
        role="dialog"
        aria-modal="true"
        aria-labelledby="document-viewer-title"
        onClick={(e) => e.stopPropagation()}
      >
        <header style={{
          flexShrink: 0,
          padding: '12px 16px',
          borderBottom: '1px solid #e0e0e0',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          gap: '12px'
        }}>
          <h2 id="document-viewer-title" style={{ margin: 0, fontSize: '16px', flex: 1, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis' }}>
            {title}
          </h2>
          {meta && (
            <span style={{ fontSize: '12px', color: '#666' }}>{meta}</span>
          )}
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
            <button
              type="button"
              ref={closeButtonRef}
              onClick={onClose}
              aria-label="Close"
              style={{
                padding: '6px 10px',
                fontSize: '18px',
                lineHeight: 1,
                backgroundColor: 'transparent',
                color: '#666',
                border: '1px solid #ccc',
                borderRadius: '4px',
                cursor: 'pointer'
              }}
            >
              ×
            </button>
          </div>
        </header>
        <div style={{
          flex: 1,
          minHeight: 0,
          overflow: 'auto',
          padding: '16px'
        }}>
          {isPdf && storageUrl && (storageUrl.startsWith('http') || storageUrl.startsWith('blob')) ? (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
              <iframe
                src={storageUrl}
                title={title}
                style={{
                  width: '100%',
                  flex: 1,
                  minHeight: '60vh',
                  border: '1px solid #e0e0e0',
                  borderRadius: '4px'
                }}
              />
              {bodyText && (
                <details style={{ marginTop: '12px' }}>
                  <summary style={{ cursor: 'pointer', fontWeight: 'bold' }}>Extracted text</summary>
                  <div style={{
                    marginTop: '8px',
                    padding: '12px',
                    backgroundColor: '#f5f5f5',
                    borderRadius: '4px',
                    fontSize: '14px',
                    whiteSpace: 'pre-wrap',
                    maxHeight: '300px',
                    overflowY: 'auto'
                  }}>
                    {bodyText}
                  </div>
                </details>
              )}
            </div>
          ) : (
            <div style={{
              fontSize: '14px',
              lineHeight: 1.6,
              whiteSpace: 'pre-wrap',
              fontFamily: 'inherit'
            }}>
              {bodyText || (isTranscript ? 'No transcript text.' : 'No extracted text for this document.')}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
