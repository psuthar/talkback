import { useRef } from 'react'
import { getMaterialIcon } from '../utils/materialIcons'

const ICON_VIDEO = '▶'
const ICON_TRANSCRIPT = '📝'
const ICON_LINK = '🔗'

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

function TreeItem({ icon, title, meta, selected, onClick }) {
  return (
    <button
      type="button"
      onClick={onClick}
      style={{
        width: '100%',
        textAlign: 'left',
        padding: '8px 10px',
        marginBottom: '2px',
        border: 'none',
        borderRadius: '4px',
        background: selected ? '#e8f5e9' : 'transparent',
        cursor: 'pointer',
        fontSize: '13px',
        display: 'flex',
        alignItems: 'center',
        gap: '8px',
        color: '#1a1a1a'
      }}
      onMouseEnter={(e) => {
        if (!selected) e.currentTarget.style.background = '#f0f0f0'
      }}
      onMouseLeave={(e) => {
        if (!selected) e.currentTarget.style.background = 'transparent'
      }}
    >
      <span style={{ flexShrink: 0 }}>{icon}</span>
      <span style={{ flex: 1, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', color: 'inherit' }}>
        {title}
      </span>
      {meta && (
        <span style={{ fontSize: '11px', color: '#555', flexShrink: 0 }}>{meta}</span>
      )}
    </button>
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
  selectedDocumentId,
  collapsed,
  onCollapsedChange,
  hideTranscriptSection = false
}) {
  const scrollRef = useRef(null)

  if (!session) return null

  const { video_sources = [], materials = [], unread_material_ids = [] } = session
  const unreadSet = new Set(unread_material_ids || [])
  const documents = materials.filter(m => (m.kind || '').toLowerCase() === 'document')
  const slidesImages = materials.filter(m => {
    const k = (m.kind || '').toLowerCase()
    return k === 'slides' || k === 'diagram'
  })

  const isMaterialImage = (m) => {
    const ct = (m.content_type || '').toLowerCase()
    const fn = (m.filename || '').toLowerCase()
    return ct.startsWith('image/') || ['.jpg', '.jpeg', '.png', '.gif', '.webp', '.bmp', '.svg'].some(e => fn.endsWith(e))
  }
  const materialStatusMeta = (m) => (isMaterialImage(m) ? 'N/A' : (m.text_status === 'ready' ? 'Ready' : (m.content_type || '')))
  const materialStatusMetaSlides = (m) => (isMaterialImage(m) ? 'N/A' : (m.text_status === 'ready' ? 'Ready' : ''))

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', minWidth: 0 }}>
      <div
        className={`materials-tree-header ${collapsed ? 'materials-tree-header-collapsed' : ''}`}
        style={{
          flexShrink: 0,
          padding: collapsed ? '8px 4px' : '10px 12px',
          borderBottom: '1px solid #e0e0e0',
          display: 'flex',
          alignItems: 'center',
          justifyContent: collapsed ? 'center' : 'space-between',
          gap: '8px',
          ...(collapsed && { cursor: 'pointer', minHeight: '36px' })
        }}
        onClick={collapsed ? () => onCollapsedChange(false) : undefined}
        onKeyDown={collapsed ? (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onCollapsedChange(false); } } : undefined}
        role={collapsed ? 'button' : undefined}
        tabIndex={collapsed ? 0 : undefined}
        aria-label={collapsed ? 'Expand materials panel' : undefined}
      >
        {!collapsed && (
          <span style={{ fontWeight: 'bold', fontSize: '14px', display: 'flex', alignItems: 'center', gap: '6px' }}>
            Materials
            {unreadSet.size > 0 && (
              <span style={{
                background: '#e65100',
                color: '#fff',
                fontSize: '10px',
                fontWeight: 'bold',
                padding: '2px 6px',
                borderRadius: '10px'
              }} title="New documents added by creator">
                New {unreadSet.size}
              </span>
            )}
          </span>
        )}
        <button
          type="button"
          onClick={(e) => { e.stopPropagation(); onCollapsedChange(!collapsed); }}
          aria-label={collapsed ? 'Expand materials panel' : 'Collapse materials panel'}
          style={{
            padding: '4px 8px',
            fontSize: '12px',
            border: '1px solid #ccc',
            borderRadius: '4px',
            background: '#fff',
            cursor: 'pointer',
            flexShrink: 0
          }}
        >
          {collapsed ? '▶' : '◀'}
        </button>
      </div>
      <div ref={scrollRef} className="materials-tree-content" style={{ flex: 1, minHeight: 0, overflowY: 'auto', padding: '8px' }}>
        <TreeSection title="Video">
          {video_sources.length === 0 ? (
            <div style={{ fontSize: '12px', color: '#999', padding: '4px 0' }}>None</div>
          ) : (
            video_sources.map((v, idx) => (
              <TreeItem
                key={v.id}
                icon={ICON_VIDEO}
                title={`Video ${idx + 1} – ${v.provider || 'Video'}`}
                meta={v.transcript_status === 'ready' ? 'Ready' : v.transcript_status || ''}
                selected={selectedVideo?.id === v.id}
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
                    icon={ICON_TRANSCRIPT}
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
            <div style={{ fontSize: '12px', color: '#999', padding: '4px 0' }}>None</div>
          ) : (
            documents.map(m => (
              <TreeItem
                key={m.id}
                icon={getMaterialIcon(m)}
                title={m.filename || 'Untitled'}
                meta={[materialStatusMeta(m), unreadSet.has(m.id) ? 'New' : null].filter(Boolean).join(' • ')}
                selected={selectedDocumentId === m.id}
                onClick={(e) => onSelectDocument(m, e)}
              />
            ))
          )}
        </TreeSection>

        <TreeSection title="Slides / Images">
          {slidesImages.length === 0 ? (
            <div style={{ fontSize: '12px', color: '#999', padding: '4px 0' }}>None</div>
          ) : (
            slidesImages.map(m => (
              <TreeItem
                key={m.id}
                icon={getMaterialIcon(m)}
                title={m.filename || 'Untitled'}
                meta={[materialStatusMetaSlides(m), unreadSet.has(m.id) ? 'New' : null].filter(Boolean).join(' • ')}
                selected={selectedDocumentId === m.id}
                onClick={(e) => onSelectDocument(m, e)}
              />
            ))
          )}
        </TreeSection>

        <TreeSection title="Links">
          <div style={{ fontSize: '12px', color: '#999', padding: '4px 0' }}>None</div>
        </TreeSection>
      </div>
    </div>
  )
}
