import { useState, useRef, useEffect, useCallback, useMemo } from 'react'
import { VideoPlayer, PlayerEvent } from '../VideoPlayer'
import { TranscriptViewer } from '../components/TranscriptViewer'
import { MaterialsTreePanel, MaterialsPanelHeader } from '../components/MaterialsTreePanel'
import { DocumentViewer } from '../components/DocumentViewer'
import { AddContentSection } from '../components/AddContentSection'
import { buildInviteMailto, buildInviteMessageBody, isValidEmailFormat } from '../utils/inviteMailto'

const PROCESSING_STEPS = ['Fetch', 'Download', 'Parse', 'Chunk', 'Embed', 'Ready', 'Preparing playback…']
const PROGRESSION_TICK_MS = 200 // Advance displayed step at most one per tick
const TARGET_STEP_TICK_MS = 500 // Advance target ref at most one per tick so steps don't jump from 0 to all-done

function processingStageToStepIndex(stage) {
  if (!stage) return 0
  const s = (stage || '').toLowerCase()
  if (s === 'fetch') return 0
  if (s === 'download') return 1
  if (s === 'parse') return 2
  if (s === 'chunk') return 3
  if (s === 'embed') return 4
  if (s === 'ready') return 5
  return 0
}

// Derive step from state so the UI advances even if stage lags; use max(stage, state) so we never show an earlier step
function processingStateToStepIndex(state) {
  if (!state) return 0
  const s = (state || '').toLowerCase()
  if (s === 'queued' || s === 'fetching') return 0
  if (s === 'downloading') return 1
  if (s === 'parsing') return 2
  if (s === 'chunking') return 3
  if (s === 'embedding') return 4
  if (s === 'ready') return 5
  return 0
}

function relativeTime(isoString) {
  if (!isoString) return ''
  const d = new Date(isoString)
  const now = new Date()
  const sec = Math.floor((now - d) / 1000)
  if (sec < 60) return 'just now'
  const min = Math.floor(sec / 60)
  if (min < 60) return `${min} min ago`
  const hr = Math.floor(min / 60)
  if (hr < 24) return `${hr} hr ago`
  const day = Math.floor(hr / 24)
  return `${day} day${day !== 1 ? 's' : ''} ago`
}

function InvitationActionButton({ apiBaseUrl, invitationId, action, onDone }) {
  const [loading, setLoading] = useState(false)
  const base = (apiBaseUrl || '').replace(/\/$/, '')
  const url = `${base}/api/invitations/${invitationId}/${action}`
  const label = action === 'resend' ? 'Resend' : 'Revoke'
  const handleClick = async () => {
    setLoading(true)
    try {
      const res = await fetch(url, { method: 'POST', credentials: 'include' })
      if (res.ok && typeof onDone === 'function') {
        if (action === 'resend') {
          const data = await res.json().catch(() => ({}))
          onDone(data)
        } else {
          onDone()
        }
      }
    } finally {
      setLoading(false)
    }
  }
  return (
    <button type="button" onClick={handleClick} disabled={loading} style={{ marginRight: '8px', padding: '2px 8px', fontSize: '11px' }}>
      {loading ? '…' : label}
    </button>
  )
}

function CopyInvitationLinkButton({ apiBaseUrl, invitationId, onCopied, onError }) {
  const [loading, setLoading] = useState(false)
  const base = (apiBaseUrl || '').replace(/\/$/, '')
  const handleClick = async () => {
    setLoading(true)
    try {
      const res = await fetch(`${base}/api/invitations/${invitationId}/link`, { method: 'GET', credentials: 'include' })
      const data = await res.json().catch(() => ({}))
      if (res.ok && data.accept_url) {
        await navigator.clipboard.writeText(data.accept_url)
        if (typeof onCopied === 'function') onCopied()
      } else {
        if (typeof onError === 'function') onError(data.error || 'Failed to get link')
      }
    } catch (_) {
      if (typeof onError === 'function') onError('Failed to get link')
    } finally {
      setLoading(false)
    }
  }
  return (
    <button type="button" onClick={handleClick} disabled={loading} style={{ marginLeft: '6px', padding: '2px 8px', fontSize: '11px' }}>
      {loading ? '…' : 'Copy link'}
    </button>
  )
}

function OpenEmailDraftButton({ apiBaseUrl, invitationId, invitation, sessionTitle, inviterEmail, inviterDisplayName, onError }) {
  const [loading, setLoading] = useState(false)
  const base = (apiBaseUrl || '').replace(/\/$/, '')
  const handleClick = async () => {
    setLoading(true)
    try {
      const res = await fetch(`${base}/api/invitations/${invitationId}/link`, { method: 'GET', credentials: 'include' })
      const data = await res.json().catch(() => ({}))
      if (res.ok && data.accept_url) {
        const draft = {
          invited_email: invitation?.invited_email,
          accept_url: data.accept_url,
          session_title: sessionTitle || 'a session',
          inviter_email: inviterEmail,
          inviter_name: inviterDisplayName,
          expires_at: invitation?.expires_at
        }
        const mailtoUrl = buildInviteMailto(draft)
        if (mailtoUrl) {
          try { window.location.href = mailtoUrl } catch (_) { /* mailto may be blocked */ }
        }
      } else {
        if (typeof onError === 'function') onError(data.error || 'Failed to get link')
      }
    } catch (_) {
      if (typeof onError === 'function') onError('Failed to get link')
    } finally {
      setLoading(false)
    }
  }
  return (
    <button type="button" onClick={handleClick} disabled={loading} style={{ marginLeft: '6px', padding: '2px 8px', fontSize: '11px' }}>
      {loading ? '…' : 'Open email draft'}
    </button>
  )
}

