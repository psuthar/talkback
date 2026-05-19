import { useEffect, useRef, useState } from 'react'
import { getMaterialSlides, retryMaterialSlides, updateVideoDisplayTitle } from '../api/materials'
import { SessionPrimaryRow } from './SetPrimaryButton'
import styles from './MaterialsTreePanel.module.css'
import { getPrimaryRecording } from '../utils/session'

const SPINNER_STYLE_ID = 'tb-spinner-keyframes'
function ensureSpinnerStyle() {
  if (typeof document === 'undefined') return
  if (document.getElementById(SPINNER_STYLE_ID)) return
  const style = document.createElement('style')
  style.id = SPINNER_STYLE_ID
  style.textContent = '@keyframes tb-spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }'
  document.head.appendChild(style)
}

/** Small inline spinner shown while async background tasks are running (e.g. slide generation).
 *  SCRUM-450: the rotation lives in an inline style so the keyframe reference
 *  is NOT subject to CSS Modules animation-name scoping. Visual properties
 *  (size, color, border) stay in the .module.css class. */
function InlineSpinner() {
  ensureSpinnerStyle()
  return (
    <span
      aria-hidden
      className={styles.inlineSpinner}
      style={{ animation: 'tb-spin 0.8s linear infinite' }}
    />
  )
}

/** Shared "Materials" header with chevron for creator and participant left panel.
 *  When collapsed and itemCount is provided, renders a vertical rotated label
 *  ("Materials (N)") so the rail announces what is behind it instead of showing
 *  a bare chevron. itemCount is opt-in so existing call sites are unchanged. */
export function MaterialsPanelHeader({ collapsed, onCollapsedChange, unreadCount = 0, itemCount = null }) {
  // After SCRUM-451 the outer panel hosts Context + Members + Materials siblings,
  // so the header label is "Session" rather than "Materials". `itemCount`
  // continues to control the chunky-vs-thin collapsed-rail layout (callers
  // that don't pass it get the chevron-only rail) but is no longer rendered as
  // a numeric badge — that count would have lied because Context/Members are
  // not materials.
  const showSessionRail = collapsed && itemCount != null
  const collapsedAriaLabel = collapsed ? 'Expand session panel' : 'Collapse session panel'
  return (
    <div
      className={`materials-tree-header ${collapsed ? 'materials-tree-header-collapsed' : ''}`}
      style={{
        flexShrink: 0,
        padding: collapsed ? (showSessionRail ? '14px 4px' : '8px 4px') : '10px 12px',
        borderBottom: '1px solid #e0e0e0',
        display: 'flex',
        alignItems: 'center',
        justifyContent: collapsed ? 'center' : 'flex-start',
        gap: '6px',
        cursor: 'pointer',
        ...(collapsed && { minHeight: showSessionRail ? '120px' : '36px' }),
      }}
      onClick={() => onCollapsedChange(!collapsed)}
      onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onCollapsedChange(!collapsed); } }}
      role="button"
      tabIndex={0}
      aria-label={collapsedAriaLabel}
    >
      {showSessionRail ? (
        <span className={styles.panelCollapsedLabel} data-testid="session-collapsed-label">
          Session
        </span>
      ) : (
        <span className={styles.panelHeaderChevron} aria-hidden>{collapsed ? '▷' : '▼'}</span>
      )}
      {collapsed && !showSessionRail && unreadCount > 0 && (
        <span className={styles.panelHeaderBadgeCollapsed} title={`${unreadCount} new material${unreadCount !== 1 ? 's' : ''}`}>
          {unreadCount}
        </span>
      )}
      {!collapsed && (
        <span className={styles.panelHeaderLabel}>
          Session
          {unreadCount > 0 && (
            <span className={styles.panelHeaderBadge} title="New documents added by creator">
              {unreadCount} new material{unreadCount !== 1 ? 's' : ''}
            </span>
          )}
        </span>
      )}
    </div>
  )
}

