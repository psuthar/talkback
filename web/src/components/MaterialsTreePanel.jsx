import { useEffect, useRef, useState } from 'react'
import { getMaterialSlides, updateVideoDisplayTitle } from '../api/materials'
import { SetPrimaryButton } from './SetPrimaryButton'
import styles from './MaterialsTreePanel.module.css'

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
  return <span aria-hidden className={styles.inlineSpinner} />
}

/** Shared "Materials" header with chevron for creator and participant left panel.
 *  When collapsed and itemCount is provided, renders a vertical rotated label
 *  ("Materials (N)") so the rail announces what is behind it instead of showing
 *  a bare chevron. itemCount is opt-in so existing call sites are unchanged. */
export function MaterialsPanelHeader({ collapsed, onCollapsedChange, unreadCount = 0, itemCount = null }) {
  const showCountedCollapsed = collapsed && itemCount != null
  const collapsedAriaLabel = collapsed
    ? `Expand materials panel${itemCount != null ? ` (${itemCount} item${itemCount !== 1 ? 's' : ''})` : ''}`
    : 'Collapse materials panel'
  return (
    <div
      className={`materials-tree-header ${collapsed ? 'materials-tree-header-collapsed' : ''}`}
      style={{
        flexShrink: 0,
        padding: collapsed ? (showCountedCollapsed ? '14px 4px' : '8px 4px') : '10px 12px',
        borderBottom: '1px solid #e0e0e0',
        display: 'flex',
        alignItems: 'center',
        justifyContent: collapsed ? 'center' : 'flex-start',
        gap: '6px',
        cursor: 'pointer',
        ...(collapsed && { minHeight: showCountedCollapsed ? '120px' : '36px' }),
      }}
      onClick={() => onCollapsedChange(!collapsed)}
      onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onCollapsedChange(!collapsed); } }}
      role="button"
      tabIndex={0}
      aria-label={collapsedAriaLabel}
    >
      {showCountedCollapsed ? (
        <span className={styles.panelCollapsedLabel} data-testid="materials-collapsed-label">
          Materials ({itemCount})
        </span>
      ) : (
        <span className={styles.panelHeaderChevron} aria-hidden>{collapsed ? '▷' : '▼'}</span>
      )}
      {collapsed && !showCountedCollapsed && unreadCount > 0 && (
        <span className={styles.panelHeaderBadgeCollapsed} title={`${unreadCount} new material${unreadCount !== 1 ? 's' : ''}`}>
          {unreadCount}
        </span>
      )}
      {!collapsed && (
        <span className={styles.panelHeaderLabel}>
          Materials
          {unreadCount > 0 && (
            <span className={styles.panelHeaderBadge} title="New documents added by creator">
              New {unreadCount}
            </span>
          )}
        </span>
      )}
    </div>
  )
}

function VideoTitleEditRow({ currentTitle, saving, onSave, onCancel, value, onChange }) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 4, padding: '4px 8px' }}>
      <input
        data-testid="video-title-input"
        autoFocus
        type="text"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter') { e.preventDefault(); onSave() }
          if (e.key === 'Escape') onCancel()
        }}
        placeholder={currentTitle}
        style={{ flex: 1, fontSize: 12, padding: '2px 4px', border: '1px solid #ccc', borderRadius: 3 }}
        disabled={saving}
      />
      <button type="button" onClick={onSave} disabled={saving} style={{ fontSize: 11, padding: '2px 6px', cursor: saving ? 'not-allowed' : 'pointer' }}>
        {saving ? '…' : 'Save'}
      </button>
      <button type="button" onClick={onCancel} disabled={saving} style={{ fontSize: 11, padding: '2px 6px', cursor: saving ? 'not-allowed' : 'pointer' }}>
        Cancel
      </button>
    </div>
  )
}

function TreeSection({ title, children }) {
  return (
    <div className={styles.treeSection}>
      <div className={styles.treeSectionTitle}>{title}</div>
      <div className={styles.treeSectionBody}>{children}</div>
    </div>
  )
}

