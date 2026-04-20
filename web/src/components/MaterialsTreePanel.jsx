import { useEffect, useRef, useState } from 'react'
import { getMaterialSlides } from '../api/materials'

const SPINNER_STYLE_ID = 'tb-spinner-keyframes'
function ensureSpinnerStyle() {
  if (typeof document === 'undefined') return
  if (document.getElementById(SPINNER_STYLE_ID)) return
  const style = document.createElement('style')
  style.id = SPINNER_STYLE_ID
  style.textContent = '@keyframes tb-spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }'
  document.head.appendChild(style)
}

/** Small inline spinner shown while async background tasks are running (e.g. slide generation). */
function InlineSpinner() {
  ensureSpinnerStyle()
  return (
    <span
      aria-hidden
      style={{
        display: 'inline-block',
        width: '10px',
        height: '10px',
        border: '2px solid #e65100',
        borderTopColor: 'transparent',
        borderRadius: '50%',
        animation: 'tb-spin 0.8s linear infinite',
        verticalAlign: 'middle',
        flexShrink: 0,
      }}
    />
  )
}

/** Shared "Materials" header with chevron for creator and participant left panel */
export function MaterialsPanelHeader({ collapsed, onCollapsedChange, unreadCount = 0 }) {
  return (
    <div
      className={`materials-tree-header ${collapsed ? 'materials-tree-header-collapsed' : ''}`}
      style={{
        flexShrink: 0,
        padding: collapsed ? '8px 4px' : '10px 12px',
        borderBottom: '1px solid #e0e0e0',
        display: 'flex',
        alignItems: 'center',
        justifyContent: collapsed ? 'center' : 'flex-start',
        gap: '6px',
        cursor: 'pointer',
        ...(collapsed && { minHeight: '36px' }),
      }}
      onClick={() => onCollapsedChange(!collapsed)}
      onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onCollapsedChange(!collapsed); } }}
      role="button"
      tabIndex={0}
      aria-label={collapsed ? 'Expand materials panel' : 'Collapse materials panel'}
    >
      <span style={{ fontSize: '12px', color: '#555' }} aria-hidden>{collapsed ? '▷' : '▼'}</span>
      {!collapsed && (
        <span style={{ fontWeight: 'bold', fontSize: '14px', display: 'flex', alignItems: 'center', gap: '6px' }}>
          Materials
          {unreadCount > 0 && (
            <span style={{
              background: '#e65100',
              color: '#fff',
              fontSize: '12px',
              fontWeight: 'bold',
              padding: '2px 6px',
              borderRadius: '10px',
            }} title="New documents added by creator">
              New {unreadCount}
            </span>
          )}
        </span>
      )}
    </div>
  )
}

function TreeSection({ title, children }) {
  return (
    <div style={{ marginBottom: '12px' }}>
      <div style={{
        fontSize: '12px',
        fontWeight: 'bold',
        textTransform: 'uppercase',
        color: '#444',
        padding: '4px 0',
        borderBottom: '1px solid #eee'
      }}>
        {title}
      </div>
      <div style={{ paddingTop: '4px' }}>{children}</div>
    </div>
  )
}