function VideoTitleEditRow({ currentTitle, saving, onSave, onCancel, value, onChange, error }) {
  return (
    <div style={{ padding: '4px 8px' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
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
      {error && (
        <div data-testid="video-title-error" style={{ marginTop: 4, fontSize: 11, color: 'var(--color-danger-dark)' }}>
          {error}
        </div>
      )}
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

// SCRUM-468: in-progress placeholder for an imported meeting recording.
// Renders an inline spinner + per-state copy so the user sees the import
// is alive immediately after clicking Import, instead of staring at a
// blank VIDEOS list for 3–10 minutes. State-to-copy mapping mirrors the
// session_processing_jobs state machine in internal/models/models.go.
const PROCESSING_PLATFORM_LABEL = {
  zoom: 'Zoom',
  google_meet: 'Google Meet',
  teams: 'Microsoft Teams',
}
function processingCopyFor(job) {
  const platform = PROCESSING_PLATFORM_LABEL[job.source] || job.source
  switch (job.state) {
    case 'queued':
      return `Queued to import from ${platform}`
    case 'fetching':
      return `Looking up ${platform} recording…`
    case 'downloading':
      return `Downloading from ${platform}…`
    case 'parsing':
    case 'chunking':
    case 'embedding':
      return 'Indexing transcript…'
    case 'awaiting_whisper':
      return 'Transcribing (Whisper)…'
    case 'waiting':
    case 'waiting_native_transcript':
      return `Waiting for ${platform} transcript…`
    case 'failed_transient':
      return `Retrying ${platform} import…`
    default:
      return `Importing from ${platform}…`
  }
}
function ProcessingJobRow({ job }) {
  ensureSpinnerStyle()
  const copy = processingCopyFor(job)
  const errMsg = job.last_error_message || ''
  return (
    <div
      data-testid={`processing-job-row-${job.id}`}
      data-state={job.state}
      data-source={job.source}
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 8,
        padding: '6px 8px',
        borderRadius: 4,
        backgroundColor: '#f5f7fb',
        opacity: 0.95,
      }}
    >
      <InlineSpinner />
      <div style={{ display: 'flex', flexDirection: 'column', minWidth: 0 }}>
        <span style={{ fontSize: 13, color: '#444' }}>{copy}</span>
        <span style={{ fontSize: 11, color: '#777' }}>
          {PROCESSING_PLATFORM_LABEL[job.source] || job.source} · this could take a few minutes
        </span>
        {errMsg && (
          <span data-testid={`processing-job-row-${job.id}-error`} style={{ fontSize: 11, color: '#c62828' }}>
            {errMsg}
          </span>
        )}
      </div>
    </div>
  )
}

function TreeItem({ icon, title, meta, metaStyle, selected, onClick, onDelete, deleting, testId, disabled, buttonTitle, primaryBadge, rowHandlers }) {
  const handlers = rowHandlers || {}
  return (
    <div
      className={styles.treeItemRow}
      style={{
        background: selected ? 'var(--color-success-bg)' : 'transparent',
        ...(disabled && { opacity: 0.7 }),
        position: 'relative',
      }}
      onMouseEnter={(e) => {
        if (!selected && !disabled) e.currentTarget.style.background = '#f0f0f0'
      }}
      onMouseLeave={(e) => {
        if (!selected) e.currentTarget.style.background = selected ? 'var(--color-success-bg)' : 'transparent'
      }}
      {...handlers}
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
      {primaryBadge && (
        <span className={styles.treeItemPrimarySlot}>{primaryBadge}</span>
      )}
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
  onDeleteLink,
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
  const [editingTitleError, setEditingTitleError] = useState('')
  const [displayTitleOverrides, setDisplayTitleOverrides] = useState({})

  if (!session) return null

  const { video_sources = [], materials = [], links = [], unread_material_ids = [], primary_video, material_slides_ready = {}, material_slides_status = {}, processing_jobs = [] } = session
  const linkCount = Array.isArray(links) ? links.length : 0
  const newLinkCount = Math.max(0, linkCount - lastSeenLinkCount)
  const unreadSet = new Set((unread_material_ids || []).map((id) => String(id)))
  // SCRUM-293: keep `presentationVideo` for the primary-row identification
  // logic in the merged Videos section (the primary row gets the
  // `primary-video-item` testid). The legacy `additional_videos` /
  // `otherVideos` split is no longer rendered as a separate section.
  // SCRUM-405: presentationVideo follows the canonical primary recording for
  // single- and multi-recording sessions alike. Pre-SCRUM-405 this was
  // `primary_video ?? video_sources[0]`, which getPrimaryRecording matches
  // for the single-recording case while making the multi-recording fallback
  // deterministic (oldest by `created_at`).
  const presentationVideo = getPrimaryRecording({ primary_video, video_sources })
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
    setEditingTitleError('')
    try {
      const trimmed = editingTitleValue.trim() || null
      // SCRUM-436: currentSession's actual prop shape is { session: { id, ... }, video_sources, ... }
      // so session.id is undefined. Resolve the id the same way the rest of
      // this component does (lines 229, 297, 572, 696). Sending undefined here
      // makes the SPA hit /sessions/undefined/... which fails uuid.Parse and
      // returns 400 "Invalid session ID".
      const resolvedSessionId = session?.session?.id || session?.id
      await updateVideoDisplayTitle(apiBaseUrl, resolvedSessionId, videoSourceId, trimmed)
      setDisplayTitleOverrides((prev) => ({ ...prev, [videoSourceId]: trimmed }))
      setSavingTitleId(null)
      setEditingTitleId(null)
    } catch (err) {
      // SCRUM-400: surface the failure instead of silently collapsing the edit
      // row. The previous catch (_) {} masked a 400 from the backend on every
      // save (see SCRUM-400 root cause) and made the save look like a data-
      // loss bug to the user. Keep the edit row open so they can retry.
      setSavingTitleId(null)
      setEditingTitleError(err?.message || 'Failed to save title. Please try again.')
    }
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
        {/* SCRUM-293: single "Videos" section. The split between Presentation
            and Additional Videos was meaningful when the primary video was
            hardcoded to the first upload; now any video can be primary and
            the Primary badge marks which one. The presentation-video testid
            is preserved on whichever row IS currently primary so the
            material-viewers e2e (`getByTestId('primary-video-item')`) keeps
            passing without an e2e change. */}
        {(video_sources.length > 0 || processing_jobs.length > 0) && (
          <TreeSection title="Videos">
            {/* SCRUM-468: placeholder rows for in-progress imports so
                the user gets immediate feedback after clicking Import.
                Rendered BEFORE the real video rows; auto-removed when
                the worker completes and the matching video_source lands. */}
            {processing_jobs.map((job) => (
              <ProcessingJobRow key={job.id} job={job} />
            ))}
            {video_sources.map((v) => {
              const materialId = videoMaterialId(v)
              const videoSelectable = !isProcessingStatus(v?.transcript_status)
              const isPrimaryRow = !!(presentationVideo && String(presentationVideo.id) === String(v.id))
              const rowTestId = isPrimaryRow ? 'primary-video-item' : 'video-item'
              if (editingTitleId === v.id) {
                return (
                  <VideoTitleEditRow
                    key={v.id}
                    currentTitle={videoDisplayTitle(v)}
                    saving={savingTitleId === v.id}
                    value={editingTitleValue}
                    onChange={setEditingTitleValue}
                    onSave={() => saveDisplayTitle(v.id)}
                    onCancel={() => { setEditingTitleId(null); setEditingTitleError('') }}
                    error={editingTitleError}
                  />
                )
              }
              // SCRUM-293: preserve the legacy "Presentation row is always
              // clickable" behavior — only disable non-primary rows when
              // their transcript is still processing. Clicking a row only
              // changes which video occupies the center pane; transcript
              // readiness gates playback features inside the player, not
              // the row itself. The primary row stays clickable so the
              // material-viewers e2e (which waits for clickable
              // primary-video-item right after MP4 upload) keeps passing.
              const rowDisabled = !isPrimaryRow && !videoSelectable
              const videoIsPrimaryRow = !!(sessionId && isPrimaryRow && currentPrimary?.kind === 'video' && currentPrimary?.id)
              // SCRUM-295: enable Make-primary on video rows. The PATCH endpoint
              // expects kind="video" + id=<file_artifact_id>. The legacy
              // `artifact_id` field on VideoSource references the old artifacts
              // table, which is NOT what primary_video_artifact_id keys off — we
              // need `file_artifact_id` (added on VideoSource by SCRUM-295).
              // Migration 038 added the column; pre-migration rows have NULL,
              // so the affordance short-circuits cleanly for legacy rows.
              //
              // Note: we deliberately do NOT gate on videoSelectable. The
              // server requires file_artifact.status=ready (PRIMARY_NOT_READY
              // otherwise), but the upload handler creates the file_artifact
              // status=ready synchronously, so the video is set-primary-able
              // before the (async) transcript pipeline finishes. Failing
              // PATCHes still surface via the inline error region.
              const videoMakePrimaryId = v.file_artifact_id
              const canSetVideoPrimary = !!(canManage && onPrimaryChanged && videoMakePrimaryId && !videoIsPrimaryRow)
              return (
                <div key={v.id}>
                  <SessionPrimaryRow
                    apiBaseUrl={apiBaseUrl}
                    sessionId={sessionId}
                    kind="video"
                    id={videoIsPrimaryRow ? currentPrimary.id : videoMakePrimaryId}
                    isCurrentPrimary={videoIsPrimaryRow}
                    canSetPrimary={canSetVideoPrimary}
                    onSuccess={canManage ? onPrimaryChanged : undefined}
                    itemLabel={videoDisplayTitle(v)}
                  >
                    {({ badge, rowHandlers, menuNode, errorNode }) => (
                      <>
                        <TreeItem
                          testId={rowTestId}
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
                          disabled={rowDisabled}
                          buttonTitle={rowDisabled ? 'Video is still processing' : undefined}
                          primaryBadge={badge}
                          rowHandlers={rowHandlers}
                        />
                        {menuNode}
                        {errorNode}
                      </>
                    )}
                  </SessionPrimaryRow>
                  {canManage && (
                    <button
                      data-testid="edit-video-title-btn"
                      type="button"
                      onClick={() => { setEditingTitleId(v.id); setEditingTitleValue(v.display_title || ''); setEditingTitleError('') }}
                      style={{ fontSize: 11, color: '#888', background: 'none', border: 'none', cursor: 'pointer', padding: '0 8px 4px' }}
                    >
                      Edit title
                    </button>
                  )}
                </div>
              )
            })}
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
                  <SessionPrimaryRow
                    apiBaseUrl={apiBaseUrl}
                    sessionId={sessionId}
                    kind="document"
                    id={m.id}
                    isCurrentPrimary={isCurrentPrimary}
                    canSetPrimary={!!(canManage && onPrimaryChanged && viewable)}
                    onSuccess={canManage ? onPrimaryChanged : undefined}
                    itemLabel={m.title || m.filename}
                  >
                    {({ badge, rowHandlers, menuNode, errorNode }) => (
                      <>
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
                          primaryBadge={badge}
                          rowHandlers={rowHandlers}
                        />
                        {menuNode}
                        {errorNode}
                      </>
                    )}
                  </SessionPrimaryRow>
                  {/* SCRUM-287: surface a Retry-slides button when the slides
                      pipeline failed for this .ppt/.pptx so the creator can
                      recover from a transient pdftoppm/soffice error
                      without re-uploading. */}
                  {canManage && sessionId && isSlideDeckMaterial(m) && resolveSlidesStatus(m) === 'failed' && (
                    <div style={{ padding: '0 8px 4px 24px' }}>
                      <RetrySlidesButton
                        apiBaseUrl={apiBaseUrl}
                        sessionId={sessionId}
                        materialId={m.id}
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
              // SCRUM-289: image materials are eligible to be the session
              // primary too. The backend's primary_content_kind='document'
              // is the entity-pointer ("any material row"), not a content
              // label, so reusing kind="document" for image PATCHes is
              // correct without a schema change.
              const isCurrentPrimary = !!(
                currentPrimary && currentPrimary.kind === 'document' &&
                String(currentPrimary.id) === String(m.id)
              )
              return (
                <div key={m.id}>
                  <SessionPrimaryRow
                    apiBaseUrl={apiBaseUrl}
                    sessionId={sessionId}
                    kind="document"
                    id={m.id}
                    isCurrentPrimary={isCurrentPrimary}
                    canSetPrimary={!!(canManage && onPrimaryChanged && viewable)}
                    onSuccess={canManage ? onPrimaryChanged : undefined}
                    itemLabel={m.filename}
                  >
                    {({ badge, rowHandlers, menuNode, errorNode }) => (
                      <>
                        <TreeItem
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
                          primaryBadge={badge}
                          rowHandlers={rowHandlers}
                        />
                        {menuNode}
                        {errorNode}
                      </>
                    )}
                  </SessionPrimaryRow>
                </div>
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
                  <SessionPrimaryRow
                    apiBaseUrl={apiBaseUrl}
                    sessionId={sessionId}
                    kind="link"
                    id={link.id}
                    isCurrentPrimary={isLinkPrimary}
                    canSetPrimary={!!(canManage && onPrimaryChanged && linkSelectable)}
                    onSuccess={canManage ? onPrimaryChanged : undefined}
                    itemLabel={link.title || link.url}
                  >
                    {({ badge, rowHandlers, menuNode, errorNode }) => (
                      <>
                        <div className={styles.linkRow} {...rowHandlers}>
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
                            {/* SCRUM-296: collapse the verified `✓` so the row
                               grammar matches docs/images ([title] [×]).
                               Pending/failed still render so users see signal. */}
                            {link.status !== 'verified' && (
                              <span className={styles.linkStatusIcon} title={link.status === 'failed' && link.error_message ? link.error_message : link.status === 'pending' || link.status === 'processing' ? 'Processing' : 'Not verified'}>
                                {link.status === 'pending' || link.status === 'processing' ? (
                                  <span style={{ color: '#ed6c02' }} aria-hidden>…</span>
                                ) : (
                                  <span style={{ color: 'var(--color-danger-dark)' }} aria-hidden>✕</span>
                                )}
                              </span>
                            )}
                          </button>
                          {badge && (
                            <span className={styles.treeItemPrimarySlot}>{badge}</span>
                          )}
                          {canManage && onDeleteLink && (
                            <button
                              type="button"
                              data-testid="link-delete-btn"
                              onClick={(e) => { e.stopPropagation(); onDeleteLink(link.id) }}
                              disabled={deletingId === `link-${link.id}`}
                              aria-label={`Remove ${link.title || link.url}`}
                              title={`Remove ${link.title || link.url}`}
                              className={styles.treeItemDeleteBtn}
                              style={{
                                color: deletingId === `link-${link.id}` ? '#bbb' : '#999',
                                cursor: deletingId === `link-${link.id}` ? 'not-allowed' : 'pointer',
                              }}
                              onMouseEnter={(e) => { if (deletingId !== `link-${link.id}`) e.currentTarget.style.color = 'var(--color-danger-dark)' }}
                              onMouseLeave={(e) => { if (deletingId !== `link-${link.id}`) e.currentTarget.style.color = '#999' }}
                            >
                              {deletingId === `link-${link.id}` ? '…' : '×'}
                            </button>
                          )}
                        </div>
                        {menuNode}
                        {errorNode}
                      </>
                    )}
                  </SessionPrimaryRow>
                </div>
              )
            })}
          </TreeSection>
        )}

        {video_sources.length === 0 && documentMaterials.length === 0 && imageMaterials.length === 0 && (!Array.isArray(links) || links.length === 0) && (hideTranscriptSection || video_sources.filter(v => v.transcript_text).length === 0) && (
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

/**
 * RetrySlidesButton — creator-only "Retry slides" inline button shown next
 * to a .ppt/.pptx row whose slides pipeline failed (SCRUM-287). Resolves
 * on a 202 enqueue from the new POST /api/sessions/:id/materials/:mid/
 * retry-slides endpoint; the actual outcome propagates via the
 * session_updated WebSocket event when the background goroutine finishes.
 */
function RetrySlidesButton({ apiBaseUrl, sessionId, materialId, onSuccess }) {
	const [pending, setPending] = useState(false)
	const [error, setError] = useState(null)
	const onClick = async () => {
		setPending(true)
		setError(null)
		try {
			await retryMaterialSlides(apiBaseUrl, sessionId, materialId)
			onSuccess?.()
		} catch (e) {
			setError(e.message || 'Retry failed')
		} finally {
			setPending(false)
		}
	}
	return (
		<span style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
			<button
				type="button"
				data-testid="retry-slides-btn"
				aria-label="Retry slides derivation"
				onClick={onClick}
				disabled={pending}
				style={{
					fontSize: 11,
					color: '#666',
					background: 'none',
					border: 'none',
					padding: 0,
					cursor: pending ? 'not-allowed' : 'pointer',
					textDecoration: 'underline',
				}}
			>
				{pending ? 'Retrying…' : 'Retry slides'}
			</button>
			{error && (
				<span
					data-testid="retry-slides-error"
					role="alert"
					aria-live="polite"
					style={{ fontSize: 11, color: 'var(--color-danger-dark, #b00020)' }}
				>
					{error}
				</span>
			)}
		</span>
	)
}