export function CreatorMode({
  currentSession,
  sessionProcessingReadyVersion = 0,
  stanceVersion = 0,
  refetchSession,
  artifactId,
  setArtifactId,
  videoId,
  setVideoId,
  selectedVideo,
  selectedVideoIdRef,
  setSelectedVideo,
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
  regenerateTranscript,
  questions,
  unreadQuestionIds = [],
  markQuestionViewed,
  fetchSessionQuestions,
  loading,
  apiBaseUrl,
  creatorIdentity,
  authUser,
  inviteEmail,
  setInviteEmail,
  inviteRole,
  setInviteRole,
  inviteFeedback,
  setInviteFeedback,
  inviteLoading,
  inviteUserToSession,
  sessionInvitations = [],
  fetchSessionInvitations,
  lastInvitationDraft,
  setLastInvitationDraft,
  setPrimaryVideoSource,
  onClearSession,
  debugMode = false
}) {
  const [materialUploading, setMaterialUploading] = useState(false)
  const [materialUploadFeedback, setMaterialUploadFeedback] = useState({ type: '', message: '' })
  const materialFileInputRef = useRef(null)
  const materialUploadFeedbackTimeoutRef = useRef(null)
  useEffect(() => () => {
    if (materialUploadFeedbackTimeoutRef.current) clearTimeout(materialUploadFeedbackTimeoutRef.current)
  }, [])
  const [answeringQuestionId, setAnsweringQuestionId] = useState(null)
  const [answerText, setAnswerText] = useState('')
  const [answerStatus, setAnswerStatus] = useState('answered')
  const [answerFeedback, setAnswerFeedback] = useState({ type: '', message: '' })
  const [answerVoiceRecording, setAnswerVoiceRecording] = useState(false)
  const [answerVoiceUploading, setAnswerVoiceUploading] = useState(false)
  const [answerVoiceFeedback, setAnswerVoiceFeedback] = useState({ type: '', message: '' })
  const [answerVoiceTranscribedText, setAnswerVoiceTranscribedText] = useState('')
  const [showAnswerVoiceConfirm, setShowAnswerVoiceConfirm] = useState(false)
  const [answerMediaRecorder, setAnswerMediaRecorder] = useState(null)
  const [answerMediaStream, setAnswerMediaStream] = useState(null)
  const answerVoiceChunksRef = useRef([])
  const [mockQuestionLoading, setMockQuestionLoading] = useState(false)
  const [confirmingAnswerId, setConfirmingAnswerId] = useState(null)
  const [answerSubmitting, setAnswerSubmitting] = useState(false)
  const [questionCardsExpanded, setQuestionCardsExpanded] = useState({}) // per-question collapse; default all collapsed
  // When answering a question, expand that card so the answer form is visible
  useEffect(() => {
    if (answeringQuestionId) {
      setQuestionCardsExpanded((prev) => ({ ...prev, [answeringQuestionId]: true }))
    }
  }, [answeringQuestionId])

  // Thread tree for participant questions (roots + nested replies), same structure as participant view (QAHistory ThreadList)
  const { roots: creatorRoots, byParent: creatorByParent } = useMemo(() => {
    const qs = questions || []
    if (qs.length === 0) return { roots: [], byParent: {} }
    const byParent = {}
    for (const q of qs) {
      const pid = q.parent_question_id ?? 'root'
      if (!byParent[pid]) byParent[pid] = []
      byParent[pid].push(q)
    }
    const roots = (byParent.root || []).sort((a, b) => new Date(b.created_at) - new Date(a.created_at))
    for (const k of Object.keys(byParent)) {
      if (k !== 'root') byParent[k].sort((a, b) => new Date(a.created_at) - new Date(b.created_at))
    }
    return { roots, byParent }
  }, [questions])

  const [membersPanelExpanded, setMembersPanelExpanded] = useState(true) // expanded by default so prominent at top
  const [contextPanelExpanded, setContextPanelExpanded] = useState(true)
  const [contextPremise, setContextPremise] = useState('')
  const [contextDecision, setContextDecision] = useState('')
  const [contextOutcome, setContextOutcome] = useState('')
  const [contextSaving, setContextSaving] = useState(false)
  const [contextFeedback, setContextFeedback] = useState({ type: '', message: '' })
  const [stanceData, setStanceData] = useState(null)
  const [stancePanelExpanded, setStancePanelExpanded] = useState(true)
  const [stanceRationale, setStanceRationale] = useState('')
  const [stanceSubmitting, setStanceSubmitting] = useState(false)
  const [stanceFeedback, setStanceFeedback] = useState({ type: '', message: '' })
  const [leftPanelCollapsed, setLeftPanelCollapsed] = useState(false)
  const [rightPanelCollapsed, setRightPanelCollapsed] = useState(false)
  const [selectedDocument, setSelectedDocument] = useState(null)
  const [selectedDocumentId, setSelectedDocumentId] = useState(null)

  const handleSelectDocument = (doc) => {
    setSelectedDocument(doc)
    setSelectedDocumentId(doc?.id ?? doc?.transcriptId ?? (doc?.type === 'link' && doc?.id ? `link-${doc.id}` : null))
  }
  const handleBackToVideo = () => {
    setSelectedDocument(null)
    setSelectedDocumentId(null)
  }
  const handleSelectVideo = (v) => {
    setSelectedDocument(null)
    setSelectedDocumentId(null)
    setSelectedVideo(v)
    setVideoId(v?.id)
    setVideoPlayerKey((prev) => prev + 1)
  }
  const handleSelectLink = (link) => {
    if (!link?.url) return
    setSelectedDocument({
      type: 'link',
      url: link.url,
      title: link.title || link.url,
      id: link.id
    })
    setSelectedDocumentId(`link-${link.id}`)
  }

  const startAnswering = (questionId, existingAnswer = null) => {
    setAnsweringQuestionId(questionId)
    if (existingAnswer && (existingAnswer.answer_text || existingAnswer.answer_text === '')) {
      setAnswerText(existingAnswer.answer_text || '')
      setAnswerStatus(existingAnswer.answer_status || 'answered')
    } else {
      setAnswerText('')
      setAnswerStatus('answered')
    }
    setAnswerFeedback({ type: '', message: '' })
  }

  const cancelAnswering = () => {
    setAnsweringQuestionId(null)
    setAnswerText('')
    setAnswerVoiceTranscribedText('')
    setShowAnswerVoiceConfirm(false)
    cleanupAnswerVoiceMedia()
  }

  const createMockQuestion = async () => {
    if (!currentSession || !currentSession.session || !currentSession.session.id) {
      return
    }

    setMockQuestionLoading(true)
    try {
      const response = await fetch(`${apiBaseUrl}/sessions/${currentSession.session.id}/questions/mock`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' }
      })

      if (!response.ok) {
        const text = await response.text()
        console.error('Failed to create mock question:', text)
        return
      }

      const data = await response.json()
      console.log('Mock question created (not persisted, will disappear on refresh):', data)
      
      // Note: We don't refresh questions from the database because mock questions
      // are not persisted. The WebSocket message will trigger a refresh in all
      // connected windows, but the question won't appear in the database query.
      // The WebSocket message itself contains the question data, so it will appear
      // in the UI until the window is refreshed.
    } catch (err) {
      console.error('Error creating mock question:', err)
    } finally {
      setMockQuestionLoading(false)
    }
  }

  const cleanupAnswerVoiceMedia = () => {
    try {
      if (answerMediaRecorder && answerMediaRecorder.state !== 'inactive') {
        answerMediaRecorder.stop()
      }
    } catch {
      // ignore
    }
    if (answerMediaStream) {
      answerMediaStream.getTracks().forEach(t => t.stop())
    }
    setAnswerMediaRecorder(null)
    setAnswerMediaStream(null)
    setAnswerVoiceRecording(false)
    answerVoiceChunksRef.current = []
  }

  const toggleAnswerVoiceRecording = async () => {
    if (!answeringQuestionId) return

    setAnswerVoiceFeedback({ type: '', message: '' })

    // Stop recording
    if (answerVoiceRecording) {
      try {
        setAnswerVoiceUploading(true)
        if (answerMediaRecorder && answerMediaRecorder.state !== 'inactive') {
          try { answerMediaRecorder.requestData() } catch { /* ignore */ }
          answerMediaRecorder.stop()
        }
      } catch (err) {
        setAnswerVoiceFeedback({ type: 'error', message: `Failed to stop recording: ${err.message}` })
        cleanupAnswerVoiceMedia()
        setAnswerVoiceUploading(false)
      }
      return
    }

    // Start recording
    try {
      if (!navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) {
        setAnswerVoiceFeedback({ type: 'error', message: 'Microphone is not supported in this browser.' })
        return
      }

      const stream = await navigator.mediaDevices.getUserMedia({ audio: true })
      setAnswerMediaStream(stream)

      if (!window.MediaRecorder) {
        setAnswerVoiceFeedback({ type: 'error', message: 'MediaRecorder is not supported in this browser.' })
        stream.getTracks().forEach(t => t.stop())
        setAnswerMediaStream(null)
        return
      }

      const preferredTypes = [
        'audio/webm;codecs=opus',
        'audio/ogg;codecs=opus',
        'audio/webm',
        'audio/ogg'
      ]
      const chosenType = preferredTypes.find(t => window.MediaRecorder.isTypeSupported && window.MediaRecorder.isTypeSupported(t))
      const recorder = chosenType ? new MediaRecorder(stream, { mimeType: chosenType }) : new MediaRecorder(stream)
      setAnswerMediaRecorder(recorder)
      answerVoiceChunksRef.current = []
      setAnswerVoiceRecording(true)

      recorder.ondataavailable = (e) => {
        if (e.data && e.data.size > 0) {
          answerVoiceChunksRef.current.push(e.data)
        }
      }

      recorder.onstop = async () => {
        setAnswerVoiceRecording(false)
        const chunks = answerVoiceChunksRef.current || []
        const mime = recorder.mimeType
        answerVoiceChunksRef.current = []
        await transcribeAnswerVoiceChunks(chunks, mime)
        stream.getTracks().forEach(t => t.stop())
        setAnswerMediaStream(null)
        setAnswerMediaRecorder(null)
      }

      recorder.start()
    } catch (err) {
      const msg = err && err.name === 'NotAllowedError'
        ? 'Microphone permission denied. Please allow microphone access and try again.'
        : `Failed to start microphone: ${err.message}`
      setAnswerVoiceFeedback({ type: 'error', message: msg })
      cleanupAnswerVoiceMedia()
    }
  }

  const transcribeAnswerVoiceChunks = async (chunks, mimeType) => {
    try {
      if (!chunks || chunks.length === 0) {
        setAnswerVoiceFeedback({ type: 'error', message: 'No audio captured. Please try again.' })
        return
      }

      setAnswerVoiceUploading(true)
      setAnswerVoiceFeedback({ type: '', message: '' })

      const blobType = mimeType || (chunks[0] && chunks[0].type) || 'audio/webm'
      const audioBlob = new Blob(chunks, { type: blobType })

      const form = new FormData()
      form.append('file', audioBlob, 'voice-answer.webm')

      const response = await fetch(`${apiBaseUrl}/sessions/${currentSession.session.id}/questions/${answeringQuestionId}/answers/voice`, {
        method: 'POST',
        credentials: 'include',
        body: form
      })

      if (!response.ok) {
        const text = await response.text()
        setAnswerVoiceFeedback({ type: 'error', message: `Transcription failed (${response.status}): ${text}` })
        return
      }

      const data = await response.json()
      const text = (data && data.transcribed_text) ? data.transcribed_text : ''
      if (!text.trim()) {
        setAnswerVoiceFeedback({ type: 'error', message: 'Transcription was empty. Please try again or type your answer.' })
        return
      }

      setAnswerVoiceTranscribedText(text)
      setShowAnswerVoiceConfirm(true)
      setAnswerVoiceFeedback({ type: 'success', message: 'Transcription ready. Review and submit.' })
    } catch (err) {
      setAnswerVoiceFeedback({ type: 'error', message: `Transcription failed: ${err.message}` })
    } finally {
      setAnswerVoiceUploading(false)
      answerVoiceChunksRef.current = []
    }
  }

  const submitAnswer = async (text) => {
    if (!answeringQuestionId || !text.trim()) {
      setAnswerFeedback({ type: 'error', message: 'Please enter an answer before submitting.' })
      return
    }

    setAnswerFeedback({ type: '', message: '' })
    setAnswerSubmitting(true)

    try {
      const response = await fetch(`${apiBaseUrl}/sessions/${currentSession.session.id}/questions/${answeringQuestionId}/answers`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          answer_text: text.trim(),
          status: answerStatus
        })
      })

      if (!response.ok) {
        const text = await response.text()
        try {
          const json = JSON.parse(text)
          setAnswerFeedback({ type: 'error', message: `Error ${response.status}: ${JSON.stringify(json, null, 2)}` })
        } catch {
          setAnswerFeedback({ type: 'error', message: `Error ${response.status}: ${text}` })
        }
        return
      }

      setAnswerFeedback({ type: 'success', message: 'Answer submitted successfully!' })
      setAnsweringQuestionId(null)
      setAnswerText('')
      setAnswerVoiceTranscribedText('')
      setShowAnswerVoiceConfirm(false)
      // Immediately refresh questions to show the updated answer
      await fetchSessionQuestions(currentSession.session.id)
    } catch (err) {
      setAnswerFeedback({ type: 'error', message: `Failed to submit answer: ${err.message}` })
    } finally {
      setAnswerSubmitting(false)
    }
  }

  const confirmAnswerVoice = async () => {
    if (!answerVoiceTranscribedText.trim()) {
      setAnswerVoiceFeedback({ type: 'error', message: 'Please enter an answer before submitting.' })
      return
    }
    setShowAnswerVoiceConfirm(false)
    await submitAnswer(answerVoiceTranscribedText.trim())
    setAnswerVoiceTranscribedText('')
  }

  // When session has primary_video_artifact_id, playback is ONLY the downloaded MP4 (R2 or local). Never use Zoom stream (410).
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
  // Resolve displayed video from session using ref (so selection survives refetches); fallback to primary
  const sources = currentSession?.video_sources ?? []
  const primary = currentSession?.primary_video ?? sources[0]
  const primarySourceId = currentSession?.primary_video?.id ?? sources[0]?.id
  const preferredId = selectedVideoIdRef?.current ?? selectedVideo?.id
  const resolvedFromSession = preferredId ? sources.find(vs => String(vs.id) === String(preferredId)) : null
  // When primary is selected and we have R2 primary, use syntheticR2Video so VideoPlayer gets primaryVideoAccessUrl (artifact id); otherwise it would get the raw VideoSource and miss the stream URL
  const useR2Primary = hasPrimaryR2Video && syntheticR2Video && (!resolvedFromSession || String(resolvedFromSession.id) === String(primarySourceId))
  const video = useR2Primary ? syntheticR2Video : (resolvedFromSession || primary)

  // Fetch questions on mount/change (WebSocket handles real-time updates)
  useEffect(() => {
    if (!currentSession || !currentSession.session || !currentSession.session.id) return

    const sessionId = currentSession.session.id
    // Fetch immediately on mount/change
    fetchSessionQuestions(sessionId)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currentSession?.session?.id])

  // Determine if we have a valid session - only show participant link when session exists
  const hasValidSession = currentSession && (
    (currentSession.session && currentSession.session.id) || 
    currentSession.id
  )
  const sessionId = hasValidSession 
    ? (currentSession?.session?.id || currentSession?.id)
    : null
  const participantUrl = sessionId && apiBaseUrl
    ? `${window.location.origin}${window.location.pathname}?session=${sessionId}&mode=view&api=${encodeURIComponent(apiBaseUrl)}`
    : null

  // Processing status (Mission #4: GET /api/sessions/:id/processing) — preferred over legacy ingestion
  const [processingStatus, setProcessingStatus] = useState(null) // { state, stage, attempt_count, next_retry_at, last_error_code, last_error_message, updated_at }
  const [processingRetrying, setProcessingRetrying] = useState(false)
  const processingIntervalRef = useRef(null)
  // Once we've seen a processing job for this session, keep the progression panel visible until video is on screen (so it never "disappears" mid-import)
  const hasSeenProcessingJobRef = useRef(false)

  // When WebSocket sends session_processing_ready, App bumps sessionProcessingReadyVersion and refetches. Set status to "ready" immediately so the panel shows "Preparing playback" until refetch returns (no disappear).
  const prevProcessingReadyVersionRef = useRef(sessionProcessingReadyVersion)
  useEffect(() => {
    if (sessionProcessingReadyVersion > prevProcessingReadyVersionRef.current && sessionId) {
      prevProcessingReadyVersionRef.current = sessionProcessingReadyVersion
      hasSeenProcessingJobRef.current = true
      setProcessingStatus((prev) => ({
        ...(prev || {}),
        state: 'ready',
        stage: 'ready',
        updated_at: new Date().toISOString()
      }))
      console.log('[ProgressPanel] WebSocket processing ready: set status to ready, panel will show "Preparing playback" until refetch')
    }
  }, [sessionProcessingReadyVersion, sessionId])
  useEffect(() => {
    if (!sessionId || !apiBaseUrl) return
    if (processingIntervalRef.current) {
      clearInterval(processingIntervalRef.current)
      processingIntervalRef.current = null
    }
    const fetchProcessing = () => {
      fetch(`${apiBaseUrl}/api/sessions/${sessionId}/processing`, { headers: { 'X-Creator-Identity': creatorIdentity } })
        .then((r) => r.json())
        .then((data) => {
          // Only update when we have a state; never clear on empty so the panel doesn't disappear on transient empty/error responses
          if (data.state != null && data.state !== '') {
            setProcessingStatus(data)
            hasSeenProcessingJobRef.current = true
            console.log('[ProgressPanel] processing fetch: state=', data.state, 'stage=', data.stage)
          } else {
            console.log('[ProgressPanel] processing fetch: empty state, keeping previous (data.state=', data.state, ')')
          }
        })
        .catch((err) => {
          console.log('[ProgressPanel] processing fetch failed, keeping previous:', err?.message || err)
        })
    }
    fetchProcessing()
    const terminal = processingStatus?.state && ['ready', 'failed_permanent', 'canceled'].includes(processingStatus.state)
    if (terminal) return
    const intervalMs = processingStatus?.state === 'waiting' ? 15000 : 2000
    processingIntervalRef.current = setInterval(fetchProcessing, intervalMs)
    return () => {
      if (processingIntervalRef.current) {
        clearInterval(processingIntervalRef.current)
        processingIntervalRef.current = null
      }
    }
  }, [sessionId, apiBaseUrl, creatorIdentity, processingStatus?.state])

  const prevSessionIdForResetRef = useRef(null)
  useEffect(() => {
    if (prevSessionIdForResetRef.current !== sessionId) {
      prevSessionIdForResetRef.current = sessionId
      hasSeenProcessingJobRef.current = false
    }
  }, [sessionId])

  // --- Progression panel: timer-driven display step (advance at most one step per PROGRESSION_TICK_MS) ---
  const [displayStepIndex, setDisplayStepIndex] = useState(0)
  const lastKnownTargetStepRef = useRef(0)
  const backendTargetStepRef = useRef(0)

  // Compute target step from backend (or WebSocket optimistic ready)
  const hasJobForStep = processingStatus?.state != null && processingStatus.state !== ''
  const readyButNoVideoYetForStep = hasJobForStep && processingStatus?.state === 'ready' && !hasPrimaryR2Video
  const effectiveStageForStep = hasJobForStep
    ? (processingStatus.stage || (!['ready', 'failed_permanent', 'canceled'].includes(processingStatus.state) ? 'fetch' : null))
    : 'fetch'
  const stageIndexForStep = processingStageToStepIndex(effectiveStageForStep)
  const stateIndexForStep = processingStateToStepIndex(hasJobForStep ? processingStatus?.state : null)
  const targetStepIndex = readyButNoVideoYetForStep
    ? PROCESSING_STEPS.length - 1
    : Math.max(stageIndexForStep, stateIndexForStep)
  if (hasJobForStep || readyButNoVideoYetForStep) {
    backendTargetStepRef.current = targetStepIndex
  }

  useEffect(() => {
    setDisplayStepIndex(0)
    lastKnownTargetStepRef.current = 0
    backendTargetStepRef.current = 0
  }, [sessionId])

  // Advance target ref at most one step per TARGET_STEP_TICK_MS toward backend so steps never jump 0 -> all-done
  useEffect(() => {
    if (!sessionId) return
    const id = setInterval(() => {
      const backendTarget = backendTargetStepRef.current
      lastKnownTargetStepRef.current = Math.min(lastKnownTargetStepRef.current + 1, backendTarget)
    }, TARGET_STEP_TICK_MS)
    return () => clearInterval(id)
  }, [sessionId])

  // Advance displayed step at most one per PROGRESSION_TICK_MS toward the ref
  useEffect(() => {
    if (!sessionId) return
    const id = setInterval(() => {
      setDisplayStepIndex((prev) => {
        const target = lastKnownTargetStepRef.current
        if (prev < target) return prev + 1
        if (prev > target) return target
        return prev
      })
    }, PROGRESSION_TICK_MS)
    return () => clearInterval(id)
  }, [sessionId])

  // Progress panel: only visible while a video is being processed; hide once the video player is visible (after 2.5s grace)
  const PROGRESS_PANEL_DELAY_MS = 2500
  const [hideProgressPanelAfter, setHideProgressPanelAfter] = useState(null)
  const [primaryVideoMounted, setPrimaryVideoMounted] = useState(false)
  const handlePrimaryVideoMounted = useCallback(() => {
    setPrimaryVideoMounted(true)
    setHideProgressPanelAfter(Date.now() + PROGRESS_PANEL_DELAY_MS)
  }, [])
  useEffect(() => {
    if (hideProgressPanelAfter == null) return
    const remaining = Math.max(0, hideProgressPanelAfter - Date.now())
    const t = setTimeout(() => setHideProgressPanelAfter(null), remaining)
    return () => clearTimeout(t)
  }, [hideProgressPanelAfter])
  const prevSessionIdForPanelRef = useRef(null)
  useEffect(() => {
    if (prevSessionIdForPanelRef.current !== sessionId) {
      prevSessionIdForPanelRef.current = sessionId
      setHideProgressPanelAfter(null)
      setPrimaryVideoMounted(false)
    }
  }, [sessionId])
  useEffect(() => {
    if (!hasPrimaryR2Video) setPrimaryVideoMounted(false)
  }, [hasPrimaryR2Video])

  const runningStatesForPanel = ['queued', 'fetching', 'downloading', 'parsing', 'chunking', 'embedding']
  const hasJobForPanel = processingStatus?.state != null && processingStatus.state !== ''
  const isRunningForPanel = hasJobForPanel && runningStatesForPanel.includes(processingStatus.state)
  const readyButNoVideoYetForPanel = hasJobForPanel && processingStatus?.state === 'ready' && !hasPrimaryR2Video
  const processingInProgress = isRunningForPanel || readyButNoVideoYetForPanel
  // Show panel only while processing or preparing playback; hide once session is ready with video (e.g. on login we don't reshow)
  const showPanel = processingInProgress

  // Legacy ingestion status (fallback when no processing job)
  const [ingestionStatus, setIngestionStatus] = useState(null)
  const [ingestionRetrying, setIngestionRetrying] = useState(false)
  const ingestionIntervalRef = useRef(null)
  useEffect(() => {
    if (!sessionId || !apiBaseUrl || (processingStatus?.state != null && processingStatus.state !== '')) return
    const fetchIngestion = () => {
      fetch(`${apiBaseUrl}/api/sessions/${sessionId}/ingestion`, {
        headers: { 'X-Creator-Identity': creatorIdentity }
      })
        .then((r) => r.json())
        .then((data) => {
          if (data.source && data.state) {
            setIngestionStatus(data)
            if ((data.state === 'ready' || data.state === 'failed') && ingestionIntervalRef.current) {
              clearInterval(ingestionIntervalRef.current)
              ingestionIntervalRef.current = null
            }
          } else {
            setIngestionStatus(null)
          }
        })
        .catch(() => setIngestionStatus(null))
    }
    fetchIngestion()
    ingestionIntervalRef.current = setInterval(fetchIngestion, 2500)
    return () => {
      if (ingestionIntervalRef.current) {
        clearInterval(ingestionIntervalRef.current)
        ingestionIntervalRef.current = null
      }
    }
  }, [sessionId, apiBaseUrl, creatorIdentity, processingStatus?.state])

  // When Zoom import completes, refetch session until we have a playable video (video_access_url or video_sources). Backend sets video_access_url only when file artifact is Ready, so we must keep refetching.
  const hasRefetchedForIngestionReady = useRef(false)
  const readyState = processingStatus?.state === 'ready' ? 'ready' : ingestionStatus?.state === 'ready' ? 'ready' : null
  const hasPlayableVideo = hasPrimaryR2Video || (currentSession?.video_sources && currentSession.video_sources.length > 0)
  useEffect(() => {
    if (readyState !== 'ready' || !refetchSession) return
    if (hasPlayableVideo) return
    if (hasRefetchedForIngestionReady.current) return
    hasRefetchedForIngestionReady.current = true
    refetchSession()
    const t1 = setTimeout(() => refetchSession(), 1500)
    const t2 = setTimeout(() => refetchSession(), 3500)
    return () => { clearTimeout(t1); clearTimeout(t2) }
  }, [readyState, refetchSession, hasPlayableVideo])
  const prevSessionIdRef = useRef(sessionId)
  const videoPollAttemptsRef = useRef(0)
  useEffect(() => {
    if (prevSessionIdRef.current !== sessionId) {
      prevSessionIdRef.current = sessionId
      hasRefetchedForIngestionReady.current = false
      videoPollAttemptsRef.current = 0
    }
  }, [sessionId])

  // Poll session until we have a playable primary video (video_access_url) or video_sources — backend may set primary_video_artifact_id before file is Ready, so keep refetching until video_access_url appears
  const VIDEO_POLL_MAX_ATTEMPTS = 20
  const VIDEO_POLL_INTERVAL_MS = 2500
  useEffect(() => {
    if (!sessionId || !apiBaseUrl || !refetchSession) return
    if (hasPlayableVideo) return
    if (videoPollAttemptsRef.current >= VIDEO_POLL_MAX_ATTEMPTS) return
    const t = setInterval(() => {
      videoPollAttemptsRef.current += 1
      if (videoPollAttemptsRef.current > VIDEO_POLL_MAX_ATTEMPTS) return
      refetchSession()
    }, VIDEO_POLL_INTERVAL_MS)
    return () => clearInterval(t)
  }, [sessionId, apiBaseUrl, refetchSession, hasPlayableVideo])

  // Session transcript (Mission #2: GET /api/sessions/:id/transcript)
  const [transcriptData, setTranscriptData] = useState(null) // { status, source, updated_at, error_message, segments }
  const transcriptIntervalRef = useRef(null)
  useEffect(() => {
    if (!sessionId || !apiBaseUrl) return
    setTranscriptData(null) // reset so we show Loading when session changes; avoids stale transcript
    const fetchTranscript = () => {
      fetch(`${apiBaseUrl}/api/sessions/${sessionId}/transcript`)
        .then((r) => r.json())
        .then((data) => {
          setTranscriptData(data)
          // Only stop polling when we have a final state; keep polling on 'none' so async Zoom import will show transcript when ready
          if (data.status === 'ready' || data.status === 'failed') {
            if (transcriptIntervalRef.current) {
              clearInterval(transcriptIntervalRef.current)
              transcriptIntervalRef.current = null
            }
          }
        })
        .catch(() => setTranscriptData(null))
    }
    fetchTranscript()
    transcriptIntervalRef.current = setInterval(fetchTranscript, 2500)
    return () => {
      if (transcriptIntervalRef.current) {
        clearInterval(transcriptIntervalRef.current)
        transcriptIntervalRef.current = null
      }
    }
  }, [sessionId, apiBaseUrl, readyState])
  // readyState in deps: when Zoom import completes (state -> 'ready'), effect re-runs and refetches transcript

  const retryProcessing = async () => {
    if (!sessionId || processingRetrying) return
    setProcessingRetrying(true)
    try {
      await fetch(`${apiBaseUrl}/api/sessions/${sessionId}/processing/retry`, {
        method: 'POST',
        headers: { 'X-Creator-Identity': creatorIdentity }
      })
      setProcessingStatus((s) => (s ? { ...s, state: 'queued', last_error_code: '', last_error_message: '' } : null))
    } catch {
      // ignore
    } finally {
      setProcessingRetrying(false)
    }
  }

  const retryIngestion = async () => {
    if (!sessionId || !ingestionStatus?.meeting_uuid || ingestionRetrying) return
    setIngestionRetrying(true)
    try {
      await fetch(`${apiBaseUrl}/api/sessions/${sessionId}/import/zoom`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-Creator-Identity': creatorIdentity },
        body: JSON.stringify({
          meeting_uuid: ingestionStatus.meeting_uuid,
          instance_uuid: ingestionStatus.instance_uuid || ingestionStatus.meeting_uuid
        })
      })
      setIngestionStatus((s) => (s ? { ...s, state: 'queued' } : null))
    } catch {
      // ignore
    } finally {
      setIngestionRetrying(false)
    }
  }

  const uploadMaterialToSession = async (file) => {
    if (!sessionId || !apiBaseUrl || !file) return
    setMaterialUploading(true)
    setMaterialUploadFeedback({ type: '', message: '' })
    const base = (apiBaseUrl || '').replace(/\/$/, '')
    try {
      const form = new FormData()
      form.append('file', file)
      const res = await fetch(`${base}/sessions/${sessionId}/materials/upload`, { method: 'POST', body: form, credentials: 'include' })
      if (!res.ok) {
        const t = await res.text()
        setMaterialUploadFeedback({ type: 'error', message: t || res.statusText })
        return
      }
      setMaterialUploadFeedback({ type: 'success', message: `Uploaded ${file.name}` })
      if (materialUploadFeedbackTimeoutRef.current) clearTimeout(materialUploadFeedbackTimeoutRef.current)
      materialUploadFeedbackTimeoutRef.current = setTimeout(() => {
        setMaterialUploadFeedback({ type: '', message: '' })
        materialUploadFeedbackTimeoutRef.current = null
      }, 4000)
      if (refetchSession) await refetchSession()
      if (materialFileInputRef.current) materialFileInputRef.current.value = ''
    } catch (err) {
      setMaterialUploadFeedback({ type: 'error', message: err?.message || 'Upload failed' })
    } finally {
      setMaterialUploading(false)
      if (materialFileInputRef.current) materialFileInputRef.current.value = ''
    }
  }

  const handleMaterialFileChange = (e) => {
    const files = Array.from(e?.target?.files || [])
    if (files.length > 0) uploadMaterialToSession(files[0])
  }

  const [deletingMaterialId, setDeletingMaterialId] = useState(null)
  const [deleteMaterialError, setDeleteMaterialError] = useState(null)

  const deleteMaterial = async (materialId) => {
    if (!sessionId || !materialId || !apiBaseUrl) return
    setDeleteMaterialError(null)
    setDeletingMaterialId(String(materialId))
    try {
      const base = (apiBaseUrl || '').replace(/\/$/, '')
      const res = await fetch(`${base}/sessions/${sessionId}/materials/${materialId}`, { method: 'DELETE', credentials: 'include' })
      if (!res.ok) {
        const text = await res.text()
        throw new Error(text || `HTTP ${res.status}`)
      }
      if (refetchSession) await refetchSession()
    } catch (err) {
      setDeleteMaterialError(err?.message || 'Delete failed')
    } finally {
      setDeletingMaterialId(null)
    }
  }

  // Sync context form fields when session changes
  useEffect(() => {
    setContextPremise(currentSession?.session?.premise ?? '')
    setContextDecision(currentSession?.session?.primary_decision ?? '')
    setContextOutcome(currentSession?.session?.decision_outcome ?? '')
  }, [currentSession?.session?.id, currentSession?.session?.premise, currentSession?.session?.primary_decision, currentSession?.session?.decision_outcome])

  const fetchStances = useCallback(async () => {
    if (!currentSession?.session?.id || !apiBaseUrl) return
    const base = (apiBaseUrl || '').replace(/\/$/, '')
    try {
      const res = await fetch(`${base}/api/sessions/${currentSession.session.id}/stances`, { credentials: 'include' })
      if (!res.ok) return
      const data = await res.json()
      setStanceData(data)
    } catch { /* ignore */ }
  }, [currentSession?.session?.id, apiBaseUrl])

  useEffect(() => {
    fetchStances()
  }, [fetchStances, stanceVersion])

  useEffect(() => {
    if (stanceData?.my_stance?.rationale != null) {
      setStanceRationale(typeof stanceData.my_stance.rationale === 'string' ? stanceData.my_stance.rationale : '')
    }
  }, [stanceData?.my_stance?.rationale])

  const submitStance = async (stanceValue) => {
    if (!currentSession?.session?.id || stanceSubmitting || !apiBaseUrl) return
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
      const data = await res.json().catch(() => ({}))
      if (!res.ok) {
        const msg = data.error || `HTTP ${res.status}`
        setStanceFeedback({ type: 'error', message: msg })
        return
      }
      setStanceFeedback({ type: 'success', message: 'Position recorded' })
      await fetchStances()
    } catch (err) {
      setStanceFeedback({ type: 'error', message: err?.message || 'Failed to submit stance' })
    } finally {
      setStanceSubmitting(false)
    }
  }

  const clearStance = async () => {
    if (!currentSession?.session?.id || stanceSubmitting || currentSession.session.decision_outcome) return
    setStanceSubmitting(true)
    setStanceFeedback({ type: '', message: '' })
    const base = (apiBaseUrl || '').replace(/\/$/, '')
    try {
      const res = await fetch(`${base}/api/sessions/${currentSession.session.id}/stance`, {
        method: 'DELETE',
        credentials: 'include'
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        setStanceFeedback({ type: 'error', message: data.error || `HTTP ${res.status}` })
        return
      }
      setStanceRationale('')
      setStanceFeedback({ type: 'success', message: 'Decision cleared' })
      await fetchStances()
    } catch (err) {
      setStanceFeedback({ type: 'error', message: err?.message || 'Failed to clear stance' })
    } finally {
      setStanceSubmitting(false)
    }
  }

  const saveSessionContext = async () => {
    if (!currentSession?.session?.id || contextSaving) return
    setContextSaving(true)
    setContextFeedback({ type: '', message: '' })
    const base = (apiBaseUrl || '').replace(/\/$/, '')
    try {
      const res = await fetch(`${base}/api/sessions/${currentSession.session.id}`, {
        method: 'PATCH',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ premise: contextPremise, primary_decision: contextDecision, decision_outcome: contextOutcome })
      })
      if (!res.ok) {
        const t = await res.text()
        setContextFeedback({ type: 'error', message: t || res.statusText })
        return
      }
      setContextFeedback({ type: 'success', message: 'Saved' })
      if (refetchSession) await refetchSession()
      setTimeout(() => setContextFeedback({ type: '', message: '' }), 2500)
    } catch (err) {
      setContextFeedback({ type: 'error', message: err?.message || 'Save failed' })
    } finally {
      setContextSaving(false)
    }
  }

  if (!currentSession) {
    return (
      <div style={{ padding: '20px', color: '#666', textAlign: 'center' }}>
        {loading ? 'Loading session...' : 'No session loaded. Select or create a session to continue.'}
      </div>
    )
  }

  return (
    <>
      {/* Topbar */}
      <div className="creator-layout-topbar">
        <div style={{ display: 'flex', alignItems: 'center', gap: '10px', minWidth: 0, flex: 1 }}>
          {currentSession?.session && (
            <>
              <h2 style={{ margin: 0, fontSize: '18px', color: '#2e7d32', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                {currentSession.session.title}
                <span style={{ fontWeight: 'normal', fontSize: '0.85rem', color: '#666', marginLeft: '8px' }}>(ID: {currentSession.session.id})</span>
              </h2>
              <span style={{ fontSize: '12px', color: currentSession.session.status === 'open' ? '#2e7d32' : '#999', fontWeight: 'bold', flexShrink: 0 }}>
                {currentSession.session.status}
              </span>
            </>
          )}
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: '8px', flexShrink: 0 }}>
          <button type="button" className="creator-panel-toggle" onClick={() => setRightPanelCollapsed(v => !v)} title={rightPanelCollapsed ? 'Expand Q&A panel' : 'Collapse Q&A panel'}>
            {rightPanelCollapsed ? '‹' : '›'}
          </button>
          {onClearSession && (
            <button type="button" onClick={onClearSession} style={{ backgroundColor: '#f44336', color: 'white', padding: '6px 12px', border: 'none', borderRadius: '4px', cursor: 'pointer', fontWeight: 500, fontSize: '13px', margin: 0 }}>
              Clear Session
            </button>
          )}
        </div>
      </div>

      {/* Premise / Decision / Outcome: always visible like participant view */}
      {currentSession?.session && (currentSession.session.premise || currentSession.session.primary_decision || currentSession.session.decision_outcome) && (
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

      {/* Processing progression: flex-shrink 0, sits above grid */}
      {showPanel && (() => {
        const runningStates = ['queued', 'fetching', 'downloading', 'parsing', 'chunking', 'embedding']
        const hasJob = processingStatus?.state != null && processingStatus.state !== ''
        const isRunning = hasJob && runningStates.includes(processingStatus.state)
        const isFailed = hasJob && (processingStatus.state === 'failed_transient' || processingStatus.state === 'failed_permanent')
        const isWaiting = hasJob && processingStatus.state === 'waiting'
        const readyButNoVideoYet = hasJob && processingStatus.state === 'ready' && !hasPrimaryR2Video
        const allComplete = hasJob && processingStatus.state === 'ready' && hasPrimaryR2Video
        const activeStepIndex = displayStepIndex
        return (
          <div style={{
            flexShrink: 0,
            padding: '12px 16px',
            borderRadius: 0,
            borderBottom: '1px solid #e0e0e0',
            backgroundColor: allComplete ? '#e8f5e9' : hasJob && processingStatus.state === 'failed_permanent' ? '#ffebee' : isWaiting ? '#e3f2fd' : '#fff8e1',
            borderLeft: allComplete ? '3px solid #4CAF50' : hasJob && processingStatus.state === 'failed_permanent' ? '3px solid #f44336' : isWaiting ? '3px solid #2196F3' : '3px solid #ff9800'
          }}>
            <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'flex-start', justifyContent: 'space-between', gap: '10px' }}>
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: '4px', flexWrap: 'wrap', marginBottom: '6px', fontSize: '13px' }}>
                  {PROCESSING_STEPS.map((label, idx) => {
                    const completed = idx < activeStepIndex || allComplete
                    const active = idx === activeStepIndex && !completed
                    const showSpinner = active && (isRunning || !hasJob || readyButNoVideoYet || displayStepIndex < lastKnownTargetStepRef.current)
                    const showWaiting = active && isWaiting
                    const showWarning = active && isFailed
                    const activeWeight = 700
                    return (
                      <div key={label} style={{ display: 'flex', alignItems: 'center', gap: '3px' }}>
                        <span style={{
                          fontSize: '13px',
                          color: completed ? '#4CAF50' : active ? (showWarning ? '#f44336' : '#1976D2') : '#9e9e9e',
                          fontWeight: active ? activeWeight : 400
                        }}>
                          {completed ? '✓' : showSpinner ? '⌛' : showWaiting ? '⏸' : showWarning ? '⚠' : '○'}
                        </span>
                        <span style={{ color: completed ? '#2e7d32' : active ? '#1a1a1a' : '#9e9e9e', fontSize: '13px', fontWeight: active ? activeWeight : 400 }}>{label}</span>
                        {idx < PROCESSING_STEPS.length - 1 && <span style={{ marginLeft: '1px', marginRight: '1px', color: '#bdbdbd', fontSize: '12px' }}>→</span>}
                      </div>
                    )
                  })}
                </div>
                <div style={{ fontSize: '13px', fontWeight: 600, color: '#333' }}>
                  {!hasJob
                    ? 'Checking import status…'
                    : isRunning
                      ? <span className="processing-flash">Processing this session…</span>
                      : isWaiting
                        ? "Waiting for Zoom to finish processing. We'll keep checking."
                        : processingStatus.state === 'failed_transient'
                          ? "Temporary issue. We'll retry automatically. You can retry now."
                          : processingStatus.state === 'failed_permanent'
                            ? `Processing failed. ${processingStatus.last_error_message || processingStatus.last_error_code || 'Unknown error'}. Reconnect Zoom or retry.`
                            : readyButNoVideoYet
                              ? 'Preparing playback — video will appear on screen shortly…'
                              : processingStatus.state === 'ready'
                                ? 'Import complete — video and transcript are ready.'
                                : processingStatus.state === 'canceled'
                                  ? 'Import canceled.'
                                  : processingStatus.state}
                </div>
              </div>
              <div style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
                {hasJob && (processingStatus.state === 'failed_transient' || processingStatus.state === 'failed_permanent' || processingStatus.state === 'waiting') && (
                  <button
                    type="button"
                    onClick={retryProcessing}
                    disabled={processingRetrying}
                    style={{
                      padding: '4px 10px',
                      fontSize: '12px',
                      backgroundColor: '#2196F3',
                      color: 'white',
                      border: 'none',
                      borderRadius: '4px',
                      cursor: processingRetrying ? 'not-allowed' : 'pointer',
                      margin: 0
                    }}
                  >
                    {processingRetrying ? 'Retrying…' : 'Retry now'}
                  </button>
                )}
                {hasJob && processingStatus.state === 'failed_permanent' && (processingStatus.last_error_code === 'zoom_auth' || processingStatus.last_error_code === 'zoom_not_connected') && (
                  <a
                    href={`${window.location.origin}${window.location.pathname}?session=${sessionId}&zoom=connect`}
                    style={{ fontSize: '12px', color: '#1976D2', fontWeight: 500 }}
                  >
                    Reconnect Zoom
                  </a>
                )}
              </div>
            </div>
          </div>
        )
      })()}

      {/* Legacy ingestion banner */}
      {!(processingStatus?.state != null && processingStatus.state !== '') && ingestionStatus?.source === 'zoom' && (
        <div style={{
          flexShrink: 0,
          padding: '10px 16px',
          borderBottom: '1px solid #e0e0e0',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          flexWrap: 'wrap',
          gap: '8px',
          backgroundColor: ingestionStatus.state === 'ready' ? '#e8f5e9' : ingestionStatus.state === 'failed' ? '#ffebee' : '#fff8e1',
          borderLeft: ingestionStatus.state === 'ready' ? '3px solid #4CAF50' : ingestionStatus.state === 'failed' ? '3px solid #f44336' : '3px solid #ff9800'
        }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
            {(ingestionStatus.state === 'queued' || ingestionStatus.state === 'fetching') && (
              <span style={{ fontSize: '16px' }}>⏳</span>
            )}
            <span style={{ fontSize: '13px', fontWeight: 500 }}>
              {ingestionStatus.state === 'queued' || ingestionStatus.state === 'fetching'
                ? 'Import in progress…'
                : ingestionStatus.state === 'ready'
                  ? 'Zoom import complete'
                  : ingestionStatus.state === 'failed'
                    ? (ingestionStatus.last_error || 'Import failed')
                    : ''}
            </span>
            {ingestionStatus.updated_at && (
              <span style={{ fontSize: '11px', color: '#666' }}>
                Updated: {new Date(ingestionStatus.updated_at).toLocaleString()}
              </span>
            )}
          </div>
          {ingestionStatus.state === 'failed' && (
            <button
              type="button"
              onClick={retryIngestion}
              disabled={ingestionRetrying}
              style={{
                padding: '4px 10px',
                fontSize: '12px',
                backgroundColor: '#2196F3',
                color: 'white',
                border: 'none',
                borderRadius: '4px',
                cursor: ingestionRetrying ? 'not-allowed' : 'pointer',
                margin: 0
              }}
            >
              {ingestionRetrying ? 'Retrying…' : 'Retry'}
            </button>
          )}
        </div>
      )}

      {/* Three-panel grid */}
      <div className={`creator-layout-grid${leftPanelCollapsed ? ' left-collapsed' : ''}${rightPanelCollapsed ? ' right-collapsed' : ''}`}>

        {/* ===== LEFT PANEL ===== */}
        <div className={`creator-left-panel${leftPanelCollapsed ? ' collapsed' : ''}`}>
          <div className="creator-panel-header">
            <MaterialsPanelHeader
              collapsed={leftPanelCollapsed}
              onCollapsedChange={setLeftPanelCollapsed}
              unreadCount={Array.isArray(currentSession?.unread_material_ids) ? currentSession.unread_material_ids.length : 0}
            />
          </div>
          {!leftPanelCollapsed && (
            <div className="creator-left-scroll">

              {/* Decisions: at top so creator can see/capture stance first */}
              {currentSession?.session?.primary_decision && (
                <div style={{ padding: '8px 12px', borderBottom: '1px solid #e0e0e0', backgroundColor: '#f1f8e9' }}>
                  <button type="button" onClick={() => setStancePanelExpanded(e => !e)} className="creator-collapsible-btn" aria-expanded={stancePanelExpanded}>
                    <span style={{ fontSize: '12px', color: '#555' }} aria-hidden>{stancePanelExpanded ? '▼' : '▷'}</span>
                    {' '}Decisions ({stanceData?.aggregate?.total ?? 0})
                  </button>
                  {stancePanelExpanded && (
                    <div style={{ marginTop: '8px' }}>
                      <div style={{ fontSize: '11px', fontWeight: 600, color: '#555', marginBottom: '6px' }}>Your decision</div>
                      {currentSession.session.decision_outcome ? (
                        <p style={{ margin: 0, fontSize: '11px', color: '#888', fontStyle: 'italic' }}>Outcome recorded — stances are locked.</p>
                      ) : (
                        <>
                          <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', justifyContent: 'space-between', gap: '6px', marginBottom: '6px' }}>
                            <div style={{ display: 'flex', gap: '4px', flexWrap: 'wrap' }}>
                              {['agree', 'disagree', 'conditional', 'abstain', 'need_more_info'].map((s) => {
                                const label = s === 'need_more_info' ? 'Need More Info' : s.charAt(0).toUpperCase() + s.slice(1)
                                const bg = s === 'agree' ? '#e8f5e9' : s === 'disagree' ? '#ffebee' : s === 'conditional' ? '#fff3e0' : s === 'abstain' ? '#eceff1' : '#e3f2fd'
                                const border = s === 'agree' ? '#81c784' : s === 'disagree' ? '#e57373' : s === 'conditional' ? '#ffb74d' : s === 'abstain' ? '#90a4ae' : '#64b5f6'
                                const textColor = s === 'agree' ? '#2e7d32' : s === 'disagree' ? '#c62828' : s === 'conditional' ? '#e65100' : s === 'abstain' ? '#546e7a' : '#1565c0'
                                const myStance = stanceData?.my_stance
                                return (
                                  <button
                                    key={s}
                                    type="button"
                                    onClick={() => submitStance(s)}
                                    disabled={stanceSubmitting}
                                    style={{
                                      padding: '4px 10px',
                                      fontSize: '11px',
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
                            {(stanceData?.my_stance?.stance || stanceRationale?.trim()) && (
                              <button
                                type="button"
                                onClick={clearStance}
                                disabled={stanceSubmitting}
                                style={{
                                  marginLeft: 'auto',
                                  padding: '4px 10px',
                                  fontSize: '11px',
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
                            onBlur={() => { if (stanceData?.my_stance?.stance && !stanceSubmitting) submitStance(stanceData.my_stance.stance) }}
                            style={{ width: '100%', padding: '4px 8px', fontSize: '11px', border: '1px solid #e0e0e0', borderRadius: '4px', boxSizing: 'border-box', marginBottom: '4px' }}
                          />
                          {stanceFeedback.message && (
                            <p style={{ margin: 0, fontSize: '11px', color: stanceFeedback.type === 'error' ? '#c62828' : '#2e7d32' }}>{stanceFeedback.message}</p>
                          )}
                        </>
                      )}
                      <div style={{ fontSize: '11px', fontWeight: 600, color: '#555', marginTop: '10px', marginBottom: '4px' }}>Members&apos; decisions</div>
                      <div style={{ display: 'flex', gap: '4px', flexWrap: 'wrap', marginBottom: '8px' }}>
                        {[
                          ['Agree', stanceData?.aggregate?.agree ?? 0, '#2e7d32', '#e8f5e9'],
                          ['Disagree', stanceData?.aggregate?.disagree ?? 0, '#c62828', '#ffebee'],
                          ['Conditional', stanceData?.aggregate?.conditional ?? 0, '#e65100', '#fff3e0'],
                          ['Abstain', stanceData?.aggregate?.abstain ?? 0, '#546e7a', '#eceff1'],
                          ['Need More Info', stanceData?.aggregate?.need_more_info ?? 0, '#1565C0', '#e3f2fd']
                        ].map(([label, count, color, bg]) => (
                          <span key={label} style={{ padding: '2px 8px', borderRadius: '10px', fontSize: '11px', fontWeight: count > 0 ? 700 : 400, color: count > 0 ? color : '#999', backgroundColor: count > 0 ? bg : '#f5f5f5', border: `1px solid ${count > 0 ? color : '#e0e0e0'}` }}>
                            {label}: {count}
                          </span>
                        ))}
                      </div>
                      {(stanceData?.responses?.length > 0) ? (
                        <div style={{ maxHeight: '160px', overflowY: 'auto', display: 'flex', flexDirection: 'column', gap: '4px' }}>
                          {stanceData.responses.map((r) => (
                            <div key={r.id} style={{ padding: '4px 8px', backgroundColor: '#fafafa', border: '1px solid #e0e0e0', borderRadius: '3px', fontSize: '11px' }}>
                              <span style={{ fontWeight: 600 }}>{r.user_email}</span>
                              {' — '}
                              <span style={{ textTransform: 'capitalize' }}>{(r.stance || '').replace(/_/g, ' ')}</span>
                              {r.rationale && r.rationale.trim() && <span style={{ color: '#666', marginLeft: '4px' }}>&quot;{r.rationale.trim().length > 60 ? r.rationale.trim().slice(0, 60) + '…' : r.rationale.trim()}&quot;</span>}
                            </div>
                          ))}
                        </div>
                      ) : (
                        <p style={{ margin: 0, fontSize: '11px', color: '#888' }}>No responses yet.</p>
                      )}
                    </div>
                  )}
                </div>
              )}

              {/* Context: Premise, Decision, Outcome */}
              <div style={{ padding: '8px 12px', borderBottom: '1px solid #e0e0e0', backgroundColor: '#f1f8e9' }}>
                <button type="button" onClick={() => setContextPanelExpanded(e => !e)} className="creator-collapsible-btn" aria-expanded={contextPanelExpanded}>
                  <span style={{ fontSize: '12px', color: '#555' }} aria-hidden>{contextPanelExpanded ? '▼' : '▷'}</span>
                  {' '}<strong>Context</strong>
                </button>
                {contextPanelExpanded && (
                  <div style={{ marginTop: '8px' }}>
                    <label style={{ display: 'block', marginBottom: '3px', fontSize: '12px', fontWeight: '500' }}>Premise</label>
                    <textarea value={contextPremise} onChange={e => setContextPremise(e.target.value)} placeholder="Describe the session premise…" rows={2} style={{ width: '100%', padding: '4px 6px', fontSize: '12px', resize: 'vertical', boxSizing: 'border-box', border: '1px solid #ddd', borderRadius: '3px' }} />
                    <label style={{ display: 'block', marginTop: '6px', marginBottom: '3px', fontSize: '12px', fontWeight: '500' }}>Primary Decision</label>
                    <textarea value={contextDecision} onChange={e => setContextDecision(e.target.value)} placeholder="What is the primary decision being discussed…" rows={2} style={{ width: '100%', padding: '4px 6px', fontSize: '12px', resize: 'vertical', boxSizing: 'border-box', border: '1px solid #ddd', borderRadius: '3px' }} />
                    <label style={{ display: 'block', marginTop: '6px', marginBottom: '3px', fontSize: '12px', fontWeight: '500' }}>Decision Outcome</label>
                    <textarea value={contextOutcome} onChange={e => setContextOutcome(e.target.value)} placeholder="What was actually decided…" rows={2} style={{ width: '100%', padding: '4px 6px', fontSize: '12px', resize: 'vertical', boxSizing: 'border-box', border: '1px solid #ddd', borderRadius: '3px' }} />
                    <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginTop: '8px' }}>
                      <button type="button" onClick={saveSessionContext} disabled={contextSaving} style={{ padding: '4px 12px', fontSize: '12px', margin: 0 }}>
                        {contextSaving ? 'Saving…' : 'Save'}
                      </button>
                      {contextFeedback.message && (
                        <span style={{ fontSize: '11px', color: contextFeedback.type === 'error' ? '#c62828' : '#2e7d32' }}>{contextFeedback.message}</span>
                      )}
                    </div>
                  </div>
                )}
              </div>

              {/* Members — prominent at top */}
              {authUser && inviteUserToSession && (
                <div style={{ padding: '8px 12px', borderBottom: '1px solid #e0e0e0', backgroundColor: '#f1f8e9' }}>
                  <button type="button" onClick={() => setMembersPanelExpanded(e => !e)} className="creator-collapsible-btn" aria-expanded={membersPanelExpanded}>
                    <span style={{ fontSize: '12px', color: '#555' }} aria-hidden>{membersPanelExpanded ? '▼' : '▷'}</span>
                    {' '}<strong>Members</strong>{sessionInvitations?.length > 0 ? ` (${sessionInvitations.length})` : ''}
                  </button>
                  {membersPanelExpanded && (
                    <div style={{ marginTop: '8px' }}>
                      <div style={{ display: 'flex', gap: '6px', alignItems: 'center', flexWrap: 'wrap', marginBottom: '6px' }}>
                        <input type="email" value={inviteEmail ?? ''} onChange={e => setInviteEmail?.(e.target.value)} placeholder="user@example.com" style={{ flex: '1', minWidth: '120px', padding: '4px 8px', fontSize: '12px', border: '1px solid #ddd', borderRadius: '3px' }} />
                        <select value={inviteRole ?? 'participant'} onChange={e => setInviteRole?.(e.target.value)} style={{ padding: '4px 6px', fontSize: '12px', border: '1px solid #ddd', borderRadius: '3px' }}>
                          <option value="participant">Participant</option>
                          <option value="creator">Creator</option>
                        </select>
                        <button type="button" onClick={inviteUserToSession} disabled={!inviteEmail?.trim() || !isValidEmailFormat(inviteEmail?.trim()) || inviteLoading} style={{ padding: '4px 10px', fontSize: '12px', margin: 0 }}>
                          {inviteLoading ? '…' : 'Invite'}
                        </button>
                      </div>
                      {inviteFeedback?.message && (
                        <div className={inviteFeedback.type} style={{ fontSize: '12px', padding: '4px 6px', marginBottom: '6px' }}>{inviteFeedback.message}</div>
                      )}
                      {sessionInvitations?.length > 0 && (
                        <div style={{ fontSize: '11px', overflowX: 'auto' }}>
                          <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                            <thead>
                              <tr style={{ borderBottom: '1px solid #ddd', textAlign: 'left' }}>
                                <th style={{ padding: '4px 6px' }}>Email</th>
                                <th style={{ padding: '4px 6px' }}>Role</th>
                                <th style={{ padding: '4px 6px' }}>Status</th>
                                {typeof fetchSessionInvitations === 'function' && <th style={{ padding: '4px 6px' }}>Actions</th>}
                              </tr>
                            </thead>
                            <tbody>
                              {sessionInvitations.map((inv) => (
                                <tr key={inv.id} style={{ borderBottom: '1px solid #eee' }}>
                                  <td style={{ padding: '4px 6px' }}>{inv.invited_email}</td>
                                  <td style={{ padding: '4px 6px' }}>{inv.invited_role || 'participant'}</td>
                                  <td style={{ padding: '4px 6px' }}>{inv.status}</td>
                                  {typeof fetchSessionInvitations === 'function' && (
                                    <td style={{ padding: '4px 6px' }}>
                                      {inv.status === 'pending' && (
                                        <>
                                          <InvitationActionButton apiBaseUrl={apiBaseUrl} invitationId={inv.id} action="resend" onDone={(data) => { if (data?.invitation) { if (typeof setLastInvitationDraft === 'function') setLastInvitationDraft(data.invitation); if (typeof setInviteFeedback === 'function') { setInviteFeedback({ type: 'success', message: 'New link ready.' }); setTimeout(() => setInviteFeedback({ type: '', message: '' }), 6000) } } fetchSessionInvitations(currentSession?.session?.id ?? currentSession?.id) }} />
                                          <InvitationActionButton apiBaseUrl={apiBaseUrl} invitationId={inv.id} action="revoke" onDone={() => fetchSessionInvitations(currentSession?.session?.id ?? currentSession?.id)} />
                                          <CopyInvitationLinkButton apiBaseUrl={apiBaseUrl} invitationId={inv.id} onCopied={() => { if (typeof setInviteFeedback === 'function') { setInviteFeedback({ type: 'success', message: 'Copied.' }); setTimeout(() => setInviteFeedback({ type: '', message: '' }), 2000) } }} onError={(msg) => { if (typeof setInviteFeedback === 'function') setInviteFeedback({ type: 'error', message: msg || 'Failed' }) }} />
                                          <OpenEmailDraftButton apiBaseUrl={apiBaseUrl} invitationId={inv.id} invitation={{ invited_email: inv.invited_email, expires_at: inv.expires_at }} sessionTitle={currentSession?.session?.title} inviterEmail={currentSession?.session?.created_by} inviterDisplayName={currentSession?.created_by_display_name} onError={(msg) => { if (typeof setInviteFeedback === 'function') setInviteFeedback({ type: 'error', message: msg || 'Failed' }) }} />
                                        </>
                                      )}
                                    </td>
                                  )}
                                </tr>
                              ))}
                            </tbody>
                          </table>
                        </div>
                      )}
                    </div>
                  )}
                </div>
              )}

              {/* Hidden file input for material uploads */}
              {sessionId && (
                <input
                  ref={materialFileInputRef}
                  type="file"
                  accept=".pdf,.txt,.md,.docx,.xlsx,.pptx,.jpg,.jpeg,.png,.gif,.webp,.bmp,.svg,video/mp4,.mp4,application/pdf,text/plain,application/vnd.openxmlformats-officedocument.wordprocessingml.document,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,application/vnd.openxmlformats-officedocument.presentationml.presentation,image/jpeg,image/png,image/gif,image/webp,image/bmp,image/svg+xml"
                  onChange={handleMaterialFileChange}
                  disabled={materialUploading}
                  style={{ display: 'none' }}
                />
              )}

              {/* Add content at top: upload files + add links */}
              {sessionId && (
                <AddContentSection
                  sessionId={sessionId}
                  apiBaseUrl={apiBaseUrl}
                  refetchSession={refetchSession}
                  onUploadClick={() => materialFileInputRef.current?.click()}
                  uploading={materialUploading}
                  uploadFeedback={materialUploadFeedback}
                  defaultExpanded={
                    !currentSession?.materials?.length && !currentSession?.links?.length
                  }
                />
              )}

              {/* Materials tree (no duplicate header; topmost Materials is shared) */}
              <MaterialsTreePanel
                session={currentSession}
                selectedVideo={selectedVideo}
                setSelectedVideo={setSelectedVideo}
                setVideoId={setVideoId}
                setVideoPlayerKey={setVideoPlayerKey}
                onSelectDocument={handleSelectDocument}
                onSelectVideo={handleBackToVideo}
                onSelectLink={handleSelectLink}
                selectedDocumentId={selectedDocumentId}
                collapsed={leftPanelCollapsed}
                onCollapsedChange={setLeftPanelCollapsed}
                hideTranscriptSection
                hideHeader
                lastSeenLinkCount={0}
                canManage={!!sessionId}
                onDeleteMaterial={deleteMaterial}
                deletingId={deletingMaterialId}
                deleteError={deleteMaterialError}
              />

            </div>
          )}
        </div>

        {/* ===== CENTER PANEL ===== */}
        <div className="creator-center-panel">
          {selectedDocument ? (
            /* Document/slides/link viewer (same as participant); navigate via left panel */
            <div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column', overflow: 'hidden', padding: '12px' }}>
              <div style={{ flex: 1, minHeight: 0, overflow: 'auto' }}>
                <DocumentViewer
                  doc={selectedDocument}
                  apiBaseUrl={apiBaseUrl}
                  sessionId={sessionId}
                />
              </div>
            </div>
          ) : (
          <>
          {/* Video player section */}
          {((currentSession?.video_sources && currentSession.video_sources.length > 0) || currentSession?.session?.primary_video_artifact_id) && (
            <div className="creator-video-container">
              {/* Explicit non-ready states when primary_video_artifact_id is set but no playable URL */}
              {currentSession?.session?.primary_video_artifact_id && !primaryVideoAccessUrl && currentSession?.playback_reason_code && (
                <div style={{
                  padding: '24px',
                  backgroundColor: currentSession.playback_reason_code === 'VIDEO_INGEST_PENDING' ? '#fff8e1' : currentSession.playback_reason_code === 'VIDEO_INGEST_FAILED' ? '#ffebee' : '#f5f5f5',
                  textAlign: 'center',
                  border: '1px solid #e0e0e0'
                }}>
                  <p style={{ margin: '0 0 12px', color: '#333', fontSize: '15px' }}>
                    {currentSession.playback_message || (currentSession.playback_reason_code === 'VIDEO_INGEST_PENDING' ? 'Video is still being prepared. Refresh in a moment.' : currentSession.playback_reason_code === 'VIDEO_INGEST_FAILED' ? 'Video ingest failed. Creator can retry import.' : 'Video not available for this session.')}
                  </p>
                  {currentSession.playback_reason_code === 'VIDEO_INGEST_FAILED' && (
                    <button
                      type="button"
                      onClick={retryProcessing}
                      disabled={processingRetrying}
                      style={{
                        padding: '8px 16px',
                        fontSize: '14px',
                        backgroundColor: '#2196F3',
                        color: 'white',
                        border: 'none',
                        borderRadius: '4px',
                        cursor: processingRetrying ? 'not-allowed' : 'pointer',
                        margin: 0
                      }}
                    >
                      {processingRetrying ? 'Retrying…' : 'Retry ingest'}
                    </button>
                  )}
                </div>
              )}
              {hasPrimaryR2Video && (
                <div style={{ padding: '6px 12px', fontSize: '13px', color: '#ccc', backgroundColor: '#1a1a1a' }}>
                  <strong style={{ color: '#999' }}>Transcript:</strong>{' '}
                  <span style={{
                    color: video.transcript_status === 'ready' ? '#4CAF50' :
                           video.transcript_status === 'pending' ? '#ff9800' :
                           video.transcript_status === 'failed' ? '#f44336' : '#999',
                    fontWeight: 'bold'
                  }}>
                    {video.transcript_status === 'missing' ? 'No transcript' :
                     video.transcript_status === 'pending' ? 'Pending...' :
                     video.transcript_status === 'processing' ? 'Processing...' :
                     video.transcript_status === 'ready' ? 'Ready' :
                     video.transcript_status === 'failed' ? 'Failed' :
                     video.transcript_status || 'Unknown'}
                  </span>
                  {transcriptJobs[video?.id] && (
                    <>
                      {' | '}
                      <strong style={{ color: '#999' }}>Job:</strong>{' '}
                      <span style={{
                        color: transcriptJobs[video.id].status === 'completed' ? '#4CAF50' :
                               transcriptJobs[video.id].status === 'failed' ? '#f44336' : '#ff9800',
                        fontWeight: 'bold'
                      }}>
                        {transcriptJobs[video.id].status}
                      </span>
                    </>
                  )}
                </div>
              )}
              {video && !(currentSession?.session?.primary_video_artifact_id && !primaryVideoAccessUrl && currentSession?.playback_reason_code) && (
                <VideoPlayer
                  video={video}
                  onEvent={handleVideoPlayerEvent}
                  onTimeUpdate={handleVideoTimeUpdate}
                  currentTime={currentVideoTime}
                  playing={isVideoPlaying}
                  sessionId={currentSession?.session?.id || currentSession?.id}
                  apiBaseUrl={apiBaseUrl}
                  creatorIdentity={creatorIdentity}
                  primaryVideoAccessUrl={primaryVideoAccessUrl}
                  primaryVideoArtifactId={currentSession?.session?.primary_video_artifact_id ?? null}
                  onPrimaryVideoMounted={handlePrimaryVideoMounted}
                />
              )}
            </div>
          )}

          {/* Transcript and other content — scrollable */}
          <div className="creator-center-scroll">
            {/* Inline transcript from video source */}
            {video && !(currentSession?.session?.primary_video_artifact_id && !primaryVideoAccessUrl && currentSession?.playback_reason_code) && ((currentSession?.video_sources && currentSession.video_sources.length > 0) || currentSession?.session?.primary_video_artifact_id) && (
              <div style={{ marginBottom: '12px' }}>
                {video.transcript_text || (video.transcript_segments && video.transcript_segments.length > 0) ? (
                  <TranscriptViewer
                    transcriptText={video.transcript_text}
                    segments={video.transcript_segments}
                    showTimestamps={true}
                  />
                ) : (
                  <div style={{ padding: '12px', color: '#666', fontSize: '14px', fontStyle: 'italic' }}>
                    Transcript: {video.transcript_status === 'pending' || video.transcript_status === 'processing' ? 'Processing…' : 'No transcript yet.'}
                  </div>
                )}
              </div>
            )}

            {/* Session-level transcript (when no inline transcript from video) */}
            {sessionId && !(hasPrimaryR2Video && (video?.transcript_text || (video?.transcript_segments && video?.transcript_segments?.length > 0))) && (
              <div style={{ marginBottom: '12px' }}>
                <div style={{ fontWeight: 600, fontSize: '13px', marginBottom: '8px', color: '#555' }}>Transcript</div>
                {!transcriptData ? (
                  <div style={{ color: '#666', fontSize: '13px' }}>Loading…</div>
                ) : transcriptData.status === 'none' ? (
                  <div style={{ color: '#666', fontStyle: 'italic', fontSize: '13px' }}>No transcript yet. Import a Zoom recording to add one.</div>
                ) : transcriptData.status === 'parsing' ? (
                  <div style={{ display: 'flex', alignItems: 'center', gap: '10px', fontSize: '13px' }}>
                    <span style={{ fontSize: '16px' }}>⏳</span>
                    <span>Parsing transcript…</span>
                  </div>
                ) : transcriptData.status === 'failed' ? (
                  <div>
                    <div style={{ color: '#c62828', marginBottom: '10px', fontSize: '13px' }}>
                      {transcriptData.error_message || 'Transcript failed.'}
                    </div>
                    {(processingStatus?.state != null && processingStatus.state !== '') ? (
                      <button
                        type="button"
                        onClick={retryProcessing}
                        disabled={processingRetrying}
                        style={{
                          padding: '6px 12px',
                          fontSize: '13px',
                          backgroundColor: '#2196F3',
                          color: 'white',
                          border: 'none',
                          borderRadius: '4px',
                          cursor: processingRetrying ? 'not-allowed' : 'pointer',
                          margin: 0
                        }}
                      >
                        {processingRetrying ? 'Retrying…' : 'Retry now'}
                      </button>
                    ) : ingestionStatus?.meeting_uuid ? (
                      <button
                        type="button"
                        onClick={retryIngestion}
                        disabled={ingestionRetrying}
                        style={{
                          padding: '6px 12px',
                          fontSize: '13px',
                          backgroundColor: '#2196F3',
                          color: 'white',
                          border: 'none',
                          borderRadius: '4px',
                          cursor: ingestionRetrying ? 'not-allowed' : 'pointer',
                          margin: 0
                        }}
                      >
                        {ingestionRetrying ? 'Retrying…' : 'Retry import'}
                      </button>
                    ) : null}
                  </div>
                ) : transcriptData.status === 'ready' && transcriptData.segments?.length > 0 ? (
                  <div>
                    {transcriptData.segments.map((seg) => {
                      const mmSs = (ms) => {
                        const m = Math.floor(ms / 60000)
                        const s = Math.floor((ms % 60000) / 1000)
                        return `${m}:${s.toString().padStart(2, '0')}`
                      }
                      return (
                        <div key={seg.idx} style={{ marginBottom: '10px', padding: '6px 0', borderBottom: '1px solid #eee' }}>
                          <span style={{ fontSize: '11px', color: '#666', marginRight: '8px' }}>
                            {mmSs(seg.start_ms)}–{mmSs(seg.end_ms)}
                          </span>
                          <span style={{ fontSize: '13px' }}>{seg.text}</span>
                        </div>
                      )
                    })}
                  </div>
                ) : transcriptData.status === 'ready' ? (
                  <div style={{ color: '#666', fontStyle: 'italic', fontSize: '13px' }}>Transcript ready (no segments).</div>
                ) : null}
              </div>
            )}
          </div>
          </>
          )}
        </div>

        {/* ===== RIGHT PANEL: Q&A ===== */}
        <div className={`creator-right-panel${rightPanelCollapsed ? ' collapsed' : ''}`}>
          <div className="creator-panel-header">
            <button type="button" onClick={() => setRightPanelCollapsed(v => !v)} className="creator-panel-toggle-inner">
              <span style={{ display: 'inline-block', transition: 'transform 0.15s', transform: rightPanelCollapsed ? 'rotate(0deg)' : 'rotate(180deg)' }}>›</span>
              {!rightPanelCollapsed && (
                <span style={{ marginLeft: '6px' }}>
                  Q&A{questions.length > 0 ? ` (${questions.length})` : ''}
                  {unreadQuestionIds.length > 0 && <span style={{ marginLeft: '4px', display: 'inline-block', width: '6px', height: '6px', borderRadius: '50%', backgroundColor: '#f44336', verticalAlign: 'middle' }} />}
                </span>
              )}
            </button>
            {!rightPanelCollapsed && debugMode && (
              <button onClick={createMockQuestion} disabled={mockQuestionLoading || loading || !currentSession?.session?.id} style={{ fontSize: '11px', padding: '2px 6px', marginLeft: 'auto', margin: '0 0 0 auto' }}>
                {mockQuestionLoading ? '…' : '🧪'}
              </button>
            )}
          </div>
          {!rightPanelCollapsed && (
            <div className="creator-qa-scroll">
              {questions.length > 0 && (
                <div style={{ padding: '4px 12px', fontSize: '12px', color: '#666', borderBottom: '1px solid #e0e0e0' }}>
                  {questions.length} question{questions.length !== 1 ? 's' : ''}
                </div>
              )}
              {creatorRoots.length === 0 ? (
                <div className="info" style={{ margin: '12px', fontSize: '13px' }}>No questions yet from participants.</div>
              ) : (
                <div>
                  {(() => {
                    const renderList = (roots, byParent, depth = 0) => (roots || []).map((q) => {
                      const isExpanded = questionCardsExpanded[q.id] === true
                      const hasReplies = (byParent[q.id]?.length ?? 0) > 0
                      const showReplies = isExpanded && hasReplies
                      return (
                        <div key={q.id} style={{ marginBottom: depth === 0 ? '8px' : 0 }}>
                          <div style={{
                            marginBottom: '20px',
                            padding: '15px',
                            border: '1px solid #ddd',
                            borderRadius: '5px',
                            backgroundColor: q.answer ? '#f9f9f9' : '#fff',
                            ...(depth > 0 && { marginLeft: 28, borderLeft: '3px solid #90caf9' })
                          }}>
                            <div style={{ display: 'flex', alignItems: 'flex-start', gap: '4px' }}>
                              <button
                                type="button"
                                onClick={() => {
                                  setQuestionCardsExpanded((prev) => {
                                    const next = { ...prev, [q.id]: !prev[q.id] }
                                    if (!prev[q.id] && next[q.id] && markQuestionViewed) {
                                      const sid = currentSession?.session?.id || currentSession?.id
                                      if (sid) markQuestionViewed(sid, q.id)
                                    }
                                    return next
                                  })
                                }}
                                aria-label={isExpanded ? 'Collapse' : 'Expand'}
                                style={{
                                  flexShrink: 0,
                                  marginTop: '2px',
                                  padding: '2px 6px',
                                  fontSize: '12px',
                                  background: 'none',
                                  border: 'none',
                                  cursor: 'pointer',
                                  color: '#666',
                                  margin: 0
                                }}
                              >
                                {isExpanded ? '▼' : '▷'}
                              </button>
                              <div style={{ flex: 1, minWidth: 0 }}>
                                {!isExpanded ? (
                                  <div style={{ display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap' }}>
                                    <span style={{ fontWeight: 'bold', color: '#333' }}>Q: {q.question_text}</span>
                                    {unreadQuestionIds && unreadQuestionIds.includes(String(q.id)) && (
                                      <span style={{ fontSize: '10px', fontWeight: 600, color: '#1976D2', backgroundColor: '#e3f2fd', padding: '2px 6px', borderRadius: '4px' }}>New</span>
                                    )}
                                  </div>
                                ) : (
                                  <>
                                    <div style={{ fontWeight: 'bold', marginBottom: '5px', color: '#333', display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap' }}>
                                      Q: {q.question_text}
                                      {unreadQuestionIds && unreadQuestionIds.includes(String(q.id)) && (
                                        <span style={{ fontSize: '10px', fontWeight: 600, color: '#1976D2', backgroundColor: '#e3f2fd', padding: '2px 6px', borderRadius: '4px' }}>New</span>
                                      )}
                                    </div>
                                    <div style={{ fontSize: '11px', color: '#999', marginTop: '5px', marginBottom: '10px' }}>
                                      Asked: {new Date(q.created_at).toLocaleString()}
                                      {' · asked by '}
                                      {q.asked_by ? <strong>{q.asked_by}</strong> : <span style={{ color: '#999' }}>—</span>}
                                      {q.video_time_seconds !== null && q.video_time_seconds !== undefined && (
                                        <span style={{ marginLeft: '8px', color: '#2196F3', fontWeight: 'bold' }}>
                                          | At {Math.floor(q.video_time_seconds / 60)}:{(q.video_time_seconds % 60).toString().padStart(2, '0')}
                                        </span>
                                      )}
                                    </div>

                                    {q.answer ? (
                                      <div style={{ marginTop: '10px', paddingLeft: '10px', borderLeft: '3px solid #4CAF50' }}>
                                        <div style={{ marginBottom: '5px' }}><strong>A:</strong> {q.answer.answer_text}</div>
                                        <div style={{ fontSize: '12px', color: '#666', marginTop: '5px' }}>
                                          <div style={{ marginBottom: '5px' }}>
                                            From: <span style={{ fontWeight: 'bold' }}>
                                              {q.answer.model && q.answer.model !== 'manual'
                                                ? 'System Generated'
                                                : (q.answer.answered_by_display_name ?? q.answer.answered_by ?? currentSession?.created_by_display_name ?? '—')}
                                            </span>
                                          </div>
                                          <div style={{ display: 'flex', alignItems: 'center', gap: '15px', flexWrap: 'wrap', marginBottom: '5px' }}>
                                            <span>
                                              Status: <span style={{
                                                color: q.answer.answer_status === 'answered' ? '#4CAF50' :
                                                       q.answer.answer_status === 'not_covered' ? '#ff9800' : '#f44336',
                                                fontWeight: 'bold'
                                              }}>{q.answer.answer_status}</span>
                                            </span>
                                            {q.answer.model !== 'manual' && q.answer.confidence !== undefined && q.answer.confidence !== null && (
                                              <span>
                                                Confidence: <span style={{ fontWeight: 'bold' }}>{(q.answer.confidence * 100).toFixed(1)}%</span>
                                              </span>
                                            )}
                                          </div>
                                          <div style={{ marginTop: '8px', display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: '12px' }}>
                                            {q.answer && q.answer.answer_status === 'answered' && q.answer.model !== 'manual' && (
                                              <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: confirmingAnswerId === q.answer.id ? 'wait' : 'pointer' }}>
                                                <input
                                                  type="checkbox"
                                                  checked={q.answer.confirmed || false}
                                                  disabled={confirmingAnswerId === q.answer.id}
                                                  onChange={async (e) => {
                                                    const sessionId = currentSession?.session?.id || currentSession?.id
                                                    const answerId = q.answer?.id
                                                    if (!sessionId || !answerId) return
                                                    const confirmed = e.target.checked
                                                    setConfirmingAnswerId(answerId)
                                                    try {
                                                      const res = await fetch(`${apiBaseUrl}/sessions/${sessionId}/answers/${answerId}/confirm`, {
                                                        method: 'PATCH',
                                                        credentials: 'include',
                                                        headers: { 'Content-Type': 'application/json' },
                                                        body: JSON.stringify({ confirmed })
                                                      })
                                                      if (res.ok && fetchSessionQuestions) fetchSessionQuestions(sessionId)
                                                    } finally {
                                                      setConfirmingAnswerId(null)
                                                    }
                                                  }}
                                                />
                                                <span>
                                                  {q.answer.confirmed ? (
                                                    <span style={{ color: '#2e7d32' }} title="Verified">✓</span>
                                                  ) : (
                                                    <span style={{ color: '#c62828' }} title="Not verified">✕</span>
                                                  )}
                                                  <span style={{ marginLeft: '4px' }}>{q.answer.confirmed ? 'Verified' : 'Verify this answer'}</span>
                                                </span>
                                              </label>
                                            )}
                                            {answeringQuestionId !== q.id && (
                                              <button
                                                type="button"
                                                onClick={() => startAnswering(q.id, q.answer)}
                                                style={{ padding: '6px 12px', fontSize: '13px', fontWeight: 600, color: '#1565c0', backgroundColor: '#e3f2fd', border: '1px solid #2196F3', borderRadius: '4px', cursor: 'pointer', margin: 0 }}
                                              >
                                                {q.answer ? 'Replace answer' : 'Answer'}
                                              </button>
                                            )}
                                          </div>
                                        </div>
                                      </div>
                                    ) : (
                                      <div style={{ marginTop: '10px', padding: '10px', backgroundColor: '#fff3cd', borderRadius: '3px', fontSize: '13px' }}>
                                        No answer yet
                                        {answeringQuestionId !== q.id && (
                                          <button
                                            onClick={() => startAnswering(q.id)}
                                            style={{ marginLeft: '10px', fontSize: '12px', padding: '4px 8px' }}
                                          >
                                            Answer This Question
                                          </button>
                                        )}
                                      </div>
                                    )}

                                    {/* Answer Input Form */}
                                    {answeringQuestionId === q.id ? (
                                      <div style={{ marginTop: '15px', padding: '15px', border: '2px solid #2196F3', borderRadius: '5px', backgroundColor: '#f0f8ff' }}>
                                        <div style={{ fontWeight: 'bold', marginBottom: '10px' }}>
                                          {q.answer ? 'Replace answer' : 'Your answer'}
                                        </div>
                                        <div style={{ display: 'flex', gap: '10px', alignItems: 'center', marginBottom: '10px' }}>
                                          <button
                                            onClick={toggleAnswerVoiceRecording}
                                            disabled={loading || answerSubmitting || answerVoiceUploading}
                                            type="button"
                                            style={{
                                              marginTop: 0,
                                              backgroundColor: answerVoiceRecording ? '#d32f2f' : '#1976D2',
                                              padding: '8px 12px'
                                            }}
                                          >
                                            {answerVoiceRecording ? 'Stop Mic' : (answerVoiceUploading ? 'Processing…' : 'Mic')}
                                          </button>
                                          <div style={{ fontSize: '13px', color: answerVoiceRecording ? '#d32f2f' : '#666' }}>
                                            {answerVoiceRecording ? 'Listening…' : answerVoiceUploading ? 'Processing…' : ''}
                                          </div>
                                        </div>
                                        {answerVoiceFeedback.message && (
                                          <div className={answerVoiceFeedback.type} style={{ marginBottom: '10px' }}>
                                            {answerVoiceFeedback.message}
                                          </div>
                                        )}

                                        {showAnswerVoiceConfirm ? (
                                          <div style={{ marginBottom: '10px' }}>
                                            <div style={{ fontWeight: 600, marginBottom: '8px' }}>Review transcription:</div>
                                            <textarea
                                              value={answerVoiceTranscribedText}
                                              onChange={(e) => setAnswerVoiceTranscribedText(e.target.value)}
                                              rows={3}
                                              style={{ width: '100%', marginBottom: '10px' }}
                                            />
                                            <div style={{ display: 'flex', gap: '10px' }}>
                                              <button
                                                onClick={confirmAnswerVoice}
                                                disabled={!answerVoiceTranscribedText.trim() || loading || answerSubmitting}
                                                style={{ marginTop: 0 }}
                                              >
                                                Confirm & Submit
                                              </button>
                                              <button
                                                onClick={() => { setShowAnswerVoiceConfirm(false); setAnswerVoiceTranscribedText('') }}
                                                disabled={loading || answerSubmitting}
                                                style={{ marginTop: 0, backgroundColor: '#757575' }}
                                              >
                                                Cancel
                                              </button>
                                            </div>
                                          </div>
                                        ) : (
                                          <>
                                            <textarea
                                              value={answerText}
                                              onChange={(e) => setAnswerText(e.target.value)}
                                              placeholder="Type your answer here..."
                                              rows={4}
                                              style={{ width: '100%', marginBottom: '10px' }}
                                            />
                                            <div style={{ marginBottom: '10px' }}>
                                              <label style={{ marginRight: '10px' }}>Status:</label>
                                              <select
                                                value={answerStatus}
                                                onChange={(e) => setAnswerStatus(e.target.value)}
                                                style={{ padding: '4px 8px' }}
                                              >
                                                <option value="answered">Answered</option>
                                                <option value="not_covered">Not Covered</option>
                                                <option value="error">Error</option>
                                              </select>
                                            </div>
                                            <div style={{ display: 'flex', gap: '10px' }}>
                                              <button
                                                onClick={() => submitAnswer(answerText)}
                                                disabled={!answerText.trim() || loading || answerSubmitting}
                                                style={{ marginTop: 0 }}
                                              >
                                                Submit Answer
                                              </button>
                                              <button
                                                onClick={cancelAnswering}
                                                disabled={loading || answerSubmitting}
                                                style={{ marginTop: 0, backgroundColor: '#757575' }}
                                              >
                                                Cancel
                                              </button>
                                            </div>
                                          </>
                                        )}

                                        {answerFeedback.message && (
                                          <div className={answerFeedback.type} style={{ marginTop: '10px' }}>
                                            {answerFeedback.message}
                                          </div>
                                        )}
                                      </div>
                                    ) : null}
                                  </>
                                )}
                              </div>
                            </div>
                          </div>
                          {showReplies && (
                            <div style={{ paddingLeft: '28px', borderLeft: '2px solid #e0e0e0', marginTop: '4px' }}>
                              {renderList(byParent[q.id], byParent, depth + 1)}
                            </div>
                          )}
                        </div>
                      )
                    })
                    return renderList(creatorRoots, creatorByParent, 0)
                  })()}
                </div>
              )}
              {answerFeedback.message && (
                <div className={answerFeedback.type} style={{ margin: '0 12px 12px' }}>
                  {answerFeedback.message}
                </div>
              )}
            </div>
          )}
        </div>

      </div>
    </>
  )
}
