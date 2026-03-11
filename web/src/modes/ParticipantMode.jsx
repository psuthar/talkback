import { useEffect, useState, useRef, useCallback } from 'react'
import { VideoPlayer } from '../VideoPlayer'
import { QAHistory } from '../components/QAHistory'
import { MaterialsTreePanel } from '../components/MaterialsTreePanel'
import { QAPanel } from '../components/QAPanel'
import { TranscriptViewer } from '../components/TranscriptViewer'
import mammoth from 'mammoth'
import { getDefaultApiBaseUrl } from '../config'
import { getMaterialTypeLabel } from '../utils/materialIcons'

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
  stanceVersion = 0
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

  const [materialsCollapsed, setMaterialsCollapsedState] = useState(false)

  // Decision stance state
  const [myStance, setMyStance] = useState(null)
  const [stanceAggregate, setStanceAggregate] = useState(null)
  const [stanceResponses, setStanceResponses] = useState([]) // per-person list with user_email
  const [stanceResponsesCollapsed, setStanceResponsesCollapsed] = useState(false) // default expanded so responses are visible
  const [stanceRationale, setStanceRationale] = useState('')
  const [stanceSubmitting, setStanceSubmitting] = useState(false)
  const [stanceFeedback, setStanceFeedback] = useState({ type: '', message: '' })

  const fetchMyStance = useCallback(async () => {
    if (!currentSession?.session?.id || !apiBaseUrl) return
    const base = (apiBaseUrl || '').replace(/\/$/, '')
    try {
      const res = await fetch(`${base}/api/sessions/${currentSession.session.id}/stances`, { credentials: 'include' })
      if (!res.ok) return
      const data = await res.json()
      setMyStance(data.my_stance ?? null)
      setStanceAggregate(data.aggregate ?? null)
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
        setMaterialsCollapsedState(stored === 'true')
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
    if (!sid || !hasVideoTranscript || hasVideoSegments || !apiBaseUrl) {
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

  const handleCitationClick = (citation) => {
    onCitationClick?.(citation)
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
            The app could not reach the API server at <strong>{apiBaseUrl || 'API Base URL'}</strong>. Check that the API is running at that URL (e.g. the debugger often uses port 8081). In app settings, set <strong>API Base URL</strong> to match your server. If you opened a shared link, you can add <code>?api={apiBaseUrl || getDefaultApiBaseUrl()}</code> to the link so the correct server is used.
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
        {onClearSession && (
          <button
            type="button"
            onClick={onClearSession}
            style={{ backgroundColor: '#f44336', color: 'white', padding: '8px 16px', border: 'none', borderRadius: '4px', cursor: 'pointer', fontWeight: 500 }}
          >
            Clear Session
          </button>
        )}
      </div>

      {(currentSession.session.premise || currentSession.session.primary_decision || currentSession.session.decision_outcome) && (
        <div style={{
          flexShrink: 0,
          display: 'flex',
          gap: '20px',
          flexWrap: 'wrap',
          padding: '6px 20px',
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

      {currentSession.session.primary_decision && (
        <div style={{ flexShrink: 0, padding: '10px 20px', backgroundColor: '#fff', borderBottom: '1px solid #e0e0e0' }}>
          <strong style={{ fontSize: '13px' }}>Your Position</strong>
          {currentSession.session.decision_outcome ? (
            <p style={{ margin: '4px 0 0', fontSize: '12px', color: '#888', fontStyle: 'italic' }}>
              Stances are locked — the outcome has been recorded.
            </p>
          ) : null}
          <div style={{ display: 'flex', gap: '8px', flexWrap: 'wrap', marginTop: '8px' }}>
            {['agree', 'disagree', 'conditional', 'abstain', 'need_more_info'].map((s) => (
              <button
                key={s}
                type="button"
                disabled={stanceSubmitting || !!currentSession.session.decision_outcome}
                onClick={() => submitStance(s)}
                style={{
                  padding: '5px 12px', fontSize: '12px', borderRadius: '4px',
                  border: myStance?.stance === s ? '2px solid #1976d2' : '1px solid #ccc',
                  backgroundColor: myStance?.stance === s ? '#e3f2fd' : '#fafafa',
                  color: '#1a1a1a',
                  fontWeight: myStance?.stance === s ? 700 : 400,
                  cursor: (stanceSubmitting || currentSession.session.decision_outcome) ? 'default' : 'pointer'
                }}
              >
                {s === 'need_more_info' ? 'Need More Info' : s.charAt(0).toUpperCase() + s.slice(1)}
              </button>
            ))}
          </div>
          {!currentSession.session.decision_outcome && (
            <>
              <textarea
                value={stanceRationale}
                onChange={(e) => setStanceRationale(e.target.value.slice(0, 500))}
                onBlur={() => {
                  if (myStance?.stance && !stanceSubmitting && !currentSession.session.decision_outcome) {
                    submitStance(myStance.stance)
                  }
                }}
                placeholder="Optional: briefly explain your position (max 500 chars)…"
                rows={2}
                style={{ width: '100%', marginTop: '8px', padding: '6px 8px', fontSize: '12px', resize: 'vertical', boxSizing: 'border-box' }}
              />
              <div style={{ fontSize: '11px', color: '#999', textAlign: 'right' }}>{stanceRationale.length}/500</div>
            </>
          )}
          {stanceFeedback.message && (
            <p style={{ margin: '4px 0 0', fontSize: '12px', color: stanceFeedback.type === 'error' ? '#c62828' : '#2e7d32' }}>
              {stanceFeedback.message}
            </p>
          )}
          {(stanceAggregate?.total > 0 || stanceResponses?.length > 0) && (
            <div style={{ marginTop: '10px', fontSize: '12px', color: '#555' }}>
              <button
                type="button"
                onClick={() => {
                  setStanceResponsesCollapsed((c) => {
                    const next = !c
                    if (next) fetchMyStance() // refetch when expanding so responses list is populated
                    return next
                  })
                }}
                style={{
                  background: 'none',
                  border: 'none',
                  padding: '2px 0',
                  cursor: 'pointer',
                  display: 'flex',
                  alignItems: 'center',
                  gap: '4px',
                  fontSize: '12px',
                  color: '#555',
                  fontWeight: 600
                }}
                aria-expanded={!stanceResponsesCollapsed}
              >
                <span style={{ transform: stanceResponsesCollapsed ? 'rotate(0deg)' : 'rotate(90deg)', display: 'inline-block', transition: 'transform 0.15s ease' }}>▶</span>
                All responses ({stanceAggregate?.total ?? stanceResponses.length})
              </button>
              {!stanceResponsesCollapsed && (
                <>
                  {stanceResponses.length > 0 ? (
                    <ul style={{ margin: '6px 0 0 16px', paddingLeft: '4px' }}>
                      {stanceResponses.map((r, i) => (
                        <li key={r.id || `${r.user_id}-${i}`} style={{ marginBottom: '4px' }}>
                          <strong>{r.user_email || 'Unknown'}</strong>
                          {' — '}
                          <span style={{ textTransform: 'capitalize' }}>{(r.stance || '').replace(/_/g, ' ')}</span>
                          {r.rationale && r.rationale.trim() && (
                            <span style={{ color: '#666', fontStyle: 'italic' }}> ({r.rationale.trim().slice(0, 80)}{r.rationale.trim().length > 80 ? '…' : ''})</span>
                          )}
                        </li>
                      ))}
                    </ul>
                  ) : (
                    <p style={{ margin: '6px 0 0 16px', fontSize: '12px', color: '#888' }}>
                      {stanceAggregate?.total > 0 ? `${stanceAggregate.total} response(s) recorded. Refreshing…` : 'No responses yet.'}
                    </p>
                  )}
                </>
              )}
            </div>
          )}
        </div>
      )}

      <div className={gridClassName}>
        <aside
          className={`participant-materials-panel ${materialsCollapsed ? 'materials-panel-collapsed' : 'materials-panel-expanded'}`}
          aria-expanded={!materialsCollapsed}
        >
          <MaterialsTreePanel
            session={currentSession}
            selectedVideo={selectedVideo}
            setSelectedVideo={setSelectedVideo}
            setVideoId={setVideoId}
            setVideoPlayerKey={setVideoPlayerKey}
            onSelectDocument={handleSelectDocument}
            onSelectVideo={handleBackToVideo}
            selectedDocumentId={selectedDocumentId}
            collapsed={materialsCollapsed}
            onCollapsedChange={setMaterialsCollapsed}
            hideTranscriptSection
          />
        </aside>

        <main className="participant-video-stage">
          <div style={{ padding: '12px', flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column', overflow: 'auto' }}>
            {selectedDocument ? (
              <ParticipantDocumentView
                doc={selectedDocument}
                apiBaseUrl={apiBaseUrl}
                initialPage={citationScrollTarget?.page}
                initialBlock={citationScrollTarget?.block}
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
                    <div style={{ flexShrink: 0, display: 'flex', flexDirection: 'column' }}>
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
            fetchSessionQuestions={fetchSessionQuestions}
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

// Match backend MaterialChunkSize / MaterialChunkOverlap for block highlighting
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

function ParticipantDocumentView({ doc, apiBaseUrl, initialPage, initialBlock }) {
  const contentRef = useRef(null)
  const blockRefs = useRef([])
  const [docxHtml, setDocxHtml] = useState(null)
  const [docxLoading, setDocxLoading] = useState(false)
  const [docxError, setDocxError] = useState(null)

  const isTranscript = doc?.type === 'transcript'
  const title = isTranscript ? (doc?.title || 'Transcript') : (doc?.filename || doc?.title || 'Document')
  const meta = isTranscript ? 'Transcript' : (getMaterialTypeLabel(doc) || doc?.content_type || '')
  const bodyText = isTranscript ? (doc?.text || '') : (doc?.extracted_text ?? '')
  const contentType = (doc?.content_type || '').toLowerCase()
  const fn = (doc?.filename || '').toLowerCase()
  const isPdf = !isTranscript && contentType.includes('pdf')
  const isDocx = !isTranscript && (
    contentType.includes('wordprocessingml') || contentType.includes('msword') ||
    fn.endsWith('.docx') || fn.endsWith('.doc')
  )
  const storageUrl = !isTranscript && doc?.storage_url
  const imageExts = ['.jpg', '.jpeg', '.png', '.gif', '.webp', '.bmp', '.svg']
  const isImage = !isTranscript && (
    contentType.startsWith('image/') ||
    imageExts.some(e => fn.endsWith(e))
  )
  const baseMaterialFileUrl = apiBaseUrl && doc?.artifact_id && doc?.id && !isTranscript
    ? `${apiBaseUrl.replace(/\/$/, '')}/artifacts/${doc.artifact_id}/materials/${doc.id}/file`
    : null
  const materialFileUrl = baseMaterialFileUrl && (initialPage != null && initialPage >= 1)
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
    fetch(baseMaterialFileUrl)
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

  // For text-based docs (Office, etc.): chunk and scroll/highlight block when opened from citation with block index
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

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', minHeight: 0 }}>
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
        {meta && (
          <span style={{ fontSize: '12px', color: '#666' }}>{meta}</span>
        )}
      </header>
      <div style={{
        flex: 1,
        minHeight: 0,
        overflow: 'auto',
        padding: '16px 0 0'
      }}>
        {isImage && materialFileUrl ? (
          <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'flex-start' }}>
            <img
              src={materialFileUrl}
              alt={title}
              style={{ maxWidth: '100%', height: 'auto', borderRadius: '8px', border: '1px solid #e0e0e0' }}
            />
          </div>
        ) : isPdf && materialFileUrl ? (
          <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
            <iframe
              src={materialFileUrl}
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