function TreeItem({ icon, title, meta, metaStyle, selected, onClick, onDelete, deleting, testId, disabled, buttonTitle }) {
  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        marginBottom: '2px',
        borderRadius: '4px',
        background: selected ? 'var(--color-success-bg)' : 'transparent',
        ...(disabled && { opacity: 0.7 }),
      }}
      onMouseEnter={(e) => {
        if (!selected && !disabled) e.currentTarget.style.background = '#f0f0f0'
      }}
      onMouseLeave={(e) => {
        if (!selected) e.currentTarget.style.background = selected ? 'var(--color-success-bg)' : 'transparent'
      }}
    >
      <button
        data-testid={testId}
        type="button"
        title={buttonTitle}
        onClick={disabled ? undefined : onClick}
        disabled={disabled}
        style={{
          flex: 1,
          minWidth: 0,
          textAlign: 'left',
          padding: '8px 10px',
          border: 'none',
          borderRadius: '4px',
          background: 'transparent',
          cursor: disabled ? 'not-allowed' : 'pointer',
          fontSize: '13px',
          display: 'flex',
          alignItems: 'center',
          gap: '8px',
          color: disabled ? '#888' : '#1a1a1a'
        }}
      >
        {icon != null && icon !== '' && <span style={{ flexShrink: 0 }}>{icon}</span>}
        <span style={{ flex: 1, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', color: 'inherit' }}>
          {title}
        </span>
        {meta && (
          <span style={{ fontSize: '12px', color: '#555', flexShrink: 0, ...metaStyle }}>{meta}</span>
        )}
      </button>
      {onDelete && (
        <button
          type="button"
          onClick={(e) => { e.stopPropagation(); onDelete() }}
          disabled={deleting}
          aria-label={`Remove ${title}`}
          title={`Remove ${title}`}
          style={{
            flexShrink: 0,
            padding: '4px 6px',
            marginRight: '4px',
            border: 'none',
            borderRadius: '3px',
            background: 'transparent',
            color: deleting ? '#bbb' : '#999',
            cursor: deleting ? 'not-allowed' : 'pointer',
            fontSize: '14px',
            lineHeight: 1,
          }}
          onMouseEnter={(e) => { if (!deleting) e.currentTarget.style.color = 'var(--color-danger-dark)' }}
          onMouseLeave={(e) => { if (!deleting) e.currentTarget.style.color = '#999' }}
        >
          {deleting ? '…' : '×'}
        </button>
      )}
    </div>
  )
}

