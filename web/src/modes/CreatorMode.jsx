import { useState, useRef, useEffect } from 'react'
import { VideoPlayer, PlayerEvent } from '../VideoPlayer'
import { TranscriptViewer } from '../components/TranscriptViewer'
import { MaterialsList } from '../components/MaterialsList'
import { SessionMaterialsTab } from '../components/SessionMaterialsTab'
import { SessionSharing } from '../components/SessionSharing'

const PROCESSING_STEPS = ['Fetch', 'Download', 'Parse', 'Chunk', 'Embed', 'Ready']

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

export function CreatorMode({
  currentSession,
  refetchSession,
  artifactId,
  setArtifactId,
  videoId,
  setVideoId,
  selectedVideo,
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
  fetchSessionQuestions,
  loading,
  apiBaseUrl,
  creatorIdentity,
  viewMode,
  setViewMode,
  transcriptText,
  setTranscriptText,
  submitTranscript,
  submitTranscriptFeedback
}) {
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

  const startAnswering = (questionId) => {
    setAnsweringQuestionId(questionId)
    setAnswerText('')
    setAnswerStatus('answered')
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
    setLoading(true)

    try {
      const response = await fetch(`${apiBaseUrl}/sessions/${currentSession.session.id}/questions/${answeringQuestionId}/answers`, {
        method: 'POST',
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
      setLoading(false)
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

  const video = selectedVideo || (currentSession?.video_sources && currentSession.video_sources[0])

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
          if (data.state != null && data.state !== '') setProcessingStatus(data)
          else setProcessingStatus(null)
        })
        .catch(() => setProcessingStatus(null))
    }
    fetchProcessing()
    const terminal = processingStatus?.state && ['ready', 'failed_permanent', 'canceled'].includes(processingStatus.state)
    if (terminal) return
    const intervalMs = processingStatus?.state === 'waiting' ? 15000 : 4000
    processingIntervalRef.current = setInterval(fetchProcessing, intervalMs)
    return () => {
      if (processingIntervalRef.current) {
        clearInterval(processingIntervalRef.current)
        processingIntervalRef.current = null
      }
    }
  }, [sessionId, apiBaseUrl, creatorIdentity, processingStatus?.state])

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

  // When Zoom import completes, refetch session once so video_sources and artifacts appear (video player, etc.)
  const hasRefetchedForIngestionReady = useRef(false)
  const readyState = processingStatus?.state === 'ready' ? 'ready' : ingestionStatus?.state === 'ready' ? 'ready' : null
  useEffect(() => {
    if (readyState !== 'ready' || !refetchSession) return
    const hasVideos = currentSession?.video_sources && currentSession.video_sources.length > 0
    if (hasVideos) return
    if (hasRefetchedForIngestionReady.current) return
    hasRefetchedForIngestionReady.current = true
    refetchSession()
  }, [readyState, refetchSession, currentSession?.video_sources])
  // Reset refs when session id changes so a new session can trigger refetch and poll
  const prevSessionIdRef = useRef(sessionId)
  const videoPollAttemptsRef = useRef(0)
  useEffect(() => {
    if (prevSessionIdRef.current !== sessionId) {
      prevSessionIdRef.current = sessionId
      hasRefetchedForIngestionReady.current = false
      videoPollAttemptsRef.current = 0
    }
  }, [sessionId])

  // Poll session until video_sources appear (Zoom import is async; ingestion status may be delayed)
  const VIDEO_POLL_MAX_ATTEMPTS = 20
  const VIDEO_POLL_INTERVAL_MS = 2500
  useEffect(() => {
    if (!sessionId || !apiBaseUrl || !refetchSession) return
    const hasVideos = currentSession?.video_sources && currentSession.video_sources.length > 0
    if (hasVideos) return
    if (videoPollAttemptsRef.current >= VIDEO_POLL_MAX_ATTEMPTS) return
    const t = setInterval(() => {
      videoPollAttemptsRef.current += 1
      if (videoPollAttemptsRef.current > VIDEO_POLL_MAX_ATTEMPTS) return
      refetchSession()
    }, VIDEO_POLL_INTERVAL_MS)
    return () => clearInterval(t)
  }, [sessionId, apiBaseUrl, refetchSession, currentSession?.video_sources?.length])

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

  if (!currentSession) {
    return (
      <div style={{ padding: '20px', color: '#666', textAlign: 'center' }}>
        {loading ? 'Loading session...' : 'No session loaded. Select or create a session to continue.'}
      </div>
    )
  }

  return (
    <>
      {/* Session Header for Creator Mode */}
      {currentSession.session && (
        <div style={{ marginBottom: '20px', padding: '15px', backgroundColor: '#e8f4f8', borderRadius: '5px', border: '2px solid #2196F3' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: '10px' }}>
            <div style={{ flex: 1 }}>
              <h2 style={{ margin: '0 0 10px 0', color: '#1976D2' }}>Session: {currentSession.session.title}</h2>
              <div style={{ fontSize: '14px', color: '#666' }}>
                {currentSession.artifacts && currentSession.artifacts.length > 0 ? (
                  <>
                    <strong>Artifacts:</strong> {currentSession.artifacts.map(a => a.title).join(', ')}
                    <br />
                  </>
                ) : (
                  <div style={{ color: '#999', fontStyle: 'italic' }}>No artifacts in this session yet</div>
                )}
                <strong>Status:</strong> <span style={{ 
                  color: currentSession.session.status === 'open' ? '#4CAF50' : '#999',
                  fontWeight: 'bold'
                }}>{currentSession.session.status}</span>
                {currentSession.session.created_by && ` | Created by: ${currentSession.session.created_by}`}
                <br />
                <strong>Created:</strong> {new Date(currentSession.session.created_at).toLocaleString()}
              </div>
            </div>
            <button 
              onClick={() => { if (setViewMode) setViewMode('artifact'); }} 
              style={{ 
                marginTop: 0,
                backgroundColor: '#757575',
                padding: '8px 16px'
              }}
            >
              ← Back to Artifact View
            </button>
          </div>
        </div>
      )}

      {/* Processing status banner (Mission #4A: stepper + state copy + metadata + actions) */}
      {(processingStatus?.state != null && processingStatus.state !== '') && (() => {
        const runningStates = ['queued', 'fetching', 'downloading', 'parsing', 'chunking', 'embedding']
        const isRunning = runningStates.includes(processingStatus.state)
        const isFailed = processingStatus.state === 'failed_transient' || processingStatus.state === 'failed_permanent'
        const isWaiting = processingStatus.state === 'waiting'
        const effectiveStage = processingStatus.stage || (processingStatus.state && !['ready', 'failed_permanent', 'canceled'].includes(processingStatus.state) ? 'fetch' : null)
        const activeStepIndex = processingStageToStepIndex(effectiveStage)
        return (
          <div style={{
            marginBottom: '20px',
            padding: '16px',
            borderRadius: '8px',
            backgroundColor: processingStatus.state === 'ready' ? '#e8f5e9' : processingStatus.state === 'failed_permanent' ? '#ffebee' : isWaiting ? '#e3f2fd' : '#fff8e1',
            border: processingStatus.state === 'ready' ? '1px solid #4CAF50' : processingStatus.state === 'failed_permanent' ? '1px solid #f44336' : isWaiting ? '1px solid #2196F3' : '1px solid #ff9800'
          }}>
            <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'flex-start', justifyContent: 'space-between', gap: '12px' }}>
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap', marginBottom: '10px' }}>
                  {PROCESSING_STEPS.map((label, idx) => {
                    const completed = idx < activeStepIndex || processingStatus.state === 'ready'
                    const active = idx === activeStepIndex && !completed
                    const future = idx > activeStepIndex
                    const showSpinner = active && isRunning
                    const showWaiting = active && isWaiting
                    const showWarning = active && isFailed
                    return (
                      <div key={label} style={{ display: 'flex', alignItems: 'center', gap: '4px' }}>
                        <span style={{
                          fontSize: '13px',
                          color: completed ? '#4CAF50' : active ? (showWarning ? '#f44336' : '#1976D2') : '#9e9e9e',
                          fontWeight: active ? 600 : 400
                        }}>
                          {completed ? '✓' : showSpinner ? '⏳' : showWaiting ? '⏸' : showWarning ? '⚠' : '○'}
                        </span>
                        <span style={{ color: completed ? '#2e7d32' : active ? '#333' : '#9e9e9e', fontSize: '13px' }}>{label}</span>
                        {idx < PROCESSING_STEPS.length - 1 && <span style={{ marginLeft: '4px', marginRight: '4px', color: '#bdbdbd' }}>→</span>}
                      </div>
                    )
                  })}
                </div>
                <div style={{ fontSize: '14px', fontWeight: 500, marginBottom: '6px' }}>
                  {isRunning
                    ? 'Processing this session…'
                    : isWaiting
                      ? "Waiting for Zoom to finish processing. We'll keep checking."
                      : processingStatus.state === 'failed_transient'
                        ? "Temporary issue. We'll retry automatically. You can retry now."
                        : processingStatus.state === 'failed_permanent'
                          ? `Processing failed. ${processingStatus.last_error_message || processingStatus.last_error_code || 'Unknown error'}. Reconnect Zoom or retry.`
                          : processingStatus.state === 'ready'
                            ? 'Zoom import complete'
                            : processingStatus.state === 'canceled'
                              ? 'Import canceled.'
                              : processingStatus.state}
                </div>
                <div style={{ fontSize: '12px', color: '#666', display: 'flex', flexWrap: 'wrap', gap: '12px' }}>
                  {processingStatus.updated_at && (
                    <span>Last updated: {relativeTime(processingStatus.updated_at)}</span>
                  )}
                  {processingStatus.next_retry_at && (
                    <span>Next retry: {new Date(processingStatus.next_retry_at).toLocaleString()}</span>
                  )}
                </div>
              </div>
              <div style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
                {(processingStatus.state === 'failed_transient' || processingStatus.state === 'failed_permanent' || processingStatus.state === 'waiting') && (
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
                      cursor: processingRetrying ? 'not-allowed' : 'pointer'
                    }}
                  >
                    {processingRetrying ? 'Retrying…' : 'Retry now'}
                  </button>
                )}
                {processingStatus.state === 'failed_permanent' && (processingStatus.last_error_code === 'zoom_auth' || processingStatus.last_error_code === 'zoom_not_connected') && (
                  <a
                    href={`${window.location.origin}${window.location.pathname}?session=${sessionId}&zoom=connect`}
                    style={{ fontSize: '13px', color: '#1976D2', fontWeight: 500 }}
                  >
                    Reconnect Zoom
                  </a>
                )}
              </div>
            </div>
          </div>
        )
      })()}
      {/* Legacy ingestion banner (when no processing job) */}
      {!(processingStatus?.state != null && processingStatus.state !== '') && ingestionStatus?.source === 'zoom' && (
        <div style={{
          marginBottom: '20px',
          padding: '12px 16px',
          borderRadius: '6px',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          flexWrap: 'wrap',
          gap: '10px',
          backgroundColor: ingestionStatus.state === 'ready' ? '#e8f5e9' : ingestionStatus.state === 'failed' ? '#ffebee' : '#fff8e1',
          border: ingestionStatus.state === 'ready' ? '1px solid #4CAF50' : ingestionStatus.state === 'failed' ? '1px solid #f44336' : '1px solid #ff9800'
        }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
            {(ingestionStatus.state === 'queued' || ingestionStatus.state === 'fetching') && (
              <span style={{ fontSize: '18px' }}>⏳</span>
            )}
            <span style={{ fontSize: '14px', fontWeight: 500 }}>
              {ingestionStatus.state === 'queued' || ingestionStatus.state === 'fetching'
                ? 'Import in progress…'
                : ingestionStatus.state === 'ready'
                  ? 'Zoom import complete'
                  : ingestionStatus.state === 'failed'
                    ? (ingestionStatus.last_error || 'Import failed')
                    : ''}
            </span>
            {ingestionStatus.updated_at && (
              <span style={{ fontSize: '12px', color: '#666' }}>
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
                padding: '6px 12px',
                fontSize: '13px',
                backgroundColor: '#2196F3',
                color: 'white',
                border: 'none',
                borderRadius: '4px',
                cursor: ingestionRetrying ? 'not-allowed' : 'pointer'
              }}
            >
              {ingestionRetrying ? 'Retrying…' : 'Retry'}
            </button>
          )}
        </div>
      )}

      {/* Transcript tab (Mission #2: session transcript status + segments) */}
      {sessionId && (
        <div className="section" style={{ marginBottom: '20px', backgroundColor: '#fafafa', border: '1px solid #e0e0e0', borderRadius: '8px', padding: '16px' }}>
          <h2 style={{ marginTop: 0 }}>Transcript</h2>
          {!transcriptData ? (
            <div style={{ color: '#666' }}>Loading…</div>
          ) : transcriptData.status === 'none' ? (
            <div style={{ color: '#666', fontStyle: 'italic' }}>No transcript yet. Import a Zoom recording to add one.</div>
          ) : transcriptData.status === 'parsing' ? (
            <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
              <span style={{ fontSize: '18px' }}>⏳</span>
              <span>Parsing transcript…</span>
            </div>
          ) : transcriptData.status === 'failed' ? (
            <div>
              <div style={{ color: '#c62828', marginBottom: '10px' }}>
                {transcriptData.error_message || 'Transcript failed.'}
              </div>
              {(processingStatus?.state != null && processingStatus.state !== '') ? (
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
                    cursor: processingRetrying ? 'not-allowed' : 'pointer'
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
                    padding: '8px 16px',
                    fontSize: '14px',
                    backgroundColor: '#2196F3',
                    color: 'white',
                    border: 'none',
                    borderRadius: '4px',
                    cursor: ingestionRetrying ? 'not-allowed' : 'pointer'
                  }}
                >
                  {ingestionRetrying ? 'Retrying…' : 'Retry import'}
                </button>
              ) : null}
            </div>
          ) : transcriptData.status === 'ready' && transcriptData.segments?.length > 0 ? (
            <div style={{ maxHeight: '400px', overflow: 'auto' }}>
              {transcriptData.segments.map((seg) => {
                const mmSs = (ms) => {
                  const m = Math.floor(ms / 60000)
                  const s = Math.floor((ms % 60000) / 1000)
                  return `${m}:${s.toString().padStart(2, '0')}`
                }
                return (
                  <div key={seg.idx} style={{ marginBottom: '12px', padding: '8px 0', borderBottom: '1px solid #eee' }}>
                    <span style={{ fontSize: '12px', color: '#666', marginRight: '8px' }}>
                      {mmSs(seg.start_ms)}–{mmSs(seg.end_ms)}
                    </span>
                    <span style={{ fontSize: '14px' }}>{seg.text}</span>
                  </div>
                )
              })}
            </div>
          ) : transcriptData.status === 'ready' ? (
            <div style={{ color: '#666', fontStyle: 'italic' }}>Transcript ready (no segments).</div>
          ) : null}
        </div>
      )}

      {/* Mode Indicator */}
      <div style={{ 
        marginBottom: '20px', 
        padding: '12px 20px', 
        backgroundColor: '#e3f2fd', 
        borderRadius: '5px', 
        border: '2px solid #2196F3',
        display: 'flex',
        alignItems: 'center',
        gap: '10px'
      }}>
        <span style={{ 
          backgroundColor: '#2196F3', 
          color: 'white', 
          padding: '4px 12px', 
          borderRadius: '4px',
          fontWeight: 'bold',
          fontSize: '14px'
        }}>
          CREATOR MODE
        </span>
        <span style={{ color: '#1976D2', fontSize: '14px' }}>
          You can upload and configure session content
        </span>
      </div>

      {/* Session Sharing */}
      {currentSession?.session && (
        <SessionSharing 
          sessionId={currentSession.session.id} 
          sessionTitle={currentSession.session.title}
          apiBaseUrl={apiBaseUrl}
        />
      )}

      {/* Existing Artifacts */}
      {currentSession?.artifacts && currentSession.artifacts.length > 0 && (
        <div className="section" style={{ marginBottom: '20px', backgroundColor: '#f0f8ff', border: '1px solid #2196F3' }}>
          <h2>Existing Artifacts ({currentSession.artifacts.length})</h2>
          {currentSession.artifacts.map((artifact, idx) => (
            <div key={artifact.id || idx} style={{ 
              marginBottom: '15px', 
              padding: '15px', 
              border: '1px solid #ddd', 
              borderRadius: '5px',
              backgroundColor: artifact.id === artifactId ? '#e3f2fd' : '#fff'
            }}>
              <div style={{ fontWeight: 'bold', fontSize: '16px', marginBottom: '5px' }}>
                {artifact.title}
              </div>
              {artifact.description && (
                <div style={{ fontSize: '14px', color: '#666', marginBottom: '10px' }}>
                  {artifact.description}
                </div>
              )}
              <div style={{ fontSize: '12px', color: '#999' }}>
                ID: <code>{artifact.id}</code> | 
                Status: <span style={{ 
                  color: artifact.status === 'ready' ? '#4CAF50' : '#999',
                  fontWeight: 'bold'
                }}>{artifact.status}</span>
                {artifact.id === artifactId && (
                  <span style={{ marginLeft: '10px', color: '#2196F3', fontWeight: 'bold' }}>
                    (Currently Selected)
                  </span>
                )}
              </div>
              {artifact.id !== artifactId && (
                <button 
                  onClick={() => setArtifactId(artifact.id)}
                  style={{ marginTop: '10px', fontSize: '12px', padding: '5px 10px' }}
                >
                  Select This Artifact
                </button>
              )}
            </div>
          ))}
        </div>
      )}

      {/* Materials tab (Mission #6): upload, paste, list, preview, delete */}
      {sessionId && (
        <SessionMaterialsTab
          sessionId={sessionId}
          materials={currentSession?.materials || []}
          videoSources={currentSession?.video_sources || []}
          apiBaseUrl={apiBaseUrl}
          refetchSession={refetchSession}
        />
      )}

      {/* Existing Materials (artifact view legacy list) */}
      {currentSession?.materials && currentSession.materials.length > 0 && (
        <MaterialsList
          materials={currentSession.materials}
          sessionId={sessionId}
          apiBaseUrl={apiBaseUrl}
          refetchSession={refetchSession}
        />
      )}

      {/* Video Player */}
      {currentSession?.video_sources && currentSession.video_sources.length > 0 && (
        <div className="section" style={{ marginBottom: '20px', backgroundColor: '#fff3e0', border: '1px solid #ff9800' }}>
          <h2>Video Player</h2>
          
          {currentSession.video_sources.length > 1 && (
            <div style={{ marginBottom: '15px' }}>
              <label style={{ fontWeight: 'bold', marginRight: '10px' }}>Select Video:</label>
              <select 
                value={selectedVideo?.id || currentSession.video_sources[0].id}
                onChange={(e) => {
                  const video = currentSession.video_sources.find(v => v.id === e.target.value)
                  setSelectedVideo(video)
                  setVideoId(video.id)
                  setCurrentVideoTime(0)
                  setIsVideoPlaying(false)
                }}
                style={{ padding: '5px 10px', fontSize: '14px' }}
              >
                {currentSession.video_sources.map((video, idx) => {
                  const statusLabel = video.transcript_status === 'missing' ? 'No transcript' :
                                      video.transcript_status === 'pending' ? 'Pending...' :
                                      video.transcript_status === 'processing' ? 'Processing...' :
                                      video.transcript_status === 'ready' ? 'Ready' :
                                      video.transcript_status === 'failed' ? 'Failed' :
                                      video.transcript_status || 'Unknown'
                  
                  // Determine source type label
                  const sourceTypeLabel = video.source_type === 'upload' ? 'Uploaded' :
                                         video.source_type === 'direct_url' ? 'Direct URL' :
                                         video.source_type === 'embed_url' ? 'Embed URL' :
                                         'Unknown'
                  return (
                    <option key={video.id} value={video.id}>
                      Video {idx + 1} - {video.provider} ({statusLabel})
                    </option>
                  )
                })}
              </select>
            </div>
          )}

          {video && (
            <div>
              <div style={{ marginBottom: '10px' }}>
                <strong>Provider:</strong> <span style={{ textTransform: 'capitalize' }}>{video.provider}</span>
                {' | '}
                <strong>Mode:</strong> <span style={{ 
                  color: video.playback_mode === 'direct' ? '#4CAF50' : '#ff9800',
                  fontWeight: 'bold'
                }}>
                  {video.playback_mode === 'direct' ? 'Direct (Full Control)' : 'Embed (Limited Control)'}
                </span>
                {' | '}
                <strong>Transcript Status:</strong>{' '}
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
                  {video.source_type && (
                    <span style={{ marginLeft: '8px', fontSize: '11px', color: '#666' }}>
                      ({video.source_type === 'upload' ? 'Uploaded' :
                        video.source_type === 'direct_url' ? 'Direct URL' :
                        video.source_type === 'embed_url' ? 'Embed URL' :
                        'Unknown'})
                    </span>
                  )}
                  {video.transcript_status === 'failed' && video.failure_reason && (
                    <div style={{ marginTop: '5px', fontSize: '11px', color: '#f44336', fontStyle: 'italic' }}>
                      Error: {video.failure_reason}
                    </div>
                  )}
                </span>
                {transcriptJobs[video.id] && (
                  <>
                    {' | '}
                    <strong>Job Status:</strong>{' '}
                    <span style={{ 
                      color: transcriptJobs[video.id].status === 'completed' ? '#4CAF50' : 
                             transcriptJobs[video.id].status === 'failed' ? '#f44336' : '#ff9800',
                      fontWeight: 'bold'
                    }}>
                      {transcriptJobs[video.id].status}
                    </span>
                    {transcriptJobs[video.id].error_message && (
                      <span style={{ color: '#f44336', fontSize: '12px', marginLeft: '10px' }}>
                        ({transcriptJobs[video.id].error_message})
                      </span>
                    )}
                  </>
                )}
                {video.provider === 'loom' && (
                  <>
                    {' | '}
                    <button 
                      onClick={() => regenerateTranscript(video.id)}
                      disabled={loading}
                      style={{ 
                        padding: '4px 8px',
                        fontSize: '12px',
                        backgroundColor: '#2196F3',
                        color: 'white',
                        border: 'none',
                        borderRadius: '4px',
                        cursor: loading ? 'not-allowed' : 'pointer'
                      }}
                    >
                      🔄 Regenerate Transcript
                    </button>
                  </>
                )}
              </div>

              <VideoPlayer
                video={video}
                onEvent={handleVideoPlayerEvent}
                onTimeUpdate={handleVideoTimeUpdate}
                currentTime={currentVideoTime}
                playing={isVideoPlaying}
                sessionId={currentSession?.session?.id || currentSession?.id}
                apiBaseUrl={apiBaseUrl}
                creatorIdentity={creatorIdentity}
              />

              {video.transcript_text && (
                <TranscriptViewer transcriptText={video.transcript_text} />
              )}
            </div>
          )}
        </div>
      )}

      <h2>Submit Transcript</h2>
      <div className="section">
        <div className="form-group">
          <label>Paste transcript text (for an existing video):</label>
          <textarea
            value={transcriptText}
            onChange={(e) => setTranscriptText(e.target.value)}
            placeholder="Paste transcript text here…"
            rows={10}
          />
        </div>
        <button onClick={submitTranscript} disabled={!artifactId || !videoId || !transcriptText || loading}>
          Submit Transcript
        </button>
        {submitTranscriptFeedback.message && (
          <div className={submitTranscriptFeedback.type} style={{ marginTop: '10px' }}>
            {submitTranscriptFeedback.message}
          </div>
        )}
      </div>

      {/* Q&A History with Answer Input */}
      <div className="section">
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '10px', flexWrap: 'wrap', gap: '10px' }}>
          <h2 style={{ margin: 0 }}>Participant Questions</h2>
          <div style={{ display: 'flex', gap: '10px', alignItems: 'center', flexWrap: 'wrap' }}>
            <span style={{ fontSize: '12px', color: '#666', fontStyle: 'italic' }}>
              Real-time updates via WebSocket
            </span>
            <button 
              onClick={createMockQuestion} 
              disabled={mockQuestionLoading || loading || !currentSession?.session?.id}
              style={{ 
                backgroundColor: (mockQuestionLoading || loading || !currentSession?.session?.id) ? '#ccc' : '#9c27b0', 
                color: 'white',
                padding: '8px 16px',
                borderRadius: '4px',
                border: 'none',
                cursor: (mockQuestionLoading || loading || !currentSession?.session?.id) ? 'not-allowed' : 'pointer',
                fontWeight: 'bold',
                fontSize: '14px',
                boxShadow: (mockQuestionLoading || loading || !currentSession?.session?.id) ? 'none' : '0 2px 4px rgba(0,0,0,0.2)',
                transition: 'all 0.2s'
              }}
              title={!currentSession?.session?.id ? 'Please select a session first' : 'Create a mock question to test WebSocket functionality (not persisted to database)'}
            >
              {mockQuestionLoading ? 'Creating...' : '🧪 Test WebSocket'}
            </button>
            <button 
              onClick={() => currentSession?.session?.id && fetchSessionQuestions(currentSession.session.id)} 
              disabled={loading || !currentSession?.session?.id}
              style={{
                opacity: (loading || !currentSession?.session?.id) ? 0.6 : 1
              }}
            >
              Refresh Now
            </button>
          </div>
        </div>
        {(questions.length > 0) && (
          <div style={{ marginBottom: '10px', fontSize: '14px', color: '#666' }}>
            {questions.length} question{questions.length !== 1 ? 's' : ''} from participants
          </div>
        )}
        
        {questions.length === 0 ? (
          <div className="info">No questions yet from participants.</div>
        ) : (
          <div>
            {questions.map((q) => (
              <div key={q.id} style={{ 
                marginBottom: '20px', 
                padding: '15px', 
                border: '1px solid #ddd', 
                borderRadius: '5px',
                backgroundColor: q.answer ? '#f9f9f9' : '#fff'
              }}>
                <div style={{ fontWeight: 'bold', marginBottom: '5px', color: '#333' }}>
                  Q: {q.question_text}
                </div>
                <div style={{ fontSize: '11px', color: '#999', marginTop: '5px', marginBottom: '10px' }}>
                  Asked: {new Date(q.created_at).toLocaleString()}
                  {q.video_time_seconds !== null && q.video_time_seconds !== undefined && (
                    <span style={{ marginLeft: '10px', color: '#2196F3', fontWeight: 'bold' }}>
                      | At {Math.floor(q.video_time_seconds / 60)}:{(q.video_time_seconds % 60).toString().padStart(2, '0')}
                    </span>
                  )}
                  {q.asked_by && (
                    <span style={{ marginLeft: '10px', color: '#666' }}>
                      | By: {q.asked_by}
                    </span>
                  )}
                </div>
                
                {q.answer ? (
                  <div style={{ marginTop: '10px', paddingLeft: '10px', borderLeft: '3px solid #4CAF50' }}>
                    <div style={{ marginBottom: '5px' }}><strong>A:</strong> {q.answer.answer_text}</div>
                    <div style={{ fontSize: '12px', color: '#666', marginTop: '5px' }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: '15px', flexWrap: 'wrap', marginBottom: '5px' }}>
                        <span>
                          Status: <span style={{ 
                            color: q.answer.answer_status === 'answered' ? '#4CAF50' : 
                                   q.answer.answer_status === 'not_covered' ? '#ff9800' : '#f44336',
                            fontWeight: 'bold'
                          }}>{q.answer.answer_status}</span>
                        </span>
                        {q.answer.confidence !== undefined && q.answer.confidence !== null && (
                          <span>
                            Confidence: <span style={{ fontWeight: 'bold' }}>{(q.answer.confidence * 100).toFixed(1)}%</span>
                          </span>
                        )}
                      </div>
                      {q.answer && q.answer.answer_status === 'answered' && (
                        <div style={{ marginTop: '8px', padding: '8px', backgroundColor: '#f0f8ff', borderRadius: '4px', border: '1px solid #2196F3' }}>
                          <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: confirmingAnswerId === q.answer.id ? 'wait' : 'pointer', userSelect: 'none' }}>
                            <input
                              type="checkbox"
                              checked={q.answer.confirmed || false}
                              disabled={confirmingAnswerId === q.answer.id}
                              onChange={async (e) => {
                                // Try multiple ways to get session ID
                                let sessionId = currentSession?.session?.id || currentSession?.id
                                
                                // If still no session ID, try to get it from the question
                                if (!sessionId && q.session_id) {
                                  sessionId = q.session_id
                                }
                                
                                if (!sessionId) {
                                  console.error('No session ID available for confirmation', { currentSession, question: q })
                                  alert('Session ID not found. Please ensure you are viewing a session. The checkbox will still work if you have a session selected.')
                                  e.target.checked = !e.target.checked // Revert checkbox
                                  return
                                }
                                
                                const answerId = q.answer?.id
                                if (!answerId) {
                                  console.error('No answer ID found', { question: q })
                                  alert('Answer ID not found.')
                                  e.target.checked = !e.target.checked // Revert checkbox
                                  return
                                }
                                
                                const confirmed = e.target.checked
                                
                                console.log('Updating answer confirmation:', { answerId, sessionId, confirmed, apiBaseUrl })
                                
                                if (!apiBaseUrl) {
                                  console.error('API base URL not set')
                                  alert('API URL not configured. Please check your settings.')
                                  e.target.checked = !e.target.checked // Revert checkbox
                                  return
                                }
                                
                                setConfirmingAnswerId(answerId)
                                try {
                                  const url = `${apiBaseUrl}/sessions/${sessionId}/answers/${answerId}/confirm`
                                  console.log('Calling API:', url, { method: 'PATCH', body: { confirmed } })
                                  
                                  const response = await fetch(url, {
                                    method: 'PATCH',
                                    headers: { 'Content-Type': 'application/json' },
                                    body: JSON.stringify({ confirmed })
                                  })
                                  
                                  console.log('Response status:', response.status, response.statusText)
                                  
                                  if (!response.ok) {
                                    const text = await response.text()
                                    console.error('Failed to update answer confirmation:', { status: response.status, statusText: response.statusText, body: text })
                                    alert(`Failed to update confirmation (${response.status}): ${text || response.statusText}`)
                                    // Revert checkbox on error
                                    e.target.checked = !confirmed
                                    return
                                  }
                                  
                                  const updatedAnswer = await response.json()
                                  console.log('Answer confirmation updated successfully:', updatedAnswer)
                                  
                                  // WebSocket will update the UI, but we can also refresh questions
                                  if (fetchSessionQuestions) {
                                    fetchSessionQuestions(sessionId)
                                  }
                                } catch (err) {
                                  console.error('Error updating answer confirmation:', err)
                                  const errorMsg = err.message || (err instanceof TypeError && err.message.includes('fetch') ? 'Network error - check if the API server is running' : 'Unknown error')
                                  alert(`Error updating confirmation: ${errorMsg}`)
                                  // Revert checkbox on error
                                  e.target.checked = !confirmed
                                } finally {
                                  setConfirmingAnswerId(null)
                                }
                              }}
                              style={{ 
                                cursor: confirmingAnswerId === q.answer.id ? 'wait' : 'pointer',
                                width: '18px',
                                height: '18px',
                                margin: 0
                              }}
                            />
                            <span style={{ 
                              fontSize: '13px', 
                              color: q.answer.confirmed ? '#4CAF50' : '#2196F3', 
                              fontWeight: q.answer.confirmed ? 'bold' : 'normal' 
                            }}>
                              {q.answer.confirmed ? '✓ Confirmed by Creator' : 'Confirm this answer'}
                            </span>
                          </label>
                        </div>
                      )}
                    </div>
                    {answeringQuestionId === q.id && (
                      <button 
                        onClick={() => startAnswering(q.id)}
                        style={{ marginTop: '10px', fontSize: '12px', padding: '4px 8px' }}
                      >
                        Edit Answer
                      </button>
                    )}
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
                {answeringQuestionId === q.id && (
                  <div style={{ marginTop: '15px', padding: '15px', border: '2px solid #2196F3', borderRadius: '5px', backgroundColor: '#f0f8ff' }}>
                    <div style={{ fontWeight: 'bold', marginBottom: '10px' }}>Your Answer:</div>
                    
                    <div style={{ display: 'flex', gap: '10px', alignItems: 'center', marginBottom: '10px' }}>
                      <button
                        onClick={toggleAnswerVoiceRecording}
                        disabled={loading || answerVoiceUploading}
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
                            disabled={!answerVoiceTranscribedText.trim() || loading}
                            style={{ marginTop: 0 }}
                          >
                            Confirm & Submit
                          </button>
                          <button
                            onClick={() => { setShowAnswerVoiceConfirm(false); setAnswerVoiceTranscribedText('') }}
                            disabled={loading}
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
                            disabled={!answerText.trim() || loading}
                            style={{ marginTop: 0 }}
                          >
                            Submit Answer
                          </button>
                          <button
                            onClick={cancelAnswering}
                            disabled={loading}
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
                )}
              </div>
            ))}
          </div>
        )}
      </div>
    </>
  )
}
