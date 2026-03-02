import { useEffect, useState, useRef } from 'react'
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
  onCitationClick
}) {
  const hasSession = currentSession && currentSession.session

  const primaryVideoAccessUrl = currentSession?.video_access_url || ''
  const hasPrimaryR2Video = currentSession?.session?.primary_video_artifact_id && primaryVideoAccessUrl
  const firstVideoSource = currentSession?.video_sources?.[0]
  const syntheticR2Video = hasPrimaryR2Video
    ? {
        id: currentSession?.session?.primary_video_artifact_id ?? 'primary',
        provider: 'r2',
        playback_mode: 'direct',
        media_url: primaryVideoAccessUrl,
        transcript_status: firstVideoSource?.transcript_status ?? 'ready',
        transcript_text: firstVideoSource?.transcript_text ?? null,
        transcript_segments: firstVideoSource?.transcript_segments ?? null,
        source_type: 'upload'
      }
    : null
  // When we have a downloaded primary video, always use it (never Zoom stream)
  const video = hasPrimaryR2Video ? syntheticR2Video : (selectedVideo || (currentSession?.video_sources && currentSession.video_sources[0]))

  const [materialsCollapsed, setMaterialsCollapsedState] = useState(false)

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

  // When video has transcript text but no segments, fetch session transcript for detailed view (e.g. Zoom sessions)
  useEffect(() => {
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
  }, [currentSession?.session?.id, video?.transcript_text, video?.transcript_segments?.length, apiBaseUrl])

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

    const isTranscriptCitation = citation?.source_type === 'transcript' ||
      startMsVal != null ||
      (citation?.navigation?.type === 'video')

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
      if (selectedDocument) {
        // Switch to video view first so the transcript is visible, then seek + highlight
        handleBackToVideo()
        setTimeout(applyTranscriptCitation, 0)
      } else {
        applyTranscriptCitation()
      }
    }

    // Open material/document when citation points to one: use source_id (from API or enriched from chunk)
    const materials = currentSession?.materials
    if (!Array.isArray(materials) || materials.length === 0) return

    const sourceId = citation?.source_id ?? citation?.sourceId ?? citation?.navigation?.source_id
    const isMaterialCitation = citation?.source_type === 'material' || citation?.navigation?.type === 'pdf' || citation?.navigation?.type === 'doc'
    const labelSuggestsDoc = citation?.label && /slide|document|p\.\s*\d|page\s*\d/i.test(String(citation.label))

    let mat = sourceId ? materials.find(m => String(m?.id) === String(sourceId)) : null
    if (!mat && (isMaterialCitation || labelSuggestsDoc)) {
      const pdfs = materials.filter(m => (m?.content_type || m?.filename || '').toLowerCase().includes('pdf'))
      if (pdfs.length === 1) mat = pdfs[0]
      else if (materials.length === 1) mat = materials[0]
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
      </div>

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
                {!hasPrimaryR2Video && currentSession?.video_sources && currentSession.video_sources.length > 1 && (
                  <div style={{ marginBottom: '10px' }}>
                    <label style={{ fontWeight: 'bold', marginRight: '8px' }}>Session Video:</label>
                    <select
                      value={video?.id || currentSession.video_sources[0]?.id}
                      onChange={(e) => {
                        const v = currentSession.video_sources.find(vs => vs.id === e.target.value)
                        if (v) {
                          setSelectedVideo(v)
                          setVideoId(v.id)
                          setVideoPlayerKey(prev => prev + 1)
                        }
                      }}
                      style={{ padding: '4px 8px', fontSize: '14px' }}
                    >
                      {currentSession.video_sources.map((v, idx) => (
                        <option key={v.id} value={v.id}>
                          Video {idx + 1}
                        </option>
                      ))}
                    </select>
                  </div>
                )}
                {video && (
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
                    {video.transcript_text && (
                      <div style={{ flexShrink: 0, marginTop: '6px' }}>
                        <TranscriptViewer
                          transcriptText={video.transcript_text}
                          segments={
                            Array.isArray(video.transcript_segments) && video.transcript_segments.length > 0
                              ? video.transcript_segments
                              : sessionTranscriptSegments ?? undefined
                          }
                          highlightRangeMs={transcriptHighlightRange}
                        />
                      </div>
                    )}
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