export function MaterialsTreePanel({
  session,
  apiBaseUrl,
  selectedVideo,
  setSelectedVideo,
  setVideoId,
  setVideoPlayerKey,
  onSelectDocument,
  onSelectVideo,
  onSelectLink,
  selectedDocumentId,
  collapsed,
  onCollapsedChange,
  hideTranscriptSection = false,
  hideHeader = false,
  lastSeenLinkCount = 0,
  canManage = false,
  onDeleteMaterial,
  deletingId,
  deleteError,
  onDeleteVideo,
}) {
  const scrollRef = useRef(null)
  const [probedSlidesStatus, setProbedSlidesStatus] = useState({})

  if (!session) return null

  const { video_sources = [], materials = [], links = [], unread_material_ids = [], primary_video, additional_videos = [], material_slides_ready = {}, material_slides_status = {} } = session
  const linkCount = Array.isArray(links) ? links.length : 0
  const newLinkCount = Math.max(0, linkCount - lastSeenLinkCount)
  const unreadSet = new Set((unread_material_ids || []).map((id) => String(id)))
  const presentationVideo = primary_video ?? (video_sources?.length > 0 ? video_sources[0] : null)
  const otherVideos = (additional_videos?.length >= 0 ? additional_videos : (video_sources?.slice(1) ?? []))

  const isMaterialImage = (m) => {
    const ct = (m.content_type || '').toLowerCase()
    const fn = (m.filename || '').toLowerCase()
    return ct.startsWith('image/') || ['.jpg', '.jpeg', '.png', '.gif', '.webp', '.bmp', '.svg', '.heic', '.heif', '.avif', '.jfif', '.tif', '.tiff', '.ico'].some(e => fn.endsWith(e))
  }

  /** Everything that is not an image file / image kind (includes PDF, Office, PPTX, text, diagram, legacy kind=slides for decks, etc.) */
  const documentMaterials = materials.filter((m) => {
    const k = (m.kind || '').toLowerCase()
    if (k === 'video') return false
    if (k === 'image') return false
    if (isMaterialImage(m)) return false
    return true
  })
  /** Raster/vector uploads (kind=image or detected image file); legacy rows may still be kind=slides */
  const imageMaterials = materials.filter((m) => {
    const k = (m.kind || '').toLowerCase()
    if (k === 'image') return true
    if (isMaterialImage(m)) return true
    return false
  })
  const materialStatusMeta = (m) => {
    if (isMaterialImage(m)) return null
    if (m.text_status === 'pending' || m.text_status === 'processing') return { label: 'Processing…', color: '#e65100' }
    if (m.text_status === 'failed') return { label: 'Failed', color: 'var(--color-danger-dark)' }
    return null
  }
  const isProcessingStatus = (status) => {
    const s = String(status || '').toLowerCase()
    return s === 'pending' || s === 'processing' || s === 'queued' || s === 'transcribing' || s === 'extracting'
  }
  const isSlideDeckMaterial = (m) => {
    const filename = String(m?.filename || '').toLowerCase()
    const contentType = String(m?.content_type || '').toLowerCase()
    const looksLikePpt = filename.endsWith('.ppt') || filename.endsWith('.pptx')
    const looksLikePresentation =
      contentType.includes('presentation') ||
      contentType.includes('powerpoint') ||
      contentType.includes('ms-powerpoint') ||
      contentType.includes('openxmlformats-officedocument.presentationml.presentation')
    return looksLikePpt || looksLikePresentation
  }
  const isMaterialViewable = (m) => {
    if (isSlideDeckMaterial(m)) {
      const slideStatus = resolveSlidesStatus(m)
      return slideStatus === 'ready' || slideStatus === 'failed'
    }
    const k = (m?.kind || '').toLowerCase()
    if (k === 'image' || isMaterialImage(m)) {
      if (isProcessingStatus(m?.text_status)) return false
      return m?.text_status === 'ready' || m?.text_status === 'failed'
    }
    if (isProcessingStatus(m?.text_status)) return false
    return m?.text_status === 'ready' || m?.text_status === 'failed'
  }
  const hasSlidesReadyFlag = (m) => Object.prototype.hasOwnProperty.call(material_slides_ready, String(m.id))
  const hasSlidesStatusFlag = (m) => Object.prototype.hasOwnProperty.call(material_slides_status, String(m.id))
  const resolveSlidesStatus = (m) => {
    const id = String(m.id)
    if (hasSlidesStatusFlag(m)) return material_slides_status[id]
    if (hasSlidesReadyFlag(m)) return material_slides_ready[id] ? 'ready' : 'processing'
    if (Object.prototype.hasOwnProperty.call(probedSlidesStatus, id)) return probedSlidesStatus[id]
    return 'processing'
  }

  useEffect(() => {
    const sessionId = session?.session?.id || session?.id
    if (!apiBaseUrl || !sessionId) return
    const targets = materials
      .filter((m) => isSlideDeckMaterial(m))
      .filter((m) => !hasSlidesStatusFlag(m) && !hasSlidesReadyFlag(m))
    if (targets.length === 0) return
    let cancelled = false
    ;(async () => {
      const updates = {}
      for (const m of targets) {
        try {
          const data = await getMaterialSlides(apiBaseUrl, sessionId, m.id)
          const list = Array.isArray(data?.slides) ? data.slides : []
          updates[String(m.id)] = list.length > 0 ? 'ready' : 'processing'
        } catch {
          updates[String(m.id)] = 'processing'
        }
      }
      if (!cancelled && Object.keys(updates).length > 0) {
        setProbedSlidesStatus((prev) => ({ ...prev, ...updates }))
      }
    })()
    return () => { cancelled = true }
  }, [apiBaseUrl, session, session?.id, session?.session?.id, materials.map((m) => `${m.id}:${m.kind}`).join('|')])

  /** Status line for the Documents list (includes PPTX slide-pipeline state). */
  const materialStatusMetaDocument = (m) => {
    if (isSlideDeckMaterial(m)) {
      const slideStatus = resolveSlidesStatus(m)
      if (slideStatus === 'failed') return { label: 'Failed', color: 'var(--color-danger-dark)' }
      if (slideStatus !== 'ready') return {
        label: (
          <span style={{ display: 'inline-flex', alignItems: 'center', gap: '4px' }}>
            <InlineSpinner />
            <span>Generating slides…</span>
          </span>
        ),
        color: '#e65100'
      }
      return null
    }
    return materialStatusMeta(m)
  }

  const materialStatusMetaImage = (m) => {
    if (m.text_status === 'pending' || m.text_status === 'processing') return { label: 'Processing…', color: '#e65100' }
    if (m.text_status === 'failed') return { label: 'Failed', color: 'var(--color-danger-dark)' }
    return null
  }
  const videoDisplayTitle = (v) => {
    const decodeSegment = (seg) => {
      if (!seg) return seg
      try {
        // Decode URL-encoded sequences like %20 so names show spaces instead of %20.
        return decodeURIComponent(seg)
      } catch {
        return seg
      }
    }
    try {
      if (v?.original_url) {
        const seg = new URL(v.original_url).pathname.split('/').filter(Boolean).pop()
        const decoded = decodeSegment(seg)
        if (decoded) return decoded
      }
    } catch (_) {}
    if (v?.stored_video_object_key) {
      const seg = v.stored_video_object_key.split('/').filter(Boolean).pop()
      const decoded = decodeSegment(seg)
      if (decoded) return decoded
      return seg || v.provider || 'Video'
    }
    return v?.provider || 'Video'
  }
  const videoMaterialId = (v) => {
    if (!v) return null
    const norm = (s) => String(s || '').replace(/\\/g, '/').toLowerCase()
    const keyNorm = norm(v.stored_video_object_key)
    if (!keyNorm) return null
    const match = materials.find((m) => {
      const mk = norm(m.storage_key)
      const mu = norm(m.storage_url)
      return mk === keyNorm || mu === keyNorm || keyNorm === mu || keyNorm === mk
    })
    return match?.id || null
  }

  const content = (
    <div ref={scrollRef} className="materials-tree-content" style={{ flex: 1, minHeight: 0, overflowY: 'auto', padding: '8px' }}>
        {deleteError && (
          <div className="error" style={{ marginBottom: 8, fontSize: 12, padding: '4px 6px' }}>
            {deleteError}
          </div>
        )}
        <TreeSection title="Presentation">
          {!presentationVideo ? (
            <div style={{ fontSize: '12px', color: '#999', padding: '4px 0' }}>No presentation video selected yet.</div>
          ) : (
            <TreeItem
              key={presentationVideo.id}
              testId="primary-video-item"
              icon={null}
              title={videoDisplayTitle(presentationVideo)}
              meta="Primary"
              selected={!selectedDocumentId && selectedVideo != null && String(selectedVideo.id) === String(presentationVideo.id)}
              onClick={() => {
                setSelectedVideo(presentationVideo)
                setVideoId(presentationVideo.id)
                setVideoPlayerKey(prev => prev + 1)
                onSelectVideo?.()
              }}
            />
          )}
        </TreeSection>

        <TreeSection title="Additional Videos">
          {otherVideos.length === 0 ? (
            <div style={{ fontSize: '12px', color: '#999', padding: '4px 0' }}>None</div>
          ) : (
            otherVideos.map((v) => (
              (() => {
                const materialId = videoMaterialId(v)
                const videoSelectable = !isProcessingStatus(v?.transcript_status)
                return (
                  <TreeItem
                    key={v.id}
                    testId="video-item"
                    icon={null}
                    title={videoDisplayTitle(v)}
                    meta={v.transcript_status === 'pending' || v.transcript_status === 'processing' ? 'Processing…' : v.transcript_status === 'failed' ? 'Failed' : ''}
                    metaStyle={v.transcript_status === 'pending' || v.transcript_status === 'processing' ? { color: '#e65100' } : v.transcript_status === 'failed' ? { color: 'var(--color-danger-dark)' } : undefined}
                    selected={!selectedDocumentId && selectedVideo != null && String(selectedVideo.id) === String(v.id)}
                    onClick={() => {
                      setSelectedVideo(v)
                      setVideoId(v.id)
                      setVideoPlayerKey(prev => prev + 1)
                      onSelectVideo?.()
                    }}
                    onDelete={canManage && onDeleteVideo && materialId ? () => onDeleteVideo(materialId) : undefined}
                    deleting={materialId ? deletingId === String(materialId) : false}
                    disabled={!videoSelectable}
                    buttonTitle={!videoSelectable ? 'Video is still processing' : undefined}
                  />
                )
              })()
            ))
          )}
        </TreeSection>

        {!hideTranscriptSection && (
          <TreeSection title="Transcript">
            {video_sources.filter(v => v.transcript_text).length === 0 ? (
              <div style={{ fontSize: '12px', color: '#999', padding: '4px 0' }}>None</div>
            ) : (
              video_sources.map((v, idx) => {
                if (!v.transcript_text) return null
                const transcriptId = `transcript-${v.id}`
                const isSelected = selectedDocumentId === transcriptId
                return (
                  <TreeItem
                    key={transcriptId}
                    icon={null}
                    title={video_sources.length > 1 ? `Transcript – Video ${idx + 1}` : 'Transcript'}
                    meta=""
                    selected={isSelected}
                    onClick={(e) => {
                      onSelectDocument({
                        type: 'transcript',
                        text: v.transcript_text,
                        title: video_sources.length > 1 ? `Transcript – Video ${idx + 1}` : 'Transcript',
                        transcriptId
                      }, e)
                    }}
                  />
                )
              })
            )}
          </TreeSection>
        )}

        <TreeSection title="Documents">
          {documentMaterials.length === 0 ? (
            <div style={{ fontSize: '12px', color: '#999', padding: '4px 0' }}>
              None
            </div>
          ) : (
            documentMaterials.map(m => {
              const statusInfo = materialStatusMetaDocument(m)
              const isNew = unreadSet.has(String(m.id))
              const metaNode = (statusInfo?.label || isNew)
                ? (
                  <span style={{ display: 'inline-flex', alignItems: 'center', gap: '4px', flexWrap: 'nowrap' }}>
                    {statusInfo?.label}
                    {statusInfo?.label && isNew && <span> • </span>}
                    {isNew && <span>New</span>}
                  </span>
                )
                : null
              const viewable = isMaterialViewable(m)
              return (
                <TreeItem
                  key={m.id}
                  testId="material-item"
                  icon={null}
                  title={m.title || m.filename || 'Untitled'}
                  meta={metaNode}
                  metaStyle={statusInfo?.color ? { color: statusInfo.color } : undefined}
                  selected={selectedDocumentId === m.id}
                  onClick={(e) => onSelectDocument(m, e)}
                  onDelete={canManage && onDeleteMaterial ? () => onDeleteMaterial(m.id) : undefined}
                  deleting={deletingId === String(m.id)}
                  disabled={!viewable}
                  buttonTitle={!viewable
                    ? (isSlideDeckMaterial(m)
                      ? (resolveSlidesStatus(m) === 'failed' ? 'Slide preview generation failed' : 'Slides are still processing')
                      : (m?.text_status === 'failed' ? 'File processing failed' : 'File is still processing'))
                    : undefined}
                />
              )
            })
          )}
        </TreeSection>

        <TreeSection title="Images">
          {imageMaterials.length === 0 ? (
            <div style={{ fontSize: '12px', color: '#999', padding: '4px 0' }}>
              None
            </div>
          ) : (
            imageMaterials.map(m => {
              const statusInfo = materialStatusMetaImage(m)
              const metaParts = [statusInfo?.label, unreadSet.has(String(m.id)) ? 'New' : null].filter(Boolean)
              const viewable = isMaterialViewable(m)
              return (
                <TreeItem
                  key={m.id}
                  testId="images-item"
                  icon={null}
                  title={m.filename || 'Untitled'}
                  meta={metaParts.join(' • ')}
                  metaStyle={statusInfo?.color ? { color: statusInfo.color } : undefined}
                  selected={selectedDocumentId === m.id}
                  onClick={(e) => onSelectDocument(m, e)}
                  onDelete={canManage && onDeleteMaterial ? () => onDeleteMaterial(m.id) : undefined}
                  deleting={deletingId === String(m.id)}
                  disabled={!viewable}
                  buttonTitle={!viewable ? (m?.text_status === 'failed' ? 'File processing failed' : 'File is still processing') : undefined}
                />
              )
            })
          )}
        </TreeSection>

        <TreeSection title={newLinkCount > 0 ? `Links • New ${newLinkCount}` : 'Links'}>
          {!Array.isArray(links) || links.length === 0 ? (
            <div style={{ fontSize: '12px', color: '#999', padding: '4px 0' }}>None</div>
          ) : (
            links.map((link) => {
              const linkDocId = `link-${link.id}`
              const isSelected = selectedDocumentId === linkDocId
              const linkSelectable = !isProcessingStatus(link.status)
              return (
                <button
                  key={link.id}
                  type="button"
                  data-testid="link-item"
                  disabled={!linkSelectable}
                  onClick={(e) => {
                    if (!linkSelectable) return
                    if (e.ctrlKey || e.metaKey) {
                      e.preventDefault()
                      window.open(link.url, '_blank', 'noopener,noreferrer')
                    } else {
                      onSelectLink?.(link)
                    }
                  }}
                  style={{
                    width: '100%',
                    textAlign: 'left',
                    padding: '8px 10px',
                    marginBottom: '2px',
                    border: 'none',
                    borderRadius: '4px',
                    background: isSelected ? 'var(--color-success-bg)' : 'transparent',
                    cursor: linkSelectable ? 'pointer' : 'not-allowed',
                    fontSize: '13px',
                    display: 'flex',
                    alignItems: 'center',
                    gap: '8px',
                    color: linkSelectable ? 'var(--color-primary)' : '#999',
                    opacity: linkSelectable ? 1 : 0.7
                  }}
                  onMouseEnter={(e) => {
                    if (linkSelectable && !isSelected) e.currentTarget.style.background = '#f0f0f0'
                  }}
                  onMouseLeave={(e) => {
                    if (!isSelected) e.currentTarget.style.background = 'transparent'
                  }}
                >
                  <span style={{ flex: 1, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', color: 'inherit' }}>
                    {link.title || link.url}
                  </span>
                  <span style={{ flexShrink: 0, fontSize: '14px' }} title={link.status === 'verified' ? 'Verified' : link.status === 'failed' && link.error_message ? link.error_message : link.status === 'pending' || link.status === 'processing' ? 'Processing' : 'Not verified'}>
                    {link.status === 'verified' ? (
                      <span style={{ color: 'var(--color-success)' }} aria-hidden>✓</span>
                    ) : link.status === 'pending' || link.status === 'processing' ? (
                      <span style={{ color: '#ed6c02' }} aria-hidden>…</span>
                    ) : (
                      <span style={{ color: 'var(--color-danger-dark)' }} aria-hidden>✕</span>
                    )}
                  </span>
                </button>
              )
            })
          )}
        </TreeSection>
      </div>
  )

  if (hideHeader) return content

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', minWidth: 0 }}>
      <MaterialsPanelHeader collapsed={collapsed} onCollapsedChange={onCollapsedChange} unreadCount={unreadSet.size} />
      {content}
    </div>
  )
}
