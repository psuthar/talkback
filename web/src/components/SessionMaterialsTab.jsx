import { useState, useRef } from 'react'
import { getMaterialIcon } from '../utils/materialIcons'

function statusDisplay(m) {
  const s = m.text_status || m.status
  return s === 'ready' ? 'Ready' : s === 'pending' || s === 'processing' ? 'Processing…' : s === 'failed' ? 'Failed' : s || '—'
}

function statusColor(s) {
  return s === 'ready' ? 'var(--color-success-mid)' : s === 'pending' || s === 'processing' ? '#ff9800' : s === 'failed' ? 'var(--color-danger)' : '#666'
}

function formatBytes(n) {
  if (n == null || Number.isNaN(n)) return ''
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / (1024 * 1024)).toFixed(1)} MB`
}

/**
 * Format an upload-error response into a single line for the inline
 * `Errors:` ribbon. SCRUM-334 introduced a structured 413 body for
 * spreadsheet-too-large uploads; render those with the cap + actual
 * sizes so the user can resize and retry. Everything else falls back
 * to the legacy plain-text-or-statusText path.
 */
async function formatUploadError(res, file) {
  if (res.status === 413) {
    try {
      const body = await res.clone().json()
      if (body && body.error === 'spreadsheet_too_large') {
        const max = body.max_bytes
        const actual = body.actual_bytes
        return `${file.name}: spreadsheet too large (max ${formatBytes(max)}; this file is ${formatBytes(actual)})`
      }
    } catch (_) {
      // Fall through to plain-text below
    }
  }
  const t = await res.text().catch(() => '')
  return `${file.name}: ${t || res.statusText}`
}

export function SessionMaterialsTab({
  sessionId,
  materials = [],
  videoSources = [],
  apiBaseUrl,
  refetchSession,
  onUploading,
  onUploadDone,
  onError
}) {
  const [uploading, setUploading] = useState(false)
  const [uploadFeedback, setUploadFeedback] = useState('')
  const [pasting, setPasting] = useState(false)
  const [pasteModal, setPasteModal] = useState(false)
  const [pasteTitle, setPasteTitle] = useState('')
  const [pasteText, setPasteText] = useState('')
  const [deletingId, setDeletingId] = useState(null)
  const [previewMaterial, setPreviewMaterial] = useState(null)
  const [videoUrl, setVideoUrl] = useState('')
  const [videoUrlAdding, setVideoUrlAdding] = useState(false)
  const [duplicateWarning, setDuplicateWarning] = useState(null) // { files: File[], duplicateNames: string[], nameToMaterialId: { [name]: id } }
  const fileInputRef = useRef(null)

  const base = (apiBaseUrl || '').replace(/\/$/, '')

  const isVideoFile = (file) => {
    const type = (file.type || '').toLowerCase()
    const name = (file.name || '').toLowerCase()
    return type === 'video/mp4' || name.endsWith('.mp4')
  }

  const existingNamesSet = () => {
    const names = new Set()
    for (const m of materials) {
      const n = (m?.filename || m?.title || '').toString().toLowerCase().trim()
      if (n) names.add(n)
    }
    if (videoSources?.length) {
      for (const v of videoSources) {
        const n = (v?.original_filename || v?.filename || v?.title || '').toString().toLowerCase().trim()
        if (n) names.add(n)
      }
    }
    return names
  }

  const nameToMaterialIdMap = () => {
    const map = {}
    for (const m of materials) {
      const n = (m?.filename || m?.title || '').toString().toLowerCase().trim()
      if (n && m?.id) map[n] = m.id
    }
    return map
  }

  // R2 presign-put flow for video: presign-put → PUT to URL → complete → set session primary-video-artifact
  const uploadVideoFileR2 = async (file) => {
    const presignRes = await fetch(`${base}/api/artifacts/presign-put`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({
        session_id: sessionId,
        kind: 'video',
        filename: file.name,
        content_type: file.type || 'video/mp4',
        size_bytes: file.size
      })
    })
    if (!presignRes.ok) return { ok: false, error: await presignRes.text() || presignRes.statusText }
    const { artifact_id, put_url, required_headers } = await presignRes.json()
    const putRes = await fetch(put_url, {
      method: 'PUT',
      headers: { ...(required_headers || {}), 'Content-Type': file.type || 'video/mp4' },
      body: file
    })
    if (!putRes.ok) return { ok: false, error: `PUT failed: ${putRes.status}` }
    const completeRes = await fetch(`${base}/api/artifacts/complete`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({ artifact_id, size_bytes: file.size })
    })
    if (!completeRes.ok) return { ok: false, error: await completeRes.text() || completeRes.statusText }
    const primaryRes = await fetch(`${base}/api/sessions/${sessionId}/primary-video-artifact`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({ artifact_id })
    })
    if (!primaryRes.ok) return { ok: false, error: await primaryRes.text() || primaryRes.statusText }
    return { ok: true }
  }

  const doUploadFiles = async (files) => {
    if (files.length === 0 || !sessionId) return
    setUploading(true)
    setUploadFeedback('')
    setDuplicateWarning(null)
    onUploading?.()
    let materialsCount = 0
    let videosCount = 0
    const errors = []
    try {
      for (const file of files) {
        if (isVideoFile(file)) {
          const r2Result = await uploadVideoFileR2(file)
          if (r2Result.ok) {
            videosCount += 1
          } else {
            // Fallback to legacy multipart upload when R2 not configured (503) or other failure
            const form = new FormData()
            form.append('file', file)
            const res = await fetch(`${base}/sessions/${sessionId}/video/upload`, { method: 'POST', body: form })
            if (!res.ok) {
              const t = await res.text()
              errors.push(`${file.name}: ${t || res.statusText}`)
            } else {
              videosCount += 1
            }
          }
        } else {
          const form = new FormData()
          form.append('file', file)
          const res = await fetch(`${base}/sessions/${sessionId}/materials/upload`, { method: 'POST', body: form, credentials: 'include' })
          if (!res.ok) {
            // SCRUM-334: spreadsheet uploads over the configured cap return
            // HTTP 413 with a structured JSON body. Render a friendly
            // inline error showing both the limit and the file's actual
            // size; fall back to the legacy text-error for everything else.
            errors.push(await formatUploadError(res, file))
          } else {
            materialsCount += 1
          }
        }
      }
      if (materialsCount > 0 || videosCount > 0) {
        await refetchSession?.(sessionId)
        onUploadDone?.()
        const parts = []
        if (materialsCount > 0) parts.push(`${materialsCount} material${materialsCount !== 1 ? 's' : ''}`)
        if (videosCount > 0) parts.push(`${videosCount} video${videosCount !== 1 ? 's' : ''}`)
        setUploadFeedback(`Added ${parts.join(', ')}.`)
      }
      if (errors.length > 0) {
        onError?.(errors.join('; '))
        setUploadFeedback((prev) => (prev ? `${prev} ` : '') + `Errors: ${errors.join('; ')}`)
      }
    } catch (err) {
      onError?.(err?.message || 'Upload failed')
      setUploadFeedback(err?.message || 'Upload failed')
    } finally {
      setUploading(false)
      if (fileInputRef.current) fileInputRef.current.value = ''
    }
  }

  const uploadFiles = (e) => {
    const files = Array.from(e?.target?.files || [])
    const input = e?.target
    if (input) input.value = ''
    setUploadFeedback('')
    setDuplicateWarning(null)
    if (files.length === 0 || !sessionId) return
    const existing = existingNamesSet()
    const nameToMaterialId = nameToMaterialIdMap()
    const duplicateNames = [...new Set(files.map(f => f.name).filter(n => existing.has((n || '').toLowerCase().trim())))]
    if (duplicateNames.length > 0) {
      setDuplicateWarning({ files, duplicateNames, nameToMaterialId })
      return
    }
    doUploadFiles(files)
  }

  const confirmDuplicateUpload = () => {
    if (duplicateWarning?.files?.length) {
      doUploadFiles(duplicateWarning.files)
      setDuplicateWarning(null)
    }
  }

  const confirmReplace = async () => {
    if (!duplicateWarning?.files?.length || !sessionId) return
    const { files, nameToMaterialId } = duplicateWarning
    setUploading(true)
    setUploadFeedback('')
    onUploading?.()
    const errors = []
    try {
      const materialIdsToDelete = [...new Set(files.map(f => nameToMaterialId[(f.name || '').toLowerCase().trim()]).filter(Boolean))]
      for (const materialId of materialIdsToDelete) {
        const res = await fetch(`${base}/sessions/${sessionId}/materials/${materialId}`, { method: 'DELETE' })
        if (!res.ok) errors.push(`Failed to remove existing: ${await res.text()}`)
      }
      if (errors.length > 0) {
        setUploadFeedback(errors.join('; '))
        onError?.(errors.join('; '))
      } else {
        await doUploadFiles(files)
        setUploadFeedback((prev) => (prev ? `${prev} ` : '') + 'Replaced existing file(s).')
      }
    } catch (err) {
      setUploadFeedback(err?.message || 'Replace failed')
      onError?.(err?.message)
    } finally {
      setUploading(false)
      setDuplicateWarning(null)
    }
  }

  const addVideoUrl = async () => {
    const url = (videoUrl || '').trim()
    if (!url || !sessionId) return
    setVideoUrlAdding(true)
    setUploadFeedback('')
    try {
      const res = await fetch(`${base}/sessions/${sessionId}/video/from-url`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ url })
      })
      if (!res.ok) {
        const t = await res.text()
        let msg = t || res.statusText
        try {
          const j = JSON.parse(t)
          msg = j.message || msg
        } catch (_) {}
        onError?.(msg)
        setUploadFeedback(msg)
      } else {
        const data = await res.json().catch(() => ({}))
        await refetchSession?.(sessionId)
        onUploadDone?.()
        setVideoUrl('')
        setUploadFeedback(data.message || 'Video added.')
      }
    } catch (err) {
      onError?.(err?.message || 'Failed to add video URL')
      setUploadFeedback(err?.message || 'Failed to add video URL')
    } finally {
      setVideoUrlAdding(false)
    }
  }

  const pasteSubmit = async () => {
    const text = (pasteText || '').trim()
    if (!text || !sessionId) return
    setPasting(true)
    try {
      const res = await fetch(`${base}/sessions/${sessionId}/materials/paste`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ title: (pasteTitle || '').trim() || 'Pasted text', text })
      })
      if (!res.ok) throw new Error(await res.text())
      await refetchSession?.(sessionId)
      setPasteModal(false)
      setPasteTitle('')
      setPasteText('')
      onUploadDone?.()
    } catch (err) {
      onError?.(err?.message || 'Paste failed')
    } finally {
      setPasting(false)
    }
  }

  const deleteMaterial = async (materialId) => {
    if (!sessionId || !materialId) return
    setDeletingId(materialId)
    try {
      const res = await fetch(`${base}/sessions/${sessionId}/materials/${materialId}`, { method: 'DELETE' })
      if (!res.ok) throw new Error(await res.text())
      await refetchSession?.(sessionId)
      if (previewMaterial?.id === materialId) setPreviewMaterial(null)
    } catch (err) {
      onError?.(err?.message || 'Delete failed')
    } finally {
      setDeletingId(null)
    }
  }

  const fileUrl = (m) => {
    if (!m?.artifact_id || !m?.id) return null
    return `${base}/artifacts/${m.artifact_id}/materials/${m.id}/file`
  }

  const isPdf = (m) => (m?.content_type || '').toLowerCase().includes('pdf') || (m?.filename || '').toLowerCase().endsWith('.pdf')

  return (
    <div className="section" style={{ marginBottom: 20, backgroundColor: '#fafafa', border: '1px solid #e0e0e0', borderRadius: 8, padding: 16 }}>
      <h2 style={{ marginTop: 0 }}>Add content</h2>
      <p style={{ fontSize: 13, color: '#666', marginBottom: 16 }}>
        Upload files (PDF, .txt, .md, or MP4 video) or paste text. Materials are indexed with the transcript for Q&amp;A and citations; videos are transcribed automatically.
      </p>

      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 10, marginBottom: 20 }}>
        <label style={{ display: 'inline-flex', alignItems: 'center', gap: 8, padding: '8px 14px', backgroundColor: 'var(--color-primary-mid)', color: '#fff', borderRadius: 6, cursor: 'pointer', fontWeight: 500 }}>
          <input ref={fileInputRef} type="file" accept=".pdf,.txt,.md,.docx,.xlsx,.xls,.csv,.pptx,.jpg,.jpeg,.png,.gif,.webp,.bmp,.svg,application/pdf,text/plain,text/csv,application/vnd.openxmlformats-officedocument.wordprocessingml.document,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,application/vnd.ms-excel,application/vnd.openxmlformats-officedocument.presentationml.presentation,image/jpeg,image/png,image/gif,image/webp,image/bmp,image/svg+xml,video/mp4,.mp4" multiple onChange={uploadFiles} disabled={uploading || !sessionId} style={{ display: 'none' }} />
          {uploading ? 'Uploading…' : 'Upload file'}
        </label>
        <button type="button" onClick={() => setPasteModal(true)} disabled={pasting || !sessionId} style={{ padding: '8px 14px', borderRadius: 6, border: '1px solid #2196F3', backgroundColor: '#fff', color: 'var(--color-primary-mid)', cursor: 'pointer', fontWeight: 500 }}>
          Paste text
        </button>
      </div>

      <div style={{ marginBottom: 20 }}>
        <label style={{ fontSize: 13, fontWeight: 500, display: 'block', marginBottom: 6 }}>Add video URL</label>
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, alignItems: 'center' }}>
          <input type="url" value={videoUrl} onChange={e => setVideoUrl(e.target.value)} placeholder="https://www.loom.com/share/... or https://example.com/video.mp4" style={{ flex: 1, minWidth: 200, padding: '8px 12px', borderRadius: 6, border: '1px solid #ccc', boxSizing: 'border-box' }} />
          <button type="button" onClick={addVideoUrl} disabled={videoUrlAdding || !sessionId || !videoUrl.trim()} style={{ padding: '8px 14px', borderRadius: 6, border: 'none', backgroundColor: 'var(--color-success-mid)', color: '#fff', cursor: videoUrlAdding || !sessionId || !videoUrl.trim() ? 'not-allowed' : 'pointer', fontWeight: 500 }}>
            {videoUrlAdding ? 'Adding…' : 'Add'}
          </button>
        </div>
      </div>

      {uploadFeedback && (
        <div style={{ fontSize: 13, color: '#666', marginBottom: 12 }}>{uploadFeedback}</div>
      )}

      {pasteModal && (
        <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.4)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 }} onClick={() => !pasting && setPasteModal(false)}>
          <div style={{ background: '#fff', padding: 24, borderRadius: 8, maxWidth: 480, width: '90%' }} onClick={e => e.stopPropagation()}>
            <h3 style={{ marginTop: 0 }}>Paste text</h3>
            <input type="text" placeholder="Title (optional)" value={pasteTitle} onChange={e => setPasteTitle(e.target.value)} style={{ width: '100%', padding: 8, marginBottom: 10, boxSizing: 'border-box' }} />
            <textarea placeholder="Paste your text here…" value={pasteText} onChange={e => setPasteText(e.target.value)} rows={8} style={{ width: '100%', padding: 8, boxSizing: 'border-box' }} />
            <div style={{ marginTop: 12, display: 'flex', gap: 8 }}>
              <button type="button" onClick={pasteSubmit} disabled={pasting || !pasteText.trim()} style={{ padding: '8px 16px', background: 'var(--color-primary-mid)', color: '#fff', border: 'none', borderRadius: 6, cursor: 'pointer' }}>{pasting ? 'Adding…' : 'Add'}</button>
              <button type="button" onClick={() => setPasteModal(false)} disabled={pasting} style={{ padding: '8px 16px', border: '1px solid #ccc', borderRadius: 6, cursor: 'pointer' }}>Cancel</button>
            </div>
          </div>
        </div>
      )}

      {duplicateWarning && (
        <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.4)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 }} onClick={() => setDuplicateWarning(null)}>
          <div style={{ background: '#fff', padding: 24, borderRadius: 8, maxWidth: 440, width: '90%' }} onClick={e => e.stopPropagation()}>
            <h3 style={{ marginTop: 0, color: '#e65100' }}>File(s) already in session</h3>
            <p style={{ margin: '0 0 12px', fontSize: 14, color: '#333' }}>
              The following file(s) are already in this session.
            </p>
            <ul style={{ margin: '0 0 16px', paddingLeft: 20, fontSize: 14 }}>
              {duplicateWarning.duplicateNames.map((name, i) => (
                <li key={i} style={{ marginBottom: 4 }}><strong>{name}</strong></li>
              ))}
            </ul>
            <p style={{ margin: '0 0 16px', fontSize: 13, color: '#666' }}>
              Replace the existing file(s) with the new upload, add as duplicates, or cancel?
            </p>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
              {duplicateWarning.nameToMaterialId && duplicateWarning.duplicateNames.some(name => duplicateWarning.nameToMaterialId[(name || '').toLowerCase().trim()]) && (
                <button type="button" onClick={confirmReplace} disabled={uploading} style={{ padding: '8px 16px', background: 'var(--color-success)', color: '#fff', border: 'none', borderRadius: 6, cursor: uploading ? 'not-allowed' : 'pointer' }}>{uploading ? 'Uploading…' : 'Replace'}</button>
              )}
              <button type="button" onClick={confirmDuplicateUpload} disabled={uploading} style={{ padding: '8px 16px', background: 'var(--color-primary-mid)', color: '#fff', border: 'none', borderRadius: 6, cursor: uploading ? 'not-allowed' : 'pointer' }}>{uploading ? 'Uploading…' : 'Upload as duplicate'}</button>
              <button type="button" onClick={() => setDuplicateWarning(null)} disabled={uploading} style={{ padding: '8px 16px', border: '1px solid #ccc', borderRadius: 6, cursor: uploading ? 'not-allowed' : 'pointer' }}>Cancel</button>
            </div>
          </div>
        </div>
      )}

      {materials.length === 0 ? (
        <div style={{ color: '#999', fontStyle: 'italic' }}>No materials yet. Upload a file, paste text, or add a video URL.</div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          {materials.map((m) => {
            const status = statusDisplay(m)
            const title = m.title || m.filename || 'Untitled'
            return (
              <div key={m.id} style={{ border: '1px solid #e0e0e0', borderRadius: 6, padding: 12, backgroundColor: '#fff', display: 'flex', alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: 8 }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: 10, flex: 1, minWidth: 0 }}>
                  <span>{getMaterialIcon(m)}</span>
                  <div style={{ minWidth: 0 }}>
                    <div style={{ fontWeight: 600 }}>{title}</div>
                    <div style={{ fontSize: 12, color: '#666' }}>
                      {m.content_type || 'text'} · <span style={{ color: statusColor(m.text_status || m.status), fontWeight: 500 }}>{status}</span>
                      {m.error_message && <span style={{ color: 'var(--color-danger)', marginLeft: 8 }}>{m.error_message}</span>}
                    </div>
                  </div>
                </div>
                <div style={{ display: 'flex', gap: 8 }}>
                  <button type="button" onClick={() => setPreviewMaterial(m)} style={{ padding: '6px 12px', fontSize: 12, border: '1px solid #2196F3', color: 'var(--color-primary-mid)', background: '#fff', borderRadius: 4, cursor: 'pointer' }}>View</button>
                  <button type="button" onClick={() => deleteMaterial(m.id)} disabled={deletingId === m.id} style={{ padding: '6px 12px', fontSize: 12, border: '1px solid #f44336', color: 'var(--color-danger)', background: '#fff', borderRadius: 4, cursor: 'pointer' }}>{deletingId === m.id ? '…' : 'Delete'}</button>
                </div>
              </div>
            )
          })}
        </div>
      )}

      {previewMaterial && (
        <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.5)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1001 }} onClick={() => setPreviewMaterial(null)}>
          <div style={{ background: '#fff', borderRadius: 8, maxWidth: '90vw', maxHeight: '90vh', overflow: 'hidden', display: 'flex', flexDirection: 'column' }} onClick={e => e.stopPropagation()}>
            <div style={{ padding: 12, borderBottom: '1px solid #eee', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <strong>{previewMaterial.title || previewMaterial.filename || 'Preview'}</strong>
              <button type="button" onClick={() => setPreviewMaterial(null)} style={{ padding: '4px 12px' }}>Close</button>
            </div>
            <div style={{ flex: 1, overflow: 'auto', padding: 16, minHeight: 300 }}>
              {isPdf(previewMaterial) && fileUrl(previewMaterial) ? (
                <iframe title="PDF preview" src={fileUrl(previewMaterial)} style={{ width: '100%', height: '70vh', border: 'none' }} />
              ) : (
                <>
                  {previewMaterial.kind === 'video' && <div style={{ fontWeight: 600, marginBottom: 8 }}>Transcript</div>}
                  <div style={{ whiteSpace: 'pre-wrap', fontFamily: 'inherit', fontSize: 14 }}>{(previewMaterial.extracted_text || (previewMaterial.kind === 'video' ? 'No transcript yet.' : '')).slice(0, 50000)}{(previewMaterial.extracted_text?.length > 50000 ? '\n\n…' : '')}</div>
                </>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
