import { useEffect, useState, useRef, useCallback } from 'react'
import { VideoPlayer } from '../VideoPlayer'
import { QAHistory } from '../components/QAHistory'
import { MaterialsTreePanel, MaterialsPanelHeader } from '../components/MaterialsTreePanel'
import { QAPanel } from '../components/QAPanel'
import { TranscriptViewer } from '../components/TranscriptViewer'
import { DocumentViewer } from '../components/DocumentViewer'
import { getDefaultApiBaseUrl } from '../config'
import { VideoStartOverlay } from '../components/VideoStartOverlay'

const STORAGE_KEY_MATERIALS_COLLAPSED = 'talkback.participant.materialsCollapsed'

export function ParticipantMode({
  authUser,
  currentSession,
  selectedVideo,
  selectedVideoIdRef,
  setSelectedVideo,
  setVideoId,
  videoPlayerKey,
  setVideoPlayerKey,
  currentVideoTime,
  setCurrentVideoTime,
  isVideoPlaying,
  setIsVideoPlaying,
  handleVideoPlayerEvent,
  handleVideoTimeUpdate,
  getVideoEmbedUrl,
  transcriptJobs,
  questions,
  unreadQuestionIds = [],
  markQuestionViewed,
  fetchSessionQuestions,
  loading,
  apiBaseUrl,
  creatorIdentity,
  questionText,
  setQuestionText,
  askSessionQuestion,
  askQuestionFeedback,
  currentAnswer,
  voiceRecording,
  voiceUploading,
  toggleVoiceRecording,
  voiceFeedback,
  showVoiceConfirm,
  setShowVoiceConfirm,
  voiceTranscribedText,
  setVoiceTranscribedText,
  confirmVoiceQuestion,
  cancelVoiceReview,
  polishVoiceQuestion,
  voicePolishing,
  voicePolishMode,
  refetchSession,
  markMaterialsSeen,
  sessionLoadError,
  sessionIdFromUrl,
  onRetryLoadSession,
  replyingToQuestionId,
  setReplyingToQuestionId,
  currentAskerName,
  onCitationClick,
  onClearSession,
  stanceVersion = 0,
  sessionUpdatedVersion = 0,
  sessionInvitations = []
}) {
  const hasSession = currentSession && currentSession.session

  const primaryVideoAccessUrl = currentSession?.video_access_url || ''
  const hasPrimaryR2Video = currentSession?.session?.primary_video_artifact_id && primaryVideoAccessUrl
  // Resolve displayed video from session using ref (so selection survives refetches); fallback to primary
  const sources = currentSession?.video_sources ?? []
  const primary = currentSession?.primary_video ?? sources[0]
  const primarySourceId = currentSession?.primary_video?.id ?? sources[0]?.id
  const transcriptSourceForPrimary = primary ?? sources[0]
  const syntheticR2Video = hasPrimaryR2Video
    ? {
        id: currentSession?.session?.primary_video_artifact_id ?? 'primary',
        provider: 'r2',
        playback_mode: 'direct',
        media_url: primaryVideoAccessUrl,
        transcript_status: transcriptSourceForPrimary?.transcript_status ?? 'ready',
        transcript_text: transcriptSourceForPrimary?.transcript_text ?? null,
        transcript_segments: transcriptSourceForPrimary?.transcript_segments ?? null,
        source_type: 'upload'
      }
    : null
  const preferredId = selectedVideoIdRef?.current ?? selectedVideo?.id
  const resolvedFromSession = preferredId ? sources.find(vs => String(vs.id) === String(preferredId)) : null
  // When primary is selected and we have R2 primary, use syntheticR2Video so player uses video_access_url and correct transcript
  const useR2Primary = hasPrimaryR2Video && syntheticR2Video && (!resolvedFromSession || String(resolvedFromSession.id) === String(primarySourceId))
  const video = useR2Primary ? syntheticR2Video : (resolvedFromSession || primary)

  const [materialsCollapsed, setMaterialsCollapsedState] = useState(true) // collapsed on initial load and refresh
  // Track link count "last seen" per session so we can show "New" when creator adds links (for other users)
  const [lastSeenLinkCountBySession, setLastSeenLinkCountBySession] = useState({})
  const [membersPanelExpanded, setMembersPanelExpanded] = useState(false)

  // Decision stance state
  const [myStance, setMyStance] = useState(null)
  const [stanceAggregate, setStanceAggregate] = useState(null)
  const [stanceResponses, setStanceResponses] = useState([]) // per-person list with user_email
  const [stancePanelExpanded, setStancePanelExpanded] = useState(false) // Left-panel Decisions section: collapsed on load
  const [stanceRationale, setStanceRationale] = useState('')
  const [stanceSubmitting, setStanceSubmitting] = useState(false)
  const [stanceFeedback, setStanceFeedback] = useState({ type: '', message: '' })

  const fetchMyStance = useCallback(async () => {
    if (!currentSession?.session?.id || apiBaseUrl == null) return
    const base = (apiBaseUrl || '').replace(/\/$/, '')
    try {
      const res = await fetch(`${base}/api/sessions/${currentSession.session.id}/stances`, { credentials: 'include' })
      if (!res.ok) return
      const data = await res.json()
      setMyStance(data.my_stance ?? null)
      setStanceAggregate(data.aggregate ?? null)
      // Populate rationale from server so the text field shows what the user previously typed (participant view)
      if (data.my_stance?.rationale != null) {
        setStanceRationale(typeof data.my_stance.rationale === 'string' ? data.my_stance.rationale : '')
      }
      // API returns responses (lowercase); support both for robustness
      const list = Array.isArray(data.responses) ? data.responses : (Array.isArray(data.Responses) ? data.Responses : [])
      setStanceResponses(list)
    } catch { /* ignore */ }
  }, [currentSession?.session?.id, apiBaseUrl])

  // Refetch stances when stanceVersion changes (bumped by WebSocket stance_updated), so responses list and aggregate update in real time for all participants
  useEffect(() => {
    fetchMyStance()
  }, [fetchMyStance, stanceVersion])

  const submitStance = async (stanceValue) => {
    if (!currentSession?.session?.id || stanceSubmitting) return
    if (currentSession.session.decision_outcome) return
    setStanceSubmitting(true)
    setStanceFeedback({ type: '', message: '' })
    const base = (apiBaseUrl || '').replace(/\/$/, '')
    try {
      const body = { stance: stanceValue }
      const trimmedRationale = stanceRationale.trim().slice(0, 500)
      if (trimmedRationale) body.rationale = trimmedRationale
      const res = await fetch(`${base}/api/sessions/${currentSession.session.id}/stance`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body)
      })
      if (!res.ok) {
        let msg = 'Failed to submit stance'
        const text = await res.text()
        try {
          const errBody = JSON.parse(text)
          if (errBody?.error) msg = errBody.error
        } catch {
          if (text) msg = text
        }
        if (res.status === 401) {
          msg = 'Your session isn’t recognized. Please log out and log in again from this page (or open the session from the same site where you logged in).'
        }
        setStanceFeedback({ type: 'error', message: msg })
        return
      }
      const data = await res.json()
      setMyStance(data.my_stance ?? null)
      setStanceAggregate(data.aggregate ?? null)
      setStanceFeedback({ type: 'success', message: 'Position recorded' })
      fetchMyStance() // refetch to get updated responses list
    } catch (err) {
      setStanceFeedback({ type: 'error', message: err?.message || 'Failed to submit stance' })
    } finally {
      setStanceSubmitting(false)
    }
  }

  const clearStance = async () => {
    if (!currentSession?.session?.id || currentSession.session.decision_outcome) return
    setStanceSubmitting(true)
    setStanceFeedback({ type: '', message: '' })
    const base = (apiBaseUrl || '').replace(/\/$/, '')
    try {
      const res = await fetch(`${base}/api/sessions/${currentSession.session.id}/stance`, {
        method: 'DELETE',
        credentials: 'include'
      })
      if (!res.ok) {
        let msg = 'Failed to clear stance'
        const text = await res.text()
        try {
          const errBody = JSON.parse(text)
          if (errBody?.error) msg = errBody.error
        } catch {
          if (text) msg = text
        }
        setStanceFeedback({ type: 'error', message: msg })
        return
      }
      setStanceRationale('')
      setMyStance(null)
      const data = await res.json()
      setStanceAggregate(data.aggregate ?? null)
      setStanceFeedback({ type: 'success', message: 'Decision cleared' })
      fetchMyStance()
    } catch (err) {
      setStanceFeedback({ type: 'error', message: err?.message || 'Failed to clear stance' })
    } finally {
      setStanceSubmitting(false)
    }
  }

  // When user selects a video in the tree (Presentation or Additional Videos), mark the corresponding material as seen so the "New" badge clears.
  // Only depend on selection so we don't re-run when refetchSession updates currentSession (which would cause an infinite loop: mark seen -> refetch -> new materials ref -> effect -> mark seen -> ...).
  const lastMarkedMaterialIdsRef = useRef(null)
  useEffect(() => {
    if (!selectedVideo?.artifact_id || !markMaterialsSeen || !currentSession?.materials?.length) return
    const material = currentSession.materials.find(m => m?.artifact_id && String(m.artifact_id) === String(selectedVideo.artifact_id))
    if (!material?.id) return
    const ids = [material.id]
    if (lastMarkedMaterialIdsRef.current && ids.length === lastMarkedMaterialIdsRef.current.length && ids.every((id, i) => id === lastMarkedMaterialIdsRef.current[i])) return
    lastMarkedMaterialIdsRef.current = ids
    markMaterialsSeen(ids)
  }, [selectedVideo?.id, selectedVideo?.artifact_id, markMaterialsSeen])

  const [selectedDocument, setSelectedDocument] = useState(null)
  const [selectedDocumentId, setSelectedDocumentId] = useState(null)
  /** When opening a document from a citation, pass { page, block } so viewer can scroll to it */
  const [citationScrollTarget, setCitationScrollTarget] = useState(null)

  const [transcriptHighlightRange, setTranscriptHighlightRange] = useState(null)
  const transcriptHighlightTimerRef = useRef(null)
  /** Session transcript segments (from GET /sessions/:id/transcript) when video has no transcript_segments */
  const [sessionTranscriptSegments, setSessionTranscriptSegments] = useState(null)

  const setMaterialsCollapsed = (value) => {
    setMaterialsCollapsedState(value)
    if (hasSession?.session?.id) {
      try {
        localStorage.setItem(`${STORAGE_KEY_MATERIALS_COLLAPSED}.${currentSession.session.id}`, String(value))
      } catch {
        // ignore
      }
    }
  }

  useEffect(() => {
    if (!hasSession) return
    fetchSessionQuestions(currentSession.session.id)
  }, [hasSession])

  useEffect(() => {
    const sid = currentSession?.session?.id
    if (sid) {
      try {
        const stored = localStorage.getItem(`${STORAGE_KEY_MATERIALS_COLLAPSED}.${sid}`)
        if (stored !== null) setMaterialsCollapsedState(stored === 'true')
      } catch {
        // ignore
      }
    }
  }, [currentSession?.session?.id])

  // When primary video has transcript text but no segments, fetch session transcript (e.g. Zoom). Do not fetch for additional videos — they use their own transcript only.
  useEffect(() => {
    if (!useR2Primary) {
      setSessionTranscriptSegments(null)
      return
    }
    const sid = currentSession?.session?.id
    const hasVideoTranscript = video?.transcript_text
    const hasVideoSegments = Array.isArray(video?.transcript_segments) && video.transcript_segments.length > 0
    if (!sid || !hasVideoTranscript || hasVideoSegments || apiBaseUrl == null) {
      if (!hasVideoTranscript || hasVideoSegments) setSessionTranscriptSegments(null)
      return
    }
    const url = `${apiBaseUrl.replace(/\/$/, '')}/api/sessions/${sid}/transcript`
    let cancelled = false
    fetch(url)
      .then((res) => (res.ok ? res.json() : null))
      .then((data) => {
        if (cancelled || !data?.segments?.length) return
        setSessionTranscriptSegments(data.segments)
      })
      .catch(() => {})
    return () => { cancelled = true }
  }, [useR2Primary, currentSession?.session?.id, video?.transcript_text, video?.transcript_segments?.length, apiBaseUrl])

  const handleSelectDocument = (doc, scrollTarget = null) => {
    setSelectedDocument(doc)
    setSelectedDocumentId(doc?.id ?? doc?.transcriptId ?? null)
    setCitationScrollTarget(scrollTarget ?? null)
    if (doc?.id && markMaterialsSeen) {
      markMaterialsSeen([doc.id])
    }
  }

  const handleBackToVideo = () => {
    setSelectedDocument(null)
    setSelectedDocumentId(null)
  }

  const handleSelectLink = (link, fragment = null) => {
    if (!link?.url) return
    setSelectedDocument({
      type: 'link',
      url: link.url,
      title: link.title || link.url,
      id: link.id,
      ...(fragment != null && fragment !== '' && { fragment })
    })
    setSelectedDocumentId(`link-${link.id}`)
    setCitationScrollTarget(null)
  }

  const handleCitationClick = (citation) => {
    onCitationClick?.(citation)
    // Link citations: select the link in the left pane and show the page in the middle (with fragment if available)
    const fragment = citation?.navigation?.fragment ?? citation?.anchor?.section ?? ''
    // Resolve session links from either top-level or nested session (Render / different response shapes)
    const sessionLinks = Array.isArray(currentSession?.links) ? currentSession.links : (Array.isArray(currentSession?.session?.links) ? currentSession.session.links : null)
    const linkUrlFromCitation = citation?.navigation?.url || citation?.anchor?.url || (typeof citation?.label === 'string' && /^https?:\/\//i.test(citation.label.trim()) ? citation.label.trim() : null)
    if (citation?.navigation?.type === 'url' && citation.navigation?.url) {
      const link = citation?.source_id && sessionLinks
        ? sessionLinks.find(l => String(l?.id) === String(citation.source_id))
        : null
      if (link) {
        handleSelectLink(link, fragment)
        return
      }
      // No link in list: still show URL in middle pane and highlight would be N/A
      setSelectedDocument({
        type: 'link',
        url: citation.navigation.url,
        title: citation?.label || citation.navigation.url,
        id: citation.source_id || 'cite',
        ...(fragment && { fragment })
      })
      setSelectedDocumentId(citation.source_id ? `link-${citation.source_id}` : 'link-cite')
      setCitationScrollTarget(null)
      return
    }
    if (citation?.source_type === 'link' && citation?.source_id) {
      const link = sessionLinks ? sessionLinks.find(l => String(l?.id) === String(citation.source_id)) : null
      if (link?.url) {
        handleSelectLink(link, fragment)
        return
      }
      if (linkUrlFromCitation) {
        setSelectedDocument({
          type: 'link',
          url: linkUrlFromCitation,
          title: citation?.label || linkUrlFromCitation,
          id: citation.source_id,
          ...(fragment && { fragment })
        })
        setSelectedDocumentId(`link-${citation.source_id}`)
        setCitationScrollTarget(null)
        return
      }
    }
    const anchor = citation?.anchor
    const startMsVal = anchor?.start_ms ?? anchor?.startMs
    const endMsVal = anchor?.end_ms ?? anchor?.endMs
    const seekMs = citation?.navigation?.type === 'video' && (citation.navigation?.seek_ms != null)
      ? citation.navigation.seek_ms
      : startMsVal

    const sources = currentSession?.video_sources ?? []
    const labelLooksLikeVideo = citation?.label && typeof citation.label === 'string' && /\.(mp4|webm|mov|m4v)(\s|$|\()/i.test(citation.label)
    const labelMatchesVideo = labelLooksLikeVideo && sources.length > 0 && (() => {
      const labelBase = citation.label.split(/\s*\(\s*block\s+\d+\s*\)/i)[0]?.trim() || ''
      if (!labelBase) return false
      return sources.some(v => {
        const title = v?.stored_video_object_key ? String(v.stored_video_object_key).split('/').filter(Boolean).pop() : (v?.original_url ? (() => { try { return new URL(v.original_url).pathname.split('/').filter(Boolean).pop() } catch (_) { return '' } })() : '') || ''
        return title && (title === labelBase || labelBase.endsWith(title) || title.endsWith(labelBase))
      })
    })()

    const isTranscriptCitation = citation?.source_type === 'transcript' ||
      startMsVal != null ||
      (citation?.navigation?.type === 'video') ||
      labelMatchesVideo

    const applyTranscriptCitation = () => {
      if (seekMs != null && typeof setCurrentVideoTime === 'function') {
        setCurrentVideoTime(Number(seekMs) / 1000)
      }
      if (startMsVal != null) {
        if (transcriptHighlightTimerRef.current) clearTimeout(transcriptHighlightTimerRef.current)
        const startMs = Number(startMsVal)
        const endMs = endMsVal != null ? Number(endMsVal) : startMs + 5000
        setTranscriptHighlightRange({ startMs, endMs })
        transcriptHighlightTimerRef.current = setTimeout(() => {
          setTranscriptHighlightRange(null)
          transcriptHighlightTimerRef.current = null
        }, 5000)
      }
    }

    if (isTranscriptCitation) {
      // Select the video that this citation points to (primary or additional) so the correct video/transcript is shown
      const vidSourceId = citation?.source_id ?? citation?.sourceId ?? citation?.navigation?.source_id
      let vs = vidSourceId ? sources.find(v => String(v.id) === String(vidSourceId)) : null
      if (!vs && citation?.label && typeof citation.label === 'string' && sources.length > 0) {
        const labelBase = citation.label.split(/\s*\(\s*block\s+\d+\s*\)/i)[0]?.trim() || ''
        if (labelBase) {
          vs = sources.find(v => {
            const title = v?.stored_video_object_key
              ? String(v.stored_video_object_key).split('/').filter(Boolean).pop()
              : (v?.original_url ? (() => { try { return new URL(v.original_url).pathname.split('/').filter(Boolean).pop() } catch (_) { return '' } })() : '') || ''
            return title && (title === labelBase || labelBase.endsWith(title) || title.endsWith(labelBase))
          }) || null
        }
      }
      if (vs) {
        setSelectedVideo(vs)
        setVideoId(vs.id)
        setVideoPlayerKey(prev => prev + 1)
      }
      handleBackToVideo()
      setTimeout(applyTranscriptCitation, 0)
      return
    }

    // Open material/document when citation points to one: use source_id (from API or enriched from chunk)
    // Do not open a document when the citation label clearly refers to a video (e.g. "test.mp4 (block 1)") — we already handled that above when labelMatchesVideo
    if (labelLooksLikeVideo) return

    const materials = currentSession?.materials
    if (!Array.isArray(materials) || materials.length === 0) return

    const sourceId = citation?.source_id ?? citation?.sourceId ?? citation?.navigation?.source_id
    const isMaterialCitation = citation?.source_type === 'material' || citation?.navigation?.type === 'pdf' || citation?.navigation?.type === 'doc'
    const labelSuggestsDoc = citation?.label && /slide|document|p\.\s*\d|page\s*\d|\.(docx?|pdf)\s*(\(|$)/i.test(String(citation.label))

    let mat = sourceId ? materials.find(m => String(m?.id) === String(sourceId)) : null
    if (!mat && (isMaterialCitation || labelSuggestsDoc)) {
      const pdfs = materials.filter(m => (m?.content_type || m?.filename || '').toLowerCase().includes('pdf'))
      if (pdfs.length === 1) mat = pdfs[0]
      else if (materials.length === 1) mat = materials[0]
      // Fallback: match by filename from label (e.g. "Paresh Suthar Resume v5a.docx (block 4)" -> find material with that filename)
      else if (citation?.label && typeof citation.label === 'string') {
        const beforeBlock = citation.label.split(/\s*\(\s*block\s+\d+\s*\)/i)[0]?.trim()
        const beforePage = beforeBlock.split(/\s+p\.\s*\d+/i)[0]?.trim()
        const filenameFromLabel = beforePage || beforeBlock
        if (filenameFromLabel) {
          const byFilename = materials.find(m => {
            const f = (m?.filename || '').trim()
            return f && (f === filenameFromLabel || filenameFromLabel.endsWith(f) || f.endsWith(filenameFromLabel))
          })
          if (byFilename) mat = byFilename
        }
      }
    }
    if (mat) {
      setMaterialsCollapsed(false)
      const scrollTarget = citation?.navigation?.page != null || citation?.navigation?.block != null
        ? { page: citation.navigation?.page ?? undefined, block: citation.navigation?.block ?? undefined }
        : citation?.anchor?.page != null || citation?.anchor?.block != null
          ? { page: citation.anchor?.page ?? undefined, block: citation.anchor?.block ?? undefined }
          : null
      handleSelectDocument(mat, scrollTarget)
    }
  }

  // When session first loads, set "last seen" link count so existing links don't show as New; new links added later will show New
  const sessionIdForLinks = currentSession?.session?.id
  useEffect(() => {
    if (!sessionIdForLinks) return
    const linkCount = currentSession?.links?.length ?? 0
    setLastSeenLinkCountBySession((prev) => {
      if (prev[sessionIdForLinks] !== undefined) return prev
      return { ...prev, [sessionIdForLinks]: linkCount }
    })
  }, [sessionIdForLinks, currentSession?.links?.length])

  // Refetch session when tab becomes visible or on interval so new materials show without refresh
  const refetchRef = useRef(refetchSession)
  refetchRef.current = refetchSession
  useEffect(() => {
    if (!hasSession) return
    const refetch = () => refetchRef.current?.()
    const onVisibility = () => {
      if (document.visibilityState === 'visible') refetch()
    }
    document.addEventListener('visibilitychange', onVisibility)
    const interval = setInterval(() => {
      if (document.visibilityState === 'visible') refetch()
    }, 15000)
    return () => {
      document.removeEventListener('visibilitychange', onVisibility)
      clearInterval(interval)
    }
  }, [hasSession])

  if (!hasSession) {
    const isFailedFetch = (sessionLoadError || '').toLowerCase().includes('failed to fetch')
    return (
      <div className="section">
        <div className="error" style={{ marginBottom: '20px' }}>
          {sessionLoadError || 'Unable to load session. Please check the API connection and try again.'}
        </div>
        {isFailedFetch && (
          <p style={{ fontSize: '13px', color: '#666', marginBottom: '12px' }}>
            The app could not reach the API server at <strong>{apiBaseUrl || 'same origin (Vite proxy)'}</strong>. Check that the API is running on <code>http://localhost:8080</code>. If you're using the Vite dev server, you can leave <strong>API Base URL</strong> unset. For unusual setups (or shared links), you can add <code>?api=http://localhost:8080</code> to the link.
          </p>
        )}
        {sessionIdFromUrl && onRetryLoadSession && (
          <button
            type="button"
            onClick={() => onRetryLoadSession()}
            style={{
              padding: '8px 16px',
              fontSize: '14px',
              backgroundColor: '#1976d2',
              color: '#fff',
              border: 'none',
              borderRadius: '4px',
              cursor: 'pointer'
            }}
          >
            Retry loading session
          </button>
        )}
      </div>
    )
  }

  const gridClassName = materialsCollapsed
    ? 'participant-layout-grid materials-collapsed'
    : 'participant-layout-grid'

  return (
    <>
      <div className="participant-layout-topbar">
        <div style={{ display: 'flex', alignItems: 'center', gap: '12px', flexWrap: 'wrap', minWidth: 0 }}>
          <h2 style={{ margin: 0, fontSize: '18px', color: '#2e7d32' }}>
            {currentSession.session.title}
          </h2>
          <span style={{
            backgroundColor: currentSession.session.created_by === authUser?.email ? '#2e7d32' : '#757575',
            color: 'white',
            padding: '4px 12px',
            borderRadius: '4px',
            fontWeight: 'bold',
            fontSize: '14px'
          }}>
            {currentSession.session.created_by === authUser?.email ? 'Creator' : 'Participant'}
          </span>
        </div>
        {onClearSession && (
          <button
            type="button"
            onClick={onClearSession}
            style={{ backgroundColor: '#1976D2', color: 'white', padding: '8px 16px', border: 'none', borderRadius: '4px', cursor: 'pointer', fontWeight: 500 }}
          >
            Show All Sessions
          </button>
        )}
      </div>

      {(currentSession.session.premise || currentSession.session.primary_decision || currentSession.session.decision_outcome) && (
        <div style={{
          flexShrink: 0,
          display: 'flex',
          gap: '20px',
          flexWrap: 'wrap',
          padding: '4px 20px',
          backgroundColor: '#f1f8e9',
          borderBottom: '1px solid #c8e6c9',
          fontSize: '13px',
          color: '#333'
        }}>
          {currentSession.session.premise && (
            <span><strong>Premise:</strong> {currentSession.session.premise}</span>
          )}
          {currentSession.session.primary_decision && (
            <span><strong>Decision:</strong> {currentSession.session.primary_decision}</span>
          )}
          {currentSession.session.decision_outcome && (
            <span><strong>Outcome:</strong> {currentSession.session.decision_outcome}</span>
          )}
        </div>
      )}

      <div className={gridClassName}>
        <aside
          className={`participant-materials-panel ${materialsCollapsed ? 'materials-panel-collapsed' : 'materials-panel-expanded'}`}
          aria-expanded={!materialsCollapsed}
        >
          <MaterialsPanelHeader
            collapsed={materialsCollapsed}
            onCollapsedChange={setMaterialsCollapsed}
            unreadCount={Array.isArray(currentSession?.unread_material_ids) ? currentSession.unread_material_ids.length : 0}
          />
          {!materialsCollapsed && (
            <>
              {/* Decisions: same section as creator — Your decision + Members' decisions */}
              {currentSession?.session?.primary_decision && (
                <div style={{ flexShrink: 0, padding: '8px 12px', borderBottom: '1px solid #e0e0e0', backgroundColor: '#f1f8e9' }}>
                  <button type="button" onClick={() => setStancePanelExpanded(e => !e)} className="creator-collapsible-btn" aria-expanded={stancePanelExpanded} style={{ width: '100%', textAlign: 'left', background: 'none', border: 'none', cursor: 'pointer', padding: '4px 0', display: 'flex', alignItems: 'center', gap: '6px', fontSize: '13px', fontWeight: 'bold', color: '#1565c0' }}>
                    <span style={{ fontSize: '12px', color: '#555' }} aria-hidden>{stancePanelExpanded ? '▼' : '▷'}</span>
                    {' '}Decisions ({stanceAggregate?.total ?? stanceResponses?.length ?? 0})
                  </button>
                  {stancePanelExpanded && (
                    <div style={{ marginTop: '8px' }}>
                      <div style={{ fontSize: '12px', fontWeight: 600, color: '#555', marginBottom: '6px' }}>Your decision</div>
                      {currentSession.session.decision_outcome ? (
                        <p style={{ margin: 0, fontSize: '12px', color: '#888', fontStyle: 'italic' }}>Outcome recorded — stances are locked.</p>
                      ) : (
                        <>
                          <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', justifyContent: 'space-between', gap: '6px', marginBottom: '6px' }}>
                            <div style={{ display: 'flex', gap: '4px', flexWrap: 'wrap' }}>
                              {['agree', 'disagree', 'conditional', 'abstain', 'need_more_info'].map((s) => {
                                const label = s === 'need_more_info' ? 'Need More Info' : s.charAt(0).toUpperCase() + s.slice(1)
                                const bg = s === 'agree' ? '#e8f5e9' : s === 'disagree' ? '#ffebee' : s === 'conditional' ? '#fff3e0' : s === 'abstain' ? '#eceff1' : '#e3f2fd'
                                const border = s === 'agree' ? '#81c784' : s === 'disagree' ? '#e57373' : s === 'conditional' ? '#ffb74d' : s === 'abstain' ? '#90a4ae' : '#64b5f6'
                                const textColor = s === 'agree' ? '#2e7d32' : s === 'disagree' ? '#c62828' : s === 'conditional' ? '#e65100' : s === 'abstain' ? '#546e7a' : '#1565c0'
                                return (
                                  <button
                                    key={s}
                                    type="button"
                                    onClick={() => submitStance(s)}
                                    disabled={stanceSubmitting}
                                    style={{
                                      padding: '4px 10px',
                                      fontSize: '12px',
                                      borderRadius: '6px',
                                      border: myStance?.stance === s ? `2px solid ${border}` : `1px solid ${border}`,
                                      backgroundColor: bg,
                                      color: textColor,
                                      fontWeight: myStance?.stance === s ? 700 : 500,
                                      cursor: stanceSubmitting ? 'default' : 'pointer',
                                      margin: 0
                                    }}
                                  >
                                    {label}
                                  </button>
                                )
                              })}
                            </div>
                            {(myStance?.stance || stanceRationale?.trim()) && (
                              <button
                                type="button"
                                onClick={clearStance}
                                disabled={stanceSubmitting}
                                style={{
                                  marginLeft: 'auto',
                                  padding: '4px 10px',
                                  fontSize: '12px',
                                  borderRadius: '6px',
                                  border: '1px solid #9e9e9e',
                                  backgroundColor: '#fff',
                                  color: '#616161',
                                  fontWeight: 500,
                                  cursor: stanceSubmitting ? 'default' : 'pointer'
                                }}
                              >
                                Clear
                              </button>
                            )}
                          </div>
                          <input
                            type="text"
                            placeholder="Rationale (optional)"
                            value={stanceRationale}
                            onChange={(e) => setStanceRationale(e.target.value.slice(0, 500))}
                            onBlur={() => { if (myStance?.stance && !stanceSubmitting) submitStance(myStance.stance) }}
                            style={{ width: '100%', padding: '4px 8px', fontSize: '12px', border: '1px solid #e0e0e0', borderRadius: '4px', boxSizing: 'border-box', marginBottom: '4px' }}
                          />
                          {stanceFeedback.message && (
                            <p style={{ margin: 0, fontSize: '12px', color: stanceFeedback.type === 'error' ? '#c62828' : '#2e7d32' }}>{stanceFeedback.message}</p>
                          )}
                        </>
                      )}
                      <div style={{ fontSize: '12px', fontWeight: 600, color: '#555', marginTop: '10px', marginBottom: '4px' }}>Members&apos; decisions</div>
                      <div style={{ display: 'flex', gap: '4px', flexWrap: 'wrap', marginBottom: '8px' }}>
                        {[
                          ['Agree', stanceAggregate?.agree ?? 0, '#2e7d32', '#e8f5e9'],
                          ['Disagree', stanceAggregate?.disagree ?? 0, '#c62828', '#ffebee'],
                          ['Conditional', stanceAggregate?.conditional ?? 0, '#e65100', '#fff3e0'],
                          ['Abstain', stanceAggregate?.abstain ?? 0, '#546e7a', '#eceff1'],
                          ['Need More Info', stanceAggregate?.need_more_info ?? 0, '#1565C0', '#e3f2fd']
                        ].map(([label, count, color, bg]) => (
                          <span key={label} style={{ padding: '2px 8px', borderRadius: '10px', fontSize: '12px', fontWeight: count > 0 ? 700 : 400, color: count > 0 ? color : '#999', backgroundColor: count > 0 ? bg : '#f5f5f5', border: `1px solid ${count > 0 ? color : '#e0e0e0'}` }}>
                            {label}: {count}
                          </span>
                        ))}
                      </div>
                      {(stanceResponses?.length > 0) ? (
                        <div style={{ maxHeight: '160px', overflowY: 'auto', display: 'flex', flexDirection: 'column', gap: '4px' }}>
                          {stanceResponses.map((r, i) => (
                            <div key={r.id || `r-${i}`} style={{ padding: '4px 8px', backgroundColor: '#fafafa', border: '1px solid #e0e0e0', borderRadius: '3px', fontSize: '12px' }}>
                              <span style={{ fontWeight: 600 }}>{r.user_email || 'Unknown'}</span>
                              {' — '}
                              <span style={{ textTransform: 'capitalize' }}>{(r.stance || '').replace(/_/g, ' ')}</span>
                              {r.rationale && r.rationale.trim() && <span style={{ color: '#666', marginLeft: '4px' }}>&quot;{r.rationale.trim().length > 60 ? r.rationale.trim().slice(0, 60) + '…' : r.rationale.trim()}&quot;</span>}
                            </div>
                          ))}
                        </div>
                      ) : (
                        <p style={{ margin: 0, fontSize: '12px', color: '#888' }}>No responses yet.</p>
                      )}
                    </div>
                  )}
                </div>
              )}

              {/* Members: read-only list of invited members */}
              <div style={{ flexShrink: 0, padding: '8px 12px', borderBottom: '1px solid #e0e0e0', backgroundColor: '#f1f8e9' }}>
                <button
                  type="button"
                  onClick={() => setMembersPanelExpanded((e) => !e)}
                  aria-expanded={membersPanelExpanded}
                  style={{
                    width: '100%',
                    display: 'flex',
                    alignItems: 'center',
                    gap: '6px',
                    padding: '4px 0',
                    background: 'none',
                    border: 'none',
                    cursor: 'pointer',
                    textAlign: 'left',
                    fontSize: '13px',
                    fontWeight: 'bold',
                    color: '#2e7d32'
                  }}
                >
                  <span style={{ fontSize: '12px', color: '#555' }} aria-hidden>{membersPanelExpanded ? '▼' : '▷'}</span>
                  Members{Array.isArray(sessionInvitations) && sessionInvitations.length > 0 ? ` (${sessionInvitations.length})` : ''}
                </button>
                {membersPanelExpanded && (
                  <div style={{ marginTop: '8px', fontSize: '12px' }}>
                    {Array.isArray(sessionInvitations) && sessionInvitations.length > 0 ? (
                      <ul style={{ margin: 0, paddingLeft: '18px', color: '#333' }}>
                        {sessionInvitations.map((inv) => (
                          <li key={inv.id} style={{ marginBottom: '4px' }}>
                            <span style={{ fontWeight: 500 }}>{inv.invited_email}</span>
                            <span style={{ color: '#666', marginLeft: '4px' }}>({inv.invited_role || 'participant'}, {inv.status})</span>
                          </li>
                        ))}
                      </ul>
                    ) : (
                      <div style={{ color: '#666', fontStyle: 'italic' }}>No invited members listed.</div>
                    )}
                  </div>
                )}
              </div>

              <MaterialsTreePanel
                session={currentSession}
                apiBaseUrl={apiBaseUrl}
                selectedVideo={selectedVideo}
                setSelectedVideo={setSelectedVideo}
                setVideoId={setVideoId}
                setVideoPlayerKey={setVideoPlayerKey}
                onSelectDocument={handleSelectDocument}
                onSelectVideo={handleBackToVideo}
                onSelectLink={(link) => {
                  handleSelectLink(link)
                  const sid = currentSession?.session?.id
                  if (sid && currentSession?.links?.length != null) {
                    setLastSeenLinkCountBySession((prev) => ({ ...prev, [sid]: currentSession.links.length }))
                  }
                }}
                selectedDocumentId={selectedDocumentId}
                collapsed={materialsCollapsed}
                onCollapsedChange={setMaterialsCollapsed}
                hideTranscriptSection
                hideHeader
                lastSeenLinkCount={sessionIdForLinks ? (lastSeenLinkCountBySession[sessionIdForLinks] ?? 0) : 0}
              />
            </>
          )}
        </aside>

        <main className="participant-video-stage">
          <div style={{ padding: '12px', flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column', overflow: 'auto' }}>
            {/* Decision outcome only (Your decision lives in top "Your Position" bar) */}
            {currentSession?.session?.decision_outcome?.trim() && (
              <div style={{ flexShrink: 0, marginBottom: '16px', padding: '12px', backgroundColor: '#f1f8e9', border: '1px solid #c8e6c9', borderRadius: '8px' }}>
                <div style={{ fontWeight: '600', marginBottom: '2px', color: '#555' }}>Decision Outcome</div>
                <div style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word', fontSize: '13px', color: '#333' }}>{currentSession.session.decision_outcome.trim()}</div>
              </div>
            )}

            {selectedDocument ? (
              <DocumentViewer
                doc={selectedDocument}
                apiBaseUrl={apiBaseUrl}
                sessionId={currentSession?.session?.id || currentSession?.id}
                initialPage={citationScrollTarget?.page}
                initialBlock={citationScrollTarget?.block}
                slidesRefreshTrigger={sessionUpdatedVersion}
              />
            ) : (currentSession.video_sources && currentSession.video_sources.length > 0) || currentSession?.session?.primary_video_artifact_id ? (
              <>
                {currentSession?.session?.primary_video_artifact_id && !hasPrimaryR2Video && currentSession?.playback_reason_code && (
                  <div style={{
                    padding: '24px',
                    backgroundColor: currentSession.playback_reason_code === 'VIDEO_INGEST_PENDING' ? '#fff8e1' : '#f5f5f5',
                    borderRadius: '8px',
                    textAlign: 'center',
                    border: '1px solid #e0e0e0'
                  }}>
                    <p style={{ margin: 0, color: '#333', fontSize: '15px' }}>
                      {currentSession.playback_message || (currentSession.playback_reason_code === 'VIDEO_INGEST_PENDING' ? 'Video is still being prepared. Refresh in a moment.' : 'Video not available for this session.')}
                    </p>
                  </div>
                )}
                {video && !(currentSession?.session?.primary_video_artifact_id && !hasPrimaryR2Video && currentSession?.playback_reason_code) && (
                  <>
                    <div style={{ flexShrink: 0, display: 'flex', flexDirection: 'column', position: 'relative' }}>
                      <VideoPlayer
                        video={video}
                        onEvent={handleVideoPlayerEvent}
                        onTimeUpdate={handleVideoTimeUpdate}
                        currentTime={currentVideoTime}
                        playing={isVideoPlaying}
                        sessionId={currentSession?.session?.id}
                        apiBaseUrl={apiBaseUrl}
                        creatorIdentity={currentSession?.session?.created_by ?? creatorIdentity}
                        primaryVideoAccessUrl={primaryVideoAccessUrl}
                        primaryVideoArtifactId={currentSession?.session?.primary_video_artifact_id ?? null}
                      />
                      <VideoStartOverlay
                        sessionId={currentSession?.session?.id}
                        playing={isVideoPlaying}
                      />
                    </div>
                    <div style={{ flexShrink: 0, marginTop: '6px' }}>
                      {video.transcript_text ? (
                        <TranscriptViewer
                          transcriptText={video.transcript_text}
                          segments={
                            Array.isArray(video.transcript_segments) && video.transcript_segments.length > 0
                              ? video.transcript_segments
                              : (useR2Primary ? sessionTranscriptSegments : null) ?? undefined
                          }
                          highlightRangeMs={transcriptHighlightRange}
                        />
                      ) : (
                        <div style={{ padding: '12px', color: '#666', fontSize: '14px', fontStyle: 'italic' }}>
                          Transcript: {video.transcript_status === 'pending' || video.transcript_status === 'processing' ? 'Processing…' : 'No transcript yet.'}
                        </div>
                      )}
                    </div>
                  </>
                )}
              </>
            ) : (
              <div style={{ padding: '20px', color: '#666', fontStyle: 'italic' }}>
                No videos in this session yet.
              </div>
            )}
          </div>
        </main>

        <aside className="participant-qa-panel">
          <QAPanel
            questions={questions || []}
            unreadQuestionIds={unreadQuestionIds}
            markQuestionViewed={markQuestionViewed}
            sessionId={currentSession.session?.id}
            loading={loading}
            questionText={questionText}
            setQuestionText={setQuestionText}
            askSessionQuestion={askSessionQuestion}
            askQuestionFeedback={askQuestionFeedback}
            currentAnswer={currentAnswer}
            onCitationClick={handleCitationClick}
            replyingToQuestionId={replyingToQuestionId}
            setReplyingToQuestionId={setReplyingToQuestionId}
            currentAskerName={currentAskerName}
            creatorDisplayName={currentSession?.created_by_display_name}
            voiceRecording={voiceRecording}
            voiceUploading={voiceUploading}
            toggleVoiceRecording={toggleVoiceRecording}
            voiceFeedback={voiceFeedback}
            showVoiceConfirm={showVoiceConfirm}
            setShowVoiceConfirm={setShowVoiceConfirm}
            voiceTranscribedText={voiceTranscribedText}
            setVoiceTranscribedText={setVoiceTranscribedText}
            confirmVoiceQuestion={confirmVoiceQuestion}
            cancelVoiceReview={cancelVoiceReview}
            polishVoiceQuestion={polishVoiceQuestion}
            voicePolishing={voicePolishing}
            voicePolishMode={voicePolishMode}
          />
        </aside>
      </div>

    </>
  )
}

