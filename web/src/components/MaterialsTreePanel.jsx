import { useRef } from 'react'

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
              fontSize: '10px',
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
        fontSize: '11px',
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
        background: selected ? '#e8f5e9' : 'transparent',
        ...(disabled && { opacity: 0.7 }),
      }}
      onMouseEnter={(e) => {
        if (!selected && !disabled) e.currentTarget.style.background = '#f0f0f0'
      }}
      onMouseLeave={(e) => {
        if (!selected) e.currentTarget.style.background = selected ? '#e8f5e9' : 'transparent'
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
          <span style={{ fontSize: '11px', color: '#555', flexShrink: 0, ...metaStyle }}>{meta}</span>
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
          onMouseEnter={(e) => { if (!deleting) e.currentTarget.style.color = '#c62828' }}
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
}) {
  const scrollRef = useRef(null)

  if (!session) return null

  const { video_sources = [], materials = [], links = [], unread_material_ids = [], primary_video, additional_videos = [], material_slides_ready = {} } = session
  const linkCount = Array.isArray(links) ? links.length : 0
  const newLinkCount = Math.max(0, linkCount - lastSeenLinkCount)
  const unreadSet = new Set((unread_material_ids || []).map((id) => String(id)))
  const presentationVideo = primary_video ?? (video_sources?.length > 0 ? video_sources[0] : null)
  const otherVideos = (additional_videos?.length >= 0 ? additional_videos : (video_sources?.slice(1) ?? []))
  const documents = materials.filter(m => {
    const k = (m.kind || '').toLowerCase()
    return (k === 'document' || k === 'other') && k !== 'video' // exclude video: they appear in Additional Videos via video_sources
  })
  const slidesImages = materials.filter(m => {
    const k = (m.kind || '').toLowerCase()
    return k === 'slides' || k === 'diagram'
  })

  const isMaterialImage = (m) => {
    const ct = (m.content_type || '').toLowerCase()
    const fn = (m.filename || '').toLowerCase()
    return ct.startsWith('image/') || ['.jpg', '.jpeg', '.png', '.gif', '.webp', '.bmp', '.svg'].some(e => fn.endsWith(e))
  }
  const materialStatusMeta = (m) => {
    if (isMaterialImage(m)) return null
    if (m.text_status === 'ready') return { label: 'Ready', color: '#2e7d32' }
    if (m.text_status === 'pending' || m.text_status === 'processing') return { label: 'Processing…', color: '#e65100' }
    if (m.text_status === 'failed') return { label: 'Failed', color: '#c62828' }
    return null
  }
  // For slides/diagram materials: show "Processing…" and treat as not viewable until slides manifest exists.
  const materialStatusMetaSlides = (m) => {
    const isSlidesKind = (m.kind || '').toLowerCase() === 'slides'
    const slidesReady = isSlidesKind && material_slides_ready[String(m.id)]
    if (isSlidesKind && !slidesReady) return { label: 'Processing…', color: '#e65100' }
    return materialStatusMeta(m)
  }
  const isSlidesMaterialViewable = (m) => {
    if ((m.kind || '').toLowerCase() !== 'slides') return true
    return material_slides_ready[String(m.id)] === true
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
              <TreeItem
                key={v.id}
                testId="video-item"
                icon={null}
                title={videoDisplayTitle(v)}
                meta={v.transcript_status === 'ready' ? 'Ready' : v.transcript_status || ''}
                selected={!selectedDocumentId && selectedVideo != null && String(selectedVideo.id) === String(v.id)}
                onClick={() => {
                  setSelectedVideo(v)
                  setVideoId(v.id)
                  setVideoPlayerKey(prev => prev + 1)
                  onSelectVideo?.()
                }}
              />
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
          {documents.length === 0 ? (
            <div style={{ fontSize: '12px', color: '#999', padding: '4px 0' }}>
              None
            </div>
          ) : (
            documents.map(m => {
              const statusInfo = materialStatusMeta(m)
              const metaParts = [statusInfo?.label, unreadSet.has(String(m.id)) ? 'New' : null].filter(Boolean)
              return (
                <TreeItem
                  key={m.id}
                  testId="material-item"
                  icon={null}
                  title={m.title || m.filename || 'Untitled'}
                  meta={metaParts.join(' • ')}
                  metaStyle={statusInfo?.color ? { color: statusInfo.color } : undefined}
                  selected={selectedDocumentId === m.id}
                  onClick={(e) => onSelectDocument(m, e)}
                  onDelete={canManage && onDeleteMaterial ? () => onDeleteMaterial(m.id) : undefined}
                  deleting={deletingId === String(m.id)}
                />
              )
            })
          )}
        </TreeSection>

        <TreeSection title="Slides / Images">
          {slidesImages.length === 0 ? (
            <div style={{ fontSize: '12px', color: '#999', padding: '4px 0' }}>
              None
            </div>
          ) : (
            slidesImages.map(m => {
              const statusInfo = materialStatusMetaSlides(m)
              const metaParts = [statusInfo?.label, unreadSet.has(String(m.id)) ? 'New' : null].filter(Boolean)
              const viewable = isSlidesMaterialViewable(m)
              return (
                <TreeItem
                  key={m.id}
                  testId="slides-item"
                  icon={null}
                  title={m.filename || 'Untitled'}
                  meta={metaParts.join(' • ')}
                  metaStyle={statusInfo?.color ? { color: statusInfo.color } : undefined}
                  selected={selectedDocumentId === m.id}
                  onClick={(e) => onSelectDocument(m, e)}
                  onDelete={canManage && onDeleteMaterial ? () => onDeleteMaterial(m.id) : undefined}
                  deleting={deletingId === String(m.id)}
                  disabled={!viewable}
                  buttonTitle={!viewable ? 'Slides are still processing' : undefined}
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
              return (
                <button
                  key={link.id}
                  type="button"
                  data-testid="link-item"
                  onClick={(e) => {
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
                    background: isSelected ? '#e8f5e9' : 'transparent',
                    cursor: 'pointer',
                    fontSize: '13px',
                    display: 'flex',
                    alignItems: 'center',
                    gap: '8px',
                    color: '#1976d2'
                  }}
                  onMouseEnter={(e) => {
                    if (!isSelected) e.currentTarget.style.background = '#f0f0f0'
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
                      <span style={{ color: '#2e7d32' }} aria-hidden>✓</span>
                    ) : link.status === 'pending' || link.status === 'processing' ? (
                      <span style={{ color: '#ed6c02' }} aria-hidden>…</span>
                    ) : (
                      <span style={{ color: '#c62828' }} aria-hidden>✕</span>
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
