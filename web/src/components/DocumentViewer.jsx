import { useRef, useState, useEffect } from 'react'
import mammoth from 'mammoth'
import { SlideDeckViewer } from './SlideDeckViewer'
import { getMaterialTypeLabel } from '../utils/materialIcons'

const MATERIAL_CHUNK_SIZE = 1200
const MATERIAL_CHUNK_OVERLAP = 150

function chunkTextForDisplay(text, chunkSize = MATERIAL_CHUNK_SIZE, overlap = MATERIAL_CHUNK_OVERLAP) {
  const t = (text || '').trim()
  if (t.length <= chunkSize) return [t]
  const chunks = []
  let start = 0
  while (start < t.length) {
    let end = start + chunkSize
    if (end > t.length) end = t.length
    chunks.push(t.slice(start, end))
    if (end >= t.length) break
    start = end - overlap
  }
  return chunks
}

/**
 * Renders a document (material, link, or transcript) in the center pane.
 * Used by both ParticipantMode and CreatorMode.
 */
export function DocumentViewer({ doc, apiBaseUrl, sessionId, initialPage, initialBlock, slidesRefreshTrigger }) {
  const contentRef = useRef(null)
  const blockRefs = useRef([])
  const [docxHtml, setDocxHtml] = useState(null)
  const [docxLoading, setDocxLoading] = useState(false)
  const [docxError, setDocxError] = useState(null)

  const isTranscript = doc?.type === 'transcript'
  const isLink = doc?.type === 'link'
  const title = isTranscript ? (doc?.title || 'Transcript') : isLink ? (doc?.title || doc?.url || 'Link') : (doc?.filename || doc?.title || 'Document')
  const meta = isTranscript ? 'Transcript' : isLink ? 'Link' : (getMaterialTypeLabel(doc) || doc?.content_type || '')
  const bodyText = isTranscript ? (doc?.text || '') : (doc?.extracted_text ?? '')
  const contentType = (doc?.content_type || '').toLowerCase()
  const fn = (doc?.filename || '').toLowerCase()
  const isPdf = !isTranscript && !isLink && contentType.includes('pdf')
  const isDocx = !isTranscript && !isLink && (
    contentType.includes('wordprocessingml') || contentType.includes('msword') ||
    fn.endsWith('.docx') || fn.endsWith('.doc')
  )
  const storageUrl = !isTranscript && !isLink && doc?.storage_url
  const imageExts = ['.jpg', '.jpeg', '.png', '.gif', '.webp', '.bmp', '.svg']
  const isImage = !isTranscript && !isLink && (
    contentType.startsWith('image/') ||
    imageExts.some(e => fn.endsWith(e))
  )
  const isSlideDeck = !isTranscript && !isLink && !isImage && (
    fn.endsWith('.ppt') || fn.endsWith('.pptx') ||
    contentType.includes('presentationml') ||
    contentType.includes('ms-powerpoint') ||
    contentType.includes('vnd.ms-powerpoint') ||
    contentType.includes('openxmlformats-officedocument.presentationml.presentation')
  )
  // apiBaseUrl can be '' for same-origin-relative requests; treat null/undefined as missing.
  const baseMaterialFileUrl = apiBaseUrl != null && doc?.artifact_id && doc?.id && !isTranscript && !isLink
    ? `${apiBaseUrl.replace(/\/$/, '')}/artifacts/${doc.artifact_id}/materials/${doc.id}/file`
    : null
  const effectiveMaterialFileUrl = baseMaterialFileUrl && (initialPage != null && initialPage >= 1)
    ? `${baseMaterialFileUrl}#page=${Number(initialPage)}`
    : baseMaterialFileUrl

  // Load .docx and convert to HTML for formatted display
  useEffect(() => {
    if (!isDocx || !baseMaterialFileUrl) {
      setDocxHtml(null)
      setDocxError(null)
      return
    }
    let cancelled = false
    setDocxLoading(true)
    setDocxError(null)
    setDocxHtml(null)
    fetch(baseMaterialFileUrl, { credentials: 'include' })
      .then(res => res.arrayBuffer())
      .then(arrayBuffer => mammoth.convertToHtml({ arrayBuffer }))
      .then(({ value }) => {
        if (!cancelled) setDocxHtml(value)
      })
      .catch(err => {
        if (!cancelled) setDocxError(err?.message || 'Failed to load formatted view')
      })
      .finally(() => {
        if (!cancelled) setDocxLoading(false)
      })
    return () => { cancelled = true }
  }, [isDocx, baseMaterialFileUrl, doc?.id])

  const textChunks = (bodyText && initialBlock != null && !isPdf && !isImage && !docxHtml)
    ? chunkTextForDisplay(bodyText)
    : null

  useEffect(() => {
    if (initialBlock == null || !contentRef.current || !Array.isArray(textChunks) || initialBlock < 0 || initialBlock >= textChunks.length) return
    const el = blockRefs.current[initialBlock]
    if (el) {
      el.scrollIntoView({ behavior: 'smooth', block: 'center' })
    }
  }, [initialBlock, textChunks?.length])

  /* Slide decks (PPTX) live under kind=document; use filename/MIME, not kind */
  if (isSlideDeck && sessionId && doc?.id) {
    return (
      <SlideDeckViewer
        apiBaseUrl={apiBaseUrl}
        sessionId={sessionId}
        materialId={doc.id}
        artifactId={doc.artifact_id}
        initialSlide={initialPage}
        slidesRefreshTrigger={slidesRefreshTrigger}
      />
    )
  }

  const iframeSrc = effectiveMaterialFileUrl || baseMaterialFileUrl

  return (
    <div data-testid="document-viewer" style={{ display: 'flex', flexDirection: 'column', height: '100%', minHeight: 0 }}>
      <header style={{
        flexShrink: 0,
        padding: '8px 0 12px',
        borderBottom: '1px solid #e0e0e0',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
        gap: '12px'
      }}>
        <h2 style={{ margin: 0, fontSize: '16px', flex: 1, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis' }}>
          {title}
        </h2>
        <div style={{ display: 'flex', alignItems: 'center', gap: '12px', flexShrink: 0 }}>
          {isLink && doc?.url && (
            <a
              href={doc.url + (doc.fragment && !doc.url.includes('#') ? '#' + doc.fragment : '')}
              target="_blank"
              rel="noopener noreferrer"
              style={{ fontSize: '13px', color: 'var(--color-primary)', whiteSpace: 'nowrap' }}
            >
              Open in new tab
            </a>
          )}
          {meta && (
            <span style={{ fontSize: '12px', color: '#666' }}>{meta}</span>
          )}
        </div>
      </header>
      <div style={{
        flex: 1,
        minHeight: 0,
        overflow: 'auto',
        padding: '16px 0 0'
      }}>
        {isLink && doc?.url ? (
          <div data-testid="link-viewer" style={{ display: 'flex', flexDirection: 'column', gap: '12px', height: '100%' }}>
            <iframe
              src={doc.url + (doc.fragment && !doc.url.includes('#') ? '#' + doc.fragment : '')}
              title={title}
              style={{
                width: '100%',
                flex: 1,
                minHeight: '60vh',
                border: '1px solid #e0e0e0',
                borderRadius: '4px'
              }}
            />
            <p style={{ fontSize: '12px', color: '#666', margin: 0 }}>
              Some sites block embedding. If the page does not load, use &quot;Open in new tab&quot; above.
            </p>
          </div>
        ) : isImage && iframeSrc ? (
          <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'flex-start' }}>
            <img
              data-testid="image-viewer"
              src={iframeSrc}
              alt={title}
              style={{ maxWidth: '100%', height: 'auto', borderRadius: '8px', border: '1px solid #e0e0e0' }}
            />
          </div>
        ) : isPdf && iframeSrc ? (
          <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
            <iframe
              src={iframeSrc}
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
        ) : isPdf && storageUrl && (storageUrl.startsWith('http') || storageUrl.startsWith('blob')) ? (
          <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
            <iframe
              src={initialPage != null && initialPage >= 1 ? `${storageUrl}#page=${Number(initialPage)}` : storageUrl}
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
        ) : isDocx && docxHtml ? (
          <div
            className="docx-rendered"
            style={{ fontSize: '14px', lineHeight: 1.6 }}
            dangerouslySetInnerHTML={{ __html: docxHtml }}
          />
        ) : isDocx && docxLoading ? (
          <div style={{ padding: '24px', color: '#666' }}>Loading document…</div>
        ) : textChunks && textChunks.length > 0 ? (
          <div ref={contentRef} style={{
            flex: 1,
            minHeight: 0,
            overflow: 'auto',
            fontSize: '14px',
            lineHeight: 1.6,
            whiteSpace: 'pre-wrap',
            fontFamily: 'inherit'
          }}>
            {textChunks.map((chunk, i) => (
              <div
                key={i}
                ref={(el) => { blockRefs.current[i] = el }}
                data-block-index={i}
                style={{
                  marginBottom: '1em',
                  padding: initialBlock === i ? '10px 12px' : '6px 0',
                  borderRadius: 4,
                  backgroundColor: initialBlock === i ? 'rgba(33, 150, 243, 0.08)' : 'transparent',
                  borderLeft: initialBlock === i ? '3px solid #2196F3' : '3px solid transparent'
                }}
              >
                {chunk}
              </div>
            ))}
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
  )
}