function TreeItem({ icon, title, meta, metaStyle, selected, onClick, onDelete, deleting, testId, disabled, buttonTitle }) {
  return (
    <div
      className={styles.treeItemRow}
      style={{
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
        className={styles.treeItemBtn}
        style={{
          cursor: disabled ? 'not-allowed' : 'pointer',
          color: disabled ? '#888' : '#1a1a1a',
        }}
      >
        {icon != null && icon !== '' && <span className={styles.treeItemIcon}>{icon}</span>}
        <span className={styles.treeItemTitle}>{title}</span>
        {meta && (
          <span className={styles.treeItemMeta} style={metaStyle}>{meta}</span>
        )}
      </button>
      {onDelete && (
        <button
          type="button"
          onClick={(e) => { e.stopPropagation(); onDelete() }}
          disabled={deleting}
          aria-label={`Remove ${title}`}
          title={`Remove ${title}`}
          className={styles.treeItemDeleteBtn}
          style={{
            color: deleting ? '#bbb' : '#999',
            cursor: deleting ? 'not-allowed' : 'pointer',
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
  // SCRUM-275: creator-only "Make primary" affordance. When canManage is
  // true and onPrimaryChanged is provided, document rows render
  // SetPrimaryButton next to the title. currentPrimary lets the panel
  // badge the row that's already the session primary.
  currentPrimary = null,
  onPrimaryChanged,
}) {
  const scrollRef = useRef(null)
  const [probedSlidesStatus, setProbedSlidesStatus] = useState({})
  const [editingTitleId, setEditingTitleId] = useState(null)
  const [editingTitleValue, setEditingTitleValue] = useState('')
  const [savingTitleId, setSavingTitleId] = useState(null)
  const [displayTitleOverrides, setDisplayTitleOverrides] = useState({})

  if (!session) return null

  const { video_sources = [], materials = [], links = [], unread_material_ids = [], primary_video, additional_videos = [], material_slides_ready = {}, material_slides_status = {} } = session
  const linkCount = Array.isArray(links) ? links.length : 0
  const newLinkCount = Math.max(0, linkCount - lastSeenLinkCount)
  const unreadSet = new Set((unread_material_ids || []).map((id) => String(id)))
  const presentationVideo = primary_video ?? (video_sources?.length > 0 ? video_sources[0] : null)
  const otherVideos = (additional_videos?.length >= 0 ? additional_videos : (video_sources?.slice(1) ?? []))
  const sessionId = session?.session?.id || session?.id

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
          <span className={styles.inlineStatusRow}>
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
    if (v?.id && displayTitleOverrides[v.id] !== undefined) {
      return displayTitleOverrides[v.id] || videoUrlFallbackTitle(v)
    }
    if (v?.display_title) return v.display_title
    return videoUrlFallbackTitle(v)
  }
  const videoUrlFallbackTitle = (v) => {
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
      return seg || v?.provider || 'Video'
    }
    return v?.provider || 'Video'
  }
  const saveDisplayTitle = async (videoSourceId) => {
    setSavingTitleId(videoSourceId)
    try {
      const trimmed = editingTitleValue.trim() || null
      await updateVideoDisplayTitle(apiBaseUrl, session.id, videoSourceId, trimmed)
      setDisplayTitleOverrides((prev) => ({ ...prev, [videoSourceId]: trimmed }))
    } catch (_) {}
    setSavingTitleId(null)
    setEditingTitleId(null)
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
    <div ref={scrollRef} className={`materials-tree-content ${styles.scrollArea}`}>
        {deleteError && (
          <div className="error" style={{ marginBottom: 8, fontSize: 12, padding: '4px 6px' }}>
            {deleteError}
          </div>
        )}
        <TreeSection title="Presentation">
          {!presentationVideo ? (
            <div className={styles.emptyNote}>No presentation video selected yet.</div>
          ) : editingTitleId === presentationVideo.id ? (
            <VideoTitleEditRow
              currentTitle={videoDisplayTitle(presentationVideo)}
              saving={savingTitleId === presentationVideo.id}
              value={editingTitleValue}
              onChange={setEditingTitleValue}
              onSave={() => saveDisplayTitle(presentationVideo.id)}
              onCancel={() => setEditingTitleId(null)}
            />
          ) : (
            <>
              <TreeItem
                key={presentationVideo.id}
                testId="primary-video-item"
                icon={null}
                title={videoDisplayTitle(presentationVideo)}
                /* SCRUM-286: replace the static "Primary" meta string with the
                   SetPrimaryButton badge + Clear control rendered below so
                   video rows match the document/link row visual treatment.
                   When the session primary isn't actually kind=video (e.g. a
                   document is primary but a fallback video shows here), no
                   badge appears. */
                meta=""
                selected={!selectedDocumentId && selectedVideo != null && String(selectedVideo.id) === String(presentationVideo.id)}
                onClick={() => {
                  setSelectedVideo(presentationVideo)
                  setVideoId(presentationVideo.id)
                  setVideoPlayerKey(prev => prev + 1)
                  onSelectVideo?.()
                }}
              />
              {canManage && onPrimaryChanged && sessionId && currentPrimary?.kind === 'video' && currentPrimary?.id && (
                <span style={{ paddingLeft: 8 }}>
                  <SetPrimaryButton
                    apiBaseUrl={apiBaseUrl}
                    sessionId={sessionId}
                    kind="video"
                    id={currentPrimary.id}
                    isCurrentPrimary={true}
                    onSuccess={onPrimaryChanged}
                    itemLabel={videoDisplayTitle(presentationVideo)}
                  />
                </span>
              )}
              {canManage && (
                <button
                  data-testid="edit-video-title-btn"
                  type="button"
                  onClick={() => { setEditingTitleId(presentationVideo.id); setEditingTitleValue(presentationVideo.display_title || '') }}
                  style={{ fontSize: 11, color: '#888', background: 'none', border: 'none', cursor: 'pointer', padding: '0 8px 4px' }}
                >
                  Edit title
                </button>
              )}
            </>
          )}
        </TreeSection>

        {otherVideos.length > 0 && (
          <TreeSection title="Additional Videos">
            {otherVideos.map((v) => (
              (() => {
                const materialId = videoMaterialId(v)
                const videoSelectable = !isProcessingStatus(v?.transcript_status)
                return editingTitleId === v.id ? (
                  <VideoTitleEditRow
                    key={v.id}
                    currentTitle={videoDisplayTitle(v)}
                    saving={savingTitleId === v.id}
                    value={editingTitleValue}
                    onChange={setEditingTitleValue}
                    onSave={() => saveDisplayTitle(v.id)}
                    onCancel={() => setEditingTitleId(null)}
                  />
                ) : (
                  <div key={v.id}>
                    <TreeItem
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
                    {canManage && (
                      <button
                        data-testid="edit-video-title-btn"
                        type="button"
                        onClick={() => { setEditingTitleId(v.id); setEditingTitleValue(v.display_title || '') }}
                        style={{ fontSize: 11, color: '#888', background: 'none', border: 'none', cursor: 'pointer', padding: '0 8px 4px' }}
                      >
                        Edit title
                      </button>
                    )}
                  </div>
                )
              })()
            ))}
          </TreeSection>
        )}

        {!hideTranscriptSection && video_sources.filter(v => v.transcript_text).length > 0 && (
          <TreeSection title="Transcript">
            {video_sources.map((v, idx) => {
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
            })}
          </TreeSection>
        )}

        {documentMaterials.length > 0 && (
          <TreeSection title="Documents">
            {documentMaterials.map(m => {
              const statusInfo = materialStatusMetaDocument(m)
              const isNew = unreadSet.has(String(m.id))
              const metaNode = (statusInfo?.label || isNew)
                ? (
                  <span className={styles.linkStatusGap}>
                    {statusInfo?.label}
                    {statusInfo?.label && isNew && <span> • </span>}
                    {isNew && <span>New</span>}
                  </span>
                )
                : null
              const viewable = isMaterialViewable(m)
              const isCurrentPrimary = !!(
                currentPrimary && currentPrimary.kind === 'document' &&
                String(currentPrimary.id) === String(m.id)
              )
              const sessionId = session?.session?.id || session?.id
              return (
                <div key={m.id}>
                  <TreeItem
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
                  {canManage && onPrimaryChanged && sessionId && viewable && (
                    <div style={{ padding: '0 8px 4px 24px' }}>
                      <SetPrimaryButton
                        apiBaseUrl={apiBaseUrl}
                        sessionId={sessionId}
                        kind="document"
                        id={m.id}
                        isCurrentPrimary={isCurrentPrimary}
                        onSuccess={onPrimaryChanged}
                      />
                    </div>
                  )}
                </div>
              )
            })}
          </TreeSection>
        )}

        {imageMaterials.length > 0 && (
          <TreeSection title="Images">
            {imageMaterials.map(m => {
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
            })}
          </TreeSection>
        )}

        {Array.isArray(links) && links.length > 0 && (
          <TreeSection title={newLinkCount > 0 ? `Links • New ${newLinkCount}` : 'Links'}>
            {links.map((link) => {
              const linkDocId = `link-${link.id}`
              const isSelected = selectedDocumentId === linkDocId
              const linkSelectable = !isProcessingStatus(link.status)
              const isLinkPrimary = !!(
                currentPrimary && currentPrimary.kind === 'link' &&
                String(currentPrimary.id) === String(link.id)
              )
              const sessionId = session?.session?.id || session?.id
              return (
                <div key={link.id}>
                  <button
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
                    className={styles.linkBtn}
                    style={{
                      background: isSelected ? 'var(--color-success-bg)' : 'transparent',
                      cursor: linkSelectable ? 'pointer' : 'not-allowed',
                      color: linkSelectable ? 'var(--color-primary)' : '#999',
                      opacity: linkSelectable ? 1 : 0.7,
                    }}
                    onMouseEnter={(e) => {
                      if (linkSelectable && !isSelected) e.currentTarget.style.background = '#f0f0f0'
                    }}
                    onMouseLeave={(e) => {
                      if (!isSelected) e.currentTarget.style.background = 'transparent'
                    }}
                  >
                    <span className={styles.linkTitleCell}>
                      {link.title || link.url}
                    </span>
                    <span className={styles.linkStatusIcon} title={link.status === 'verified' ? 'Verified' : link.status === 'failed' && link.error_message ? link.error_message : link.status === 'pending' || link.status === 'processing' ? 'Processing' : 'Not verified'}>
                      {link.status === 'verified' ? (
                        <span style={{ color: 'var(--color-success)' }} aria-hidden>✓</span>
                      ) : link.status === 'pending' || link.status === 'processing' ? (
                        <span style={{ color: '#ed6c02' }} aria-hidden>…</span>
                      ) : (
                        <span style={{ color: 'var(--color-danger-dark)' }} aria-hidden>✕</span>
                      )}
                    </span>
                  </button>
                  {/* SCRUM-276: extend the SCRUM-275 primary badge / button to link rows. */}
                  {canManage && onPrimaryChanged && sessionId && linkSelectable && (
                    <div style={{ padding: '0 8px 4px 24px' }}>
                      <SetPrimaryButton
                        apiBaseUrl={apiBaseUrl}
                        sessionId={sessionId}
                        kind="link"
                        id={link.id}
                        isCurrentPrimary={isLinkPrimary}
                        onSuccess={onPrimaryChanged}
                      />
                    </div>
                  )}
                </div>
              )
            })}
          </TreeSection>
        )}

        {!presentationVideo && otherVideos.length === 0 && documentMaterials.length === 0 && imageMaterials.length === 0 && (!Array.isArray(links) || links.length === 0) && (hideTranscriptSection || video_sources.filter(v => v.transcript_text).length === 0) && (
          <div className={styles.emptyNote} data-testid="no-materials-message">No materials yet — creator can add files.</div>
        )}
      </div>
  )

  if (hideHeader) return content

  return (
    <div className={styles.contentPanel}>
      <MaterialsPanelHeader collapsed={collapsed} onCollapsedChange={onCollapsedChange} unreadCount={unreadSet.size} />
      {content}
    </div>
  )
}
