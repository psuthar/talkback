import { useState, useRef, useEffect, useCallback, useMemo } from 'react'
import { VideoPlayer, PlayerEvent } from '../VideoPlayer'
import { TranscriptViewer } from '../components/TranscriptViewer'
import { MaterialsTreePanel, MaterialsPanelHeader } from '../components/MaterialsTreePanel'
import { DocumentViewer } from '../components/DocumentViewer'
import { PrimaryStage } from '../components/PrimaryStage'
import { resolvePrimaryAutoSelection } from '../components/sessionPrimaryAutoSelect'
import { AddContentSection } from '../components/AddContentSection'
import { ParticipantSessionMenu } from '../components/ParticipantSessionMenu'
import { OrchestrationRecActions } from '../components/OrchestrationRecActions'
import { TrashIcon } from '../components/icons/TrashIcon'
import { OrchestrationRecCard } from '../components/OrchestrationRecCard'
import { OrchestrationRecGroup } from '../components/OrchestrationRecGroup'
import { OrchestrationPanelHeader } from '../components/OrchestrationPanelHeader'
import orchestrationActionsStyles from '../components/OrchestrationRecActions.module.css'
import {
  groupRecommendations,
  getOrderedGroupTypes,
  getDefaultExpandedType,
  shouldRenderUngrouped,
} from '../utils/orchestrationGrouping'
import { DecisionBriefHeader } from '../components/DecisionBriefHeader'
import { roleLabel } from '../utils/roleLabels'
import { getPrimaryRecording, getRecordings } from '../utils/session'
import { MemberRowActions } from '../components/MemberRowActions'
import { DecisionBar } from '../components/DecisionBar'
import { isValidEmailFormat } from '../utils/inviteMailto'
import { buildCanonicalSessionUrl } from '../sessionNavigation'
import {
  getStoredContextExpanded,
  setStoredContextExpanded,
  getStoredMembersExpanded,
  setStoredMembersExpanded,
  getStoredMaterialsTreeExpanded,
  setStoredMaterialsTreeExpanded,
} from '../utils/participantStorage'
import {
  sessionMaterialsCount,
  resolveInitialExpanded,
} from '../utils/sessionSidebar'
import { googleMeetWaitingCopy, googleMeetShouldShowReconnect, googleMeetTerminalErrorState, googleMeetWaitingStepIndex } from '../googleMeetMessages'
import { ORCHESTRATION_AUTO_REFRESH_DEBOUNCE_MS } from '../constants/orchestrationAutoRefresh'
import { SessionSkeleton } from '../components/SessionSkeleton'
import topbarStyles from './ParticipantMode.module.css'

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

/** SCRUM-21: decision_readiness card with evaluator metadata readiness=inputs_complete. */
function isDecisionReadinessInputsComplete(rec) {
  const readiness = rec?.metadata_json?.readiness
  return rec?.recommendation_type === 'decision_readiness' && readiness === 'inputs_complete'
}

export function getOrchestrationEmptyStateMessage(currentSession, questions) {
  const qCount = Array.isArray(questions) ? questions.length : 0
  const mats = Array.isArray(currentSession?.materials) ? currentSession.materials.length : 0
  const links = Array.isArray(currentSession?.links) ? currentSession.links.length : 0
  const vids = Array.isArray(currentSession?.video_sources) ? currentSession.video_sources.length : 0
  if (qCount === 0 && mats === 0 && links === 0 && vids === 0) {
    return 'Add transcript or materials, then collect participant questions to get AI suggested next actions.'
  }
  return 'No recommendations right now.'
}

export function getOrchestrationLoadFeedback(status, data) {
  if (status === 404) {
    return { type: 'info', message: 'Suggestions will appear after this session has initial content.' }
  }
  return { type: 'error', message: data?.error || data?.message || `Failed to load recommendations (${status})` }
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
  sessionProcessingReadyVersion = 0,
  sessionUpdatedVersion = 0,
  stanceVersion = 0,
  orchestrationRefreshTrigger = 0,
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
  questions,
  unreadQuestionIds = [],
  markQuestionViewed,
  fetchSessionQuestions,
  canDeleteQuestions = false,
  requestDeleteQuestion,
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
  onLogout,
  debugMode = false,
  setDebugMode,
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
  const [orchestrationRecommendations, setOrchestrationRecommendations] = useState([])
  const [orchestrationLoading, setOrchestrationLoading] = useState(false)
  const [orchestrationFeedback, setOrchestrationFeedback] = useState({ type: '', message: '' })
  const [orchestrationActioningId, setOrchestrationActioningId] = useState(null)
  /** SCRUM-193: per-group expansion overrides keyed by recommendation_type. Empty
   * object means "use defaults" (highest-priority type expanded, others collapsed). */
  const [orchestrationGroupExpansions, setOrchestrationGroupExpansions] = useState({})
  /** SCRUM-21: inline record-outcome UI for decision_readiness / inputs_complete */
  const [recordOutcomeRecId, setRecordOutcomeRecId] = useState(null)
  const [recordOutcomeText, setRecordOutcomeText] = useState('')
  const [recordOutcomeError, setRecordOutcomeError] = useState('')
  const [auditOpen, setAuditOpen] = useState(false)
  const [auditEntries, setAuditEntries] = useState([])
  const [auditLoading, setAuditLoading] = useState(false)
  const orchestrationAutoDebounceRef = useRef(null)
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

  const [membersPanelExpanded, setMembersPanelExpandedState] = useState(false)
  const [contextPanelExpanded, setContextPanelExpandedState] = useState(false)
  const [materialsTreeExpanded, setMaterialsTreeExpandedState] = useState(true)
  const setContextPanelExpanded = useCallback((value) => {
    const next = typeof value === 'function' ? value(contextPanelExpanded) : value
    setContextPanelExpandedState(next)
    if (currentSession?.session?.id) setStoredContextExpanded(currentSession.session.id, next)
  }, [contextPanelExpanded, currentSession?.session?.id])
  const setMembersPanelExpanded = useCallback((value) => {
    const next = typeof value === 'function' ? value(membersPanelExpanded) : value
    setMembersPanelExpandedState(next)
    if (currentSession?.session?.id) setStoredMembersExpanded(currentSession.session.id, next)
  }, [membersPanelExpanded, currentSession?.session?.id])
  const setMaterialsTreeExpanded = useCallback((value) => {
    const next = typeof value === 'function' ? value(materialsTreeExpanded) : value
    setMaterialsTreeExpandedState(next)
    if (currentSession?.session?.id) setStoredMaterialsTreeExpanded(currentSession.session.id, next)
  }, [materialsTreeExpanded, currentSession?.session?.id])
  // Load per-section collapse preferences from storage when the active session
  // changes. Context and Members honor the narrow-viewport override (force-
  // collapse below 1024px); the Materials sub-collapse always honors storage.
  useEffect(() => {
    const sid = currentSession?.session?.id
    if (!sid) return
    setContextPanelExpandedState(resolveInitialExpanded({
      stored: getStoredContextExpanded(sid),
      defaultExpanded: false,
      honorNarrowOverride: true,
      win: typeof window !== 'undefined' ? window : null,
    }))
    setMembersPanelExpandedState(resolveInitialExpanded({
      stored: getStoredMembersExpanded(sid),
      defaultExpanded: false,
      honorNarrowOverride: true,
      win: typeof window !== 'undefined' ? window : null,
    }))
    setMaterialsTreeExpandedState(resolveInitialExpanded({
      stored: getStoredMaterialsTreeExpanded(sid),
      defaultExpanded: true,
      honorNarrowOverride: false,
      win: typeof window !== 'undefined' ? window : null,
    }))
  }, [currentSession?.session?.id])
  const [contextPremise, setContextPremise] = useState('')
  const [contextDecision, setContextDecision] = useState('')
  const [contextOutcome, setContextOutcome] = useState('')
  const [contextSaving, setContextSaving] = useState(false)
  const [contextFeedback, setContextFeedback] = useState({ type: '', message: '' })
  const [stanceData, setStanceData] = useState(null)
  const [stanceRationale, setStanceRationale] = useState('')
  const [stanceSubmitting, setStanceSubmitting] = useState(false)
  const [stanceFeedback, setStanceFeedback] = useState({ type: '', message: '' })
  const [leftPanelCollapsed, setLeftPanelCollapsed] = useState(false)
  const [rightPanelCollapsed, setRightPanelCollapsed] = useState(false)
  const [selectedDocument, setSelectedDocument] = useState(null)
  const [selectedDocumentId, setSelectedDocumentId] = useState(null)
  // SCRUM-327 / SCRUM-328: Once the user explicitly switches the center pane
  // to a video (clicking a video row), two things must not happen:
  //   (1) the primary-material auto-select effect must not later restore the
  //       primary document/link on a session refetch (SCRUM-327), and
  //   (2) PrimaryStage's fall-back-to-session-primary must not silently render
  //       the primary document instead of the chosen video (SCRUM-328).
  // Both checks read the same flag so user intent is honored consistently.
  const [userSelectedVideo, setUserSelectedVideo] = useState(false)

  // Reset the guard when switching sessions so the next session's primary-
  // material auto-select still fires.
  useEffect(() => {
    setUserSelectedVideo(false)
  }, [currentSession?.session?.id])

  // SCRUM-276: when GET session reports an explicit document/link primary,
  // default the creator's center pane to that material on session load —
  // mirrors the SCRUM-274 effect already running in ParticipantMode so both
  // creator and participant see the same default. Skips if the user has
  // already chosen something else (selectedDocumentId truthy).
  useEffect(() => {
    if (!currentSession?.session?.id) return
    if (userSelectedVideo) return // SCRUM-327: respect user's explicit video selection
    const decision = resolvePrimaryAutoSelection(currentSession, selectedDocumentId)
    if (!decision) return
    if (decision.type === 'material') {
      setSelectedDocument(decision.material)
      setSelectedDocumentId(decision.material.id)
      return
    }
    if (decision.type === 'link') {
      const link = decision.link
      setSelectedDocument({
        type: 'link',
        url: link.url,
        title: link.title || link.url,
        id: link.id,
      })
      setSelectedDocumentId(`link-${link.id}`)
    }
  }, [currentSession?.session?.id, currentSession?.primary?.kind, currentSession?.primary?.id, userSelectedVideo])

  const handleSelectDocument = (doc) => {
    setSelectedDocument(doc)
    setSelectedDocumentId(doc?.id ?? doc?.transcriptId ?? (doc?.type === 'link' && doc?.id ? `link-${doc.id}` : null))
    setUserSelectedVideo(false) // SCRUM-328: user picked a doc, drop the video-intent flag
  }
  const handleBackToVideo = () => {
    // SCRUM-327 / SCRUM-328: mark the user's explicit video choice so neither
    // the auto-select effect nor PrimaryStage falls back to the primary doc.
    setUserSelectedVideo(true)
    setSelectedDocument(null)
    setSelectedDocumentId(null)
  }
  const handleSelectVideo = (v) => {
    // SCRUM-327 / SCRUM-328: mark the user's explicit video choice so neither
    // the auto-select effect nor PrimaryStage falls back to the primary doc.
    setUserSelectedVideo(true)
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

  const loadOrchestrationRecommendations = useCallback(async (sessionId, { sync = false, silent = false } = {}) => {
    if (!sessionId) return
    if (!silent) {
      setOrchestrationLoading(true)
      setOrchestrationFeedback({ type: '', message: '' })
    }
    try {
      const path = sync
        ? `${apiBaseUrl}/api/sessions/${sessionId}/orchestration/recommendations/sync`
        : `${apiBaseUrl}/api/sessions/${sessionId}/orchestration/recommendations`
      const res = await fetch(path, {
        method: sync ? 'POST' : 'GET',
        credentials: 'include'
      })
      const data = await res.json().catch(() => ({}))
      if (!res.ok) {
        setOrchestrationRecommendations([])
        setOrchestrationFeedback(getOrchestrationLoadFeedback(res.status, data))
        return
      }
      setOrchestrationRecommendations(Array.isArray(data?.recommendations) ? data.recommendations : [])
      if (sync && !silent) {
        setOrchestrationFeedback({ type: 'success', message: 'Recommendations refreshed.' })
      }
    } catch (err) {
      setOrchestrationFeedback({ type: 'error', message: `Failed to load recommendations: ${err.message}` })
    } finally {
      if (!silent) {
        setOrchestrationLoading(false)
      }
    }
  }, [apiBaseUrl])

  const loadOrchestrationAudit = useCallback(async (sessionId, { force = false } = {}) => {
    if (!sessionId) return
    if (!force && auditEntries.length > 0) return
    setAuditLoading(true)
    try {
      const res = await fetch(`${apiBaseUrl}/api/sessions/${sessionId}/orchestration/recommendations/audit?limit=20`, {
        credentials: 'include'
      })
      const data = await res.json().catch(() => ({}))
      if (res.ok) {
        setAuditEntries(Array.isArray(data?.status_audit) ? data.status_audit : [])
      }
    } catch (_) {
      // silently fail — audit is non-critical
    } finally {
      setAuditLoading(false)
    }
  }, [apiBaseUrl, auditEntries.length])

  const updateRecommendationStatus = useCallback(async (sessionId, recommendationId, status) => {
    if (!sessionId || !recommendationId || !status) return
    setOrchestrationActioningId(recommendationId)
    try {
      const res = await fetch(`${apiBaseUrl}/api/sessions/${sessionId}/orchestration/recommendations/${recommendationId}`, {
        method: 'PATCH',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ status })
      })
      const data = await res.json().catch(() => ({}))
      if (!res.ok) {
        setOrchestrationFeedback({ type: 'error', message: data?.error || data?.message || `Failed to update recommendation (${res.status})` })
        return
      }
      setOrchestrationRecommendations(prev => prev.map(rec => rec.id === recommendationId ? { ...rec, status } : rec))
      setOrchestrationFeedback({ type: 'success', message: `Recommendation marked ${status}.` })
    } catch (err) {
      setOrchestrationFeedback({ type: 'error', message: `Failed to update recommendation: ${err.message}` })
    } finally {
      setOrchestrationActioningId(null)
    }
  }, [apiBaseUrl])

  const recommendationEvidenceId = (rec, field) => {
    const evid = Array.isArray(rec?.evidence) ? rec.evidence : []
    for (const e of evid) {
      if (e && e[field]) return String(e[field])
    }
    return null
  }

  const approveDraftAnswerFromRecommendation = useCallback(async (sessionId, rec) => {
    const answerId = recommendationEvidenceId(rec, 'answer_id')
    if (!sessionId || !answerId) {
      setOrchestrationFeedback({ type: 'error', message: 'No draft answer found on recommendation evidence.' })
      return
    }
    setOrchestrationActioningId(rec.id)
    try {
      const res = await fetch(`${apiBaseUrl}/sessions/${sessionId}/answers/${answerId}/confirm`, {
        method: 'PATCH',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ confirmed: true })
      })
      if (!res.ok) {
        const text = await res.text()
        setOrchestrationFeedback({ type: 'error', message: `Failed to approve draft: ${text || res.status}` })
        return
      }
      await updateRecommendationStatus(sessionId, rec.id, 'approved')
      if (fetchSessionQuestions) await fetchSessionQuestions(sessionId)
    } catch (err) {
      setOrchestrationFeedback({ type: 'error', message: `Failed to approve draft: ${err.message}` })
    } finally {
      setOrchestrationActioningId(null)
    }
  }, [apiBaseUrl, fetchSessionQuestions, updateRecommendationStatus])

  const dismissDraftAnswerFromRecommendation = useCallback(async (sessionId, rec) => {
    const answerId = recommendationEvidenceId(rec, 'answer_id')
    if (!sessionId || !answerId) {
      setOrchestrationFeedback({ type: 'error', message: 'No draft answer found on recommendation evidence.' })
      return
    }
    setOrchestrationActioningId(rec.id)
    try {
      const res = await fetch(`${apiBaseUrl}/api/sessions/${sessionId}/orchestration/draft-answers/${answerId}`, {
        method: 'DELETE',
        credentials: 'include'
      })
      if (!res.ok) {
        const text = await res.text()
        setOrchestrationFeedback({ type: 'error', message: `Failed to dismiss draft: ${text || res.status}` })
        return
      }
      await updateRecommendationStatus(sessionId, rec.id, 'dismissed')
      if (fetchSessionQuestions) await fetchSessionQuestions(sessionId)
    } catch (err) {
      setOrchestrationFeedback({ type: 'error', message: `Failed to dismiss draft: ${err.message}` })
    } finally {
      setOrchestrationActioningId(null)
    }
  }, [apiBaseUrl, fetchSessionQuestions, updateRecommendationStatus])

  const generateDraftForRecommendation = useCallback(async (sessionId, rec) => {
    const questionId = recommendationEvidenceId(rec, 'question_id')
    if (!sessionId || !questionId) {
      setOrchestrationFeedback({ type: 'error', message: 'No question found on recommendation evidence.' })
      return
    }
    setOrchestrationActioningId(rec.id)
    try {
      const res = await fetch(`${apiBaseUrl}/api/sessions/${sessionId}/orchestration/draft-answers`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ question_id: questionId })
      })
      if (!res.ok) {
        const text = await res.text()
        let detail = text || String(res.status)
        try {
          const j = text ? JSON.parse(text) : null
          if (j && typeof j.error === 'string') detail = j.error
        } catch {
          /* keep raw text */
        }
        setOrchestrationFeedback({ type: 'error', message: `Failed to generate draft: ${detail}` })
        return
      }
      setOrchestrationFeedback({ type: 'success', message: 'Draft answer generated.' })
      if (fetchSessionQuestions) await fetchSessionQuestions(sessionId)
      await loadOrchestrationRecommendations(sessionId, { sync: true })
    } catch (err) {
      setOrchestrationFeedback({ type: 'error', message: `Failed to generate draft: ${err.message}` })
    } finally {
      setOrchestrationActioningId(null)
    }
  }, [apiBaseUrl, fetchSessionQuestions, loadOrchestrationRecommendations])

  const saveDecisionOutcomeFromOrchestration = useCallback(async (sessionId, rec, outcomeText) => {
    const trimmed = (outcomeText || '').trim()
    if (!sessionId || !rec?.id) return
    if (!trimmed) {
      setRecordOutcomeError('Enter a decision outcome before saving.')
      return
    }
    setRecordOutcomeError('')
    setOrchestrationActioningId(rec.id)
    const base = (apiBaseUrl || '').replace(/\/$/, '')
    try {
      const patchRes = await fetch(`${base}/api/sessions/${sessionId}`, {
        method: 'PATCH',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ decision_outcome: trimmed }),
      })
      const patchRaw = await patchRes.text()
      if (!patchRes.ok) {
        let msg = patchRaw || `HTTP ${patchRes.status}`
        try {
          const j = JSON.parse(patchRaw)
          if (j && typeof j.error === 'string') msg = j.error
        } catch {
          /* keep raw */
        }
        setRecordOutcomeError(msg)
        return
      }
      const statusRes = await fetch(`${base}/api/sessions/${sessionId}/orchestration/recommendations/${rec.id}`, {
        method: 'PATCH',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ status: 'completed' }),
      })
      const statusData = await statusRes.json().catch(() => ({}))
      if (!statusRes.ok) {
        setRecordOutcomeError(
          statusData?.error ||
            statusData?.message ||
            `Could not update recommendation (${statusRes.status}). Outcome was saved — try Refresh.`,
        )
        if (refetchSession) await refetchSession()
        return
      }
      if (refetchSession) await refetchSession()
      setOrchestrationRecommendations((prev) => prev.filter((r) => r.id !== rec.id))
      setRecordOutcomeRecId(null)
      setRecordOutcomeText('')
      setOrchestrationFeedback({ type: 'success', message: 'Decision outcome recorded.' })
    } catch (err) {
      setRecordOutcomeError(err?.message || 'Request failed')
    } finally {
      setOrchestrationActioningId(null)
    }
  }, [apiBaseUrl, refetchSession])

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
  // R2-synthetic-video transcript metadata: pull from the primary recording.
  // Pre-SCRUM-405 this read `video_sources[0]` directly; identical for
  // single-recording sessions, and follows the canonical primary for multi-
  // recording sessions.
  const firstVideoSource = getPrimaryRecording(currentSession)
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
  const sources = getRecordings(currentSession)
  const primary = getPrimaryRecording(currentSession)
  const primarySourceId = primary?.id
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

  useEffect(() => {
    const sid = currentSession?.session?.id
    if (!sid) return
    loadOrchestrationRecommendations(sid)
  }, [currentSession?.session?.id, loadOrchestrationRecommendations])

  // SCRUM-16: debounced sync when App bumps orchestrationRefreshTrigger on session WebSocket events
  useEffect(() => {
    if (!orchestrationRefreshTrigger) return
    const sid = currentSession?.session?.id
    if (!sid) return
    if (orchestrationAutoDebounceRef.current) clearTimeout(orchestrationAutoDebounceRef.current)
    orchestrationAutoDebounceRef.current = setTimeout(() => {
      orchestrationAutoDebounceRef.current = null
      loadOrchestrationRecommendations(sid, { sync: true, silent: true })
    }, ORCHESTRATION_AUTO_REFRESH_DEBOUNCE_MS)
    return () => {
      if (orchestrationAutoDebounceRef.current) {
        clearTimeout(orchestrationAutoDebounceRef.current)
        orchestrationAutoDebounceRef.current = null
      }
    }
  }, [orchestrationRefreshTrigger, currentSession?.session?.id, loadOrchestrationRecommendations])

  // Determine if we have a valid session - only show participant link when session exists
  const hasValidSession = currentSession && (
    (currentSession.session && currentSession.session.id) || 
    currentSession.id
  )
  const sessionId = hasValidSession 
    ? (currentSession?.session?.id || currentSession?.id)
    : null
  const participantUrl = sessionId
    ? buildCanonicalSessionUrl(sessionId, {
        mode: 'view',
        ...(apiBaseUrl ? { api: apiBaseUrl } : {})
      })
    : null

  const sessionSource = (currentSession?.session?.source_provider || '').toLowerCase() // zoom | teams | upload | ''

  // Processing status (Mission #4: GET /api/sessions/:id/processing) — preferred over legacy ingestion
  const [processingStatus, setProcessingStatus] = useState(null) // { state, stage, attempt_count, next_retry_at, last_error_code, last_error_message, updated_at }
  const [processingRetrying, setProcessingRetrying] = useState(false)
  const processingIntervalRef = useRef(null)
  // Once we've seen a processing job for this session, keep the progression panel visible until video is on screen (so it never "disappears" mid-import)
  const hasSeenProcessingJobRef = useRef(false)
  // Stop polling the processing endpoint if no job exists after several attempts
  const processingNoJobCountRef = useRef(0)

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
    if (!sessionId || apiBaseUrl == null) return
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
            processingNoJobCountRef.current = 0
            setProcessingStatus(data)
            hasSeenProcessingJobRef.current = true
            console.log('[ProgressPanel] processing fetch: state=', data.state, 'stage=', data.stage)
          } else {
            processingNoJobCountRef.current += 1
            console.log('[ProgressPanel] processing fetch: empty state, keeping previous (data.state=', data.state, ')')
            // Stop polling if no job has ever existed for this session (e.g. PPTX-only, no video import)
            if (!hasSeenProcessingJobRef.current && processingNoJobCountRef.current >= 5) {
              if (processingIntervalRef.current) {
                clearInterval(processingIntervalRef.current)
                processingIntervalRef.current = null
              }
            }
          }
        })
        .catch((err) => {
          console.log('[ProgressPanel] processing fetch failed, keeping previous:', err?.message || err)
        })
    }
    fetchProcessing()
    const terminal = processingStatus?.state && ['ready', 'failed_permanent', 'canceled'].includes(processingStatus.state)
    if (terminal) return
    const intervalMs = (processingStatus?.state === 'waiting' || processingStatus?.state === 'awaiting_whisper' || processingStatus?.state === 'failed_transient') ? 15000 : 2000
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

  const processingStatusRef = useRef(processingStatus)
  useEffect(() => {
    processingStatusRef.current = processingStatus
  }, [processingStatus])

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
  // SCRUM-398: setJobWaiting doesn't bump the stage column when the Meet
  // pipeline transitions from download → transcript-wait or awaiting_whisper.
  // The stage strip shows "Download" frozen while the user can already play
  // the video. Compensate by deriving an additional Meet-specific step index
  // from state + last_error_code; max() with the stage/state pair ensures the
  // strip never regresses for non-Meet jobs.
  const meetWaitingStepIndex = hasJobForStep
    ? googleMeetWaitingStepIndex(processingStatus?.state, processingStatus?.last_error_code)
    : null
  const targetStepIndex = readyButNoVideoYetForStep
    ? PROCESSING_STEPS.length - 1
    : Math.max(stageIndexForStep, stateIndexForStep, meetWaitingStepIndex ?? 0)
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
  const [videoPollExhausted, setVideoPollExhausted] = useState(false)
  // Also show panel for error/waiting/whisper states so panel does not disappear and reappear during retries (which looks like cycling).
  const isNonTerminalPanel = hasJobForPanel && (
    processingStatus.state === 'waiting' ||
    processingStatus.state === 'awaiting_whisper' ||
    processingStatus.state === 'failed_transient' ||
    processingStatus.state === 'failed_permanent'
  )
  const processingInProgress = isRunningForPanel || (readyButNoVideoYetForPanel && !videoPollExhausted) || isNonTerminalPanel
  // Show panel while processing, in error/waiting states, or preparing playback; hide only once session is ready with video
  const showPanel = processingInProgress

  // Legacy ingestion status (fallback when no processing job)
  const [ingestionStatus, setIngestionStatus] = useState(null)
  const [ingestionRetrying, setIngestionRetrying] = useState(false)
  const ingestionIntervalRef = useRef(null)
  const ingestionNoJobCountRef = useRef(0)
  useEffect(() => {
    if (!sessionId || apiBaseUrl == null || (processingStatus?.state != null && processingStatus.state !== '')) return
    ingestionNoJobCountRef.current = 0
    const fetchIngestion = () => {
      fetch(`${apiBaseUrl}/api/sessions/${sessionId}/ingestion`, {
        headers: { 'X-Creator-Identity': creatorIdentity }
      })
        .then((r) => r.json())
        .then((data) => {
          if (data.source && data.state) {
            ingestionNoJobCountRef.current = 0
            setIngestionStatus(data)
            if ((data.state === 'ready' || data.state === 'failed') && ingestionIntervalRef.current) {
              clearInterval(ingestionIntervalRef.current)
              ingestionIntervalRef.current = null
            }
          } else {
            setIngestionStatus(null)
            ingestionNoJobCountRef.current += 1
            // No Zoom ingestion job — stop polling after 5 empty responses
            if (ingestionNoJobCountRef.current >= 5 && ingestionIntervalRef.current) {
              clearInterval(ingestionIntervalRef.current)
              ingestionIntervalRef.current = null
            }
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
      setVideoPollExhausted(false)
    }
  }, [sessionId])

  // Poll session until we have a playable primary video (video_access_url) or video_sources — backend may set primary_video_artifact_id before file is Ready, so keep refetching until video_access_url appears.
  // Guard: only run when we've seen an import job (Zoom/Teams). PPTX-only sessions never have video, so polling is pointless and causes an endless request loop.
  // Use a ref for refetchSession to avoid restarting the effect (and resetting the interval) on every render.
  const VIDEO_POLL_MAX_ATTEMPTS = 20
  const VIDEO_POLL_INTERVAL_MS = 2500
  const refetchSessionRef = useRef(refetchSession)
  useEffect(() => { refetchSessionRef.current = refetchSession }, [refetchSession])
  useEffect(() => {
    if (!sessionId || apiBaseUrl == null) return
    if (hasPlayableVideo) {
      setVideoPollExhausted(false)
      return
    }
    // Only poll for video when an import job has been seen; PPTX/document-only sessions never have video
    if (!hasSeenProcessingJobRef.current && !ingestionStatus?.state) return
    if (videoPollAttemptsRef.current >= VIDEO_POLL_MAX_ATTEMPTS) {
      setVideoPollExhausted(true)
      return
    }
    const t = setInterval(() => {
      videoPollAttemptsRef.current += 1
      if (refetchSessionRef.current) refetchSessionRef.current()
      if (videoPollAttemptsRef.current >= VIDEO_POLL_MAX_ATTEMPTS) {
        setVideoPollExhausted(true)
        clearInterval(t)
      }
    }, VIDEO_POLL_INTERVAL_MS)
    return () => clearInterval(t)
  }, [sessionId, apiBaseUrl, hasPlayableVideo, ingestionStatus?.state])

  // Session transcript (Mission #2: GET /api/sessions/:id/transcript)
  const [transcriptData, setTranscriptData] = useState(null) // { status, source, updated_at, error_message, segments }
  const transcriptIntervalRef = useRef(null)
  useEffect(() => {
    if (!sessionId || apiBaseUrl == null) return
    setTranscriptData(null) // reset so we show Loading when session changes; avoids stale transcript
    const stopTranscriptPoll = () => {
      if (transcriptIntervalRef.current) {
        clearInterval(transcriptIntervalRef.current)
        transcriptIntervalRef.current = null
      }
    }
    const fetchTranscript = () => {
      fetch(`${apiBaseUrl}/api/sessions/${sessionId}/transcript`)
        .then((r) => r.json())
        .then((data) => {
          setTranscriptData(data)
          const ps = processingStatusRef.current?.state
          if (data.status === 'ready' || data.status === 'failed') {
            stopTranscriptPoll()
            return
          }
          // Avoid infinite polling: stop when no transcript and no active import job
          const processingTerminalOrAbsent = !ps || ps === '' || ps === 'ready' || ps === 'failed_permanent' || ps === 'canceled'
          if (data.status === 'none' && processingTerminalOrAbsent) {
            stopTranscriptPoll()
          }
        })
        .catch(() => setTranscriptData(null))
    }
    fetchTranscript()
    transcriptIntervalRef.current = setInterval(fetchTranscript, 2500)
    return () => {
      stopTranscriptPoll()
    }
  }, [sessionId, apiBaseUrl, readyState])
  // readyState in deps: when import completes (state -> 'ready'), effect re-runs and refetches transcript

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
    if (!sessionId || apiBaseUrl == null || !file) return
    setMaterialUploading(true)
    setMaterialUploadFeedback({ type: '', message: '' })
    const base = (apiBaseUrl || '').replace(/\/$/, '')
    try {
      const form = new FormData()
      form.append('file', file)
      const res = await fetch(`${base}/sessions/${sessionId}/materials/upload`, { method: 'POST', body: form, credentials: 'include' })
      if (!res.ok) {
        // SCRUM-334: structured 413 for spreadsheet-too-large uploads.
        if (res.status === 413) {
          try {
            const body = await res.clone().json()
            if (body && body.error === 'spreadsheet_too_large') {
              const fmt = (n) => n == null ? '' : n < 1024 * 1024 ? `${(n / 1024).toFixed(1)} KB` : `${(n / (1024 * 1024)).toFixed(1)} MB`
              setMaterialUploadFeedback({
                type: 'error',
                message: `Spreadsheet too large (max ${fmt(body.max_bytes)}; this file is ${fmt(body.actual_bytes)}).`,
              })
              return
            }
          } catch (_) { /* fall through */ }
        }
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
    if (!sessionId || !materialId || apiBaseUrl == null) return
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

  // SCRUM-296: link deletion from the materials tree sidebar. Reuses the
  // existing deletingMaterialId/deleteMaterialError state, namespacing link
  // IDs as `link-<id>` so they cannot collide with material IDs.
  const deleteLink = async (linkId) => {
    if (!sessionId || !linkId || apiBaseUrl == null) return
    setDeleteMaterialError(null)
    setDeletingMaterialId(`link-${linkId}`)
    try {
      const base = (apiBaseUrl || '').replace(/\/$/, '')
      const res = await fetch(`${base}/api/sessions/${sessionId}/links/${linkId}`, { method: 'DELETE', credentials: 'include' })
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
    if (!currentSession?.session?.id || apiBaseUrl == null) return
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
    if (!currentSession?.session?.id || stanceSubmitting || apiBaseUrl == null) return
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
    if (loading) return <SessionSkeleton />
    return (
      <div style={{ padding: '20px', color: '#666', textAlign: 'center' }}>
        No session loaded. Select or create a session to continue.
      </div>
    )
  }

  return (
    <>
      {/* Topbar — mirrors ParticipantMode topbar (SCRUM-186). */}
      <div className="creator-layout-topbar">
        <div className={topbarStyles.topbarInner}>
          {currentSession?.session && (
            <>
              <h2 className={topbarStyles.sessionTitle}>
                {currentSession.session.title}
              </h2>
              <span
                className={topbarStyles.roleBadge}
                style={{ backgroundColor: 'var(--color-success)' }}
              >
                Creator
              </span>
            </>
          )}
        </div>
        <ParticipantSessionMenu
          authUser={authUser}
          onShowAllSessions={onClearSession}
          participantViewHref={participantUrl}
          onLogout={onLogout}
          debugMode={debugMode}
          setDebugMode={setDebugMode}
        />
      </div>

      {/* Premise / Decision / Outcome — shared with participant view (SCRUM-187). */}
      {currentSession?.session && (
        <DecisionBriefHeader
          premise={currentSession.session.premise}
          decision={currentSession.session.primary_decision}
          decisionOutcome={currentSession.session.decision_outcome}
          readiness={stanceData?.readiness}
          onCloseDecision={async (outcomeText) => {
            const sid = currentSession?.session?.id
            if (!sid || apiBaseUrl == null) return { ok: false, error: 'Session not loaded' }
            const base = (apiBaseUrl || '').replace(/\/$/, '')
            try {
              const res = await fetch(`${base}/api/sessions/${sid}`, {
                method: 'PATCH',
                credentials: 'include',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ decision_outcome: outcomeText })
              })
              if (!res.ok) {
                let msg = `HTTP ${res.status}`
                try {
                  const j = await res.json()
                  if (j?.error) msg = j.error
                } catch { /* ignore */ }
                return { ok: false, error: msg }
              }
              if (refetchSession) await refetchSession()
              await fetchStances()
              return { ok: true }
            } catch (err) {
              return { ok: false, error: err?.message || 'Network error' }
            }
          }}
        />
      )}

      {/* Stance picker + others' counts — shared with participant view (SCRUM-188). */}
      {currentSession?.session && (
        <DecisionBar
          placement="top"
          primaryDecision={currentSession.session.primary_decision}
          decisionOutcome={currentSession.session.decision_outcome}
          myStance={stanceData?.my_stance}
          stanceAggregate={stanceData?.aggregate}
          stanceResponses={stanceData?.responses}
          sessionInvitations={sessionInvitations}
          stanceRationale={stanceRationale}
          setStanceRationale={setStanceRationale}
          stanceSubmitting={stanceSubmitting}
          stanceFeedback={stanceFeedback}
          submitStance={submitStance}
          clearStance={clearStance}
        />
      )}

      {/* Processing progression: flex-shrink 0, sits above grid */}
      {showPanel && (() => {
        const runningStates = ['queued', 'fetching', 'downloading', 'parsing', 'chunking', 'embedding']
        const hasJob = processingStatus?.state != null && processingStatus.state !== ''
        const isRunning = hasJob && runningStates.includes(processingStatus.state)
        const isFailed = hasJob && (processingStatus.state === 'failed_transient' || processingStatus.state === 'failed_permanent')
        const isWaiting = hasJob && processingStatus.state === 'waiting'
        const isAwaitingWhisper = hasJob && processingStatus.state === 'awaiting_whisper'
        const readyButNoVideoYet = hasJob && processingStatus.state === 'ready' && !hasPrimaryR2Video
        const allComplete = hasJob && processingStatus.state === 'ready' && hasPrimaryR2Video
        const activeStepIndex = displayStepIndex
        return (
          <div style={{
            flexShrink: 0,
            padding: '12px 16px',
            borderRadius: 0,
            borderBottom: '1px solid #e0e0e0',
            backgroundColor: allComplete ? 'var(--color-success-bg)' : hasJob && processingStatus.state === 'failed_permanent' ? '#ffebee' : (isWaiting || isAwaitingWhisper) ? 'var(--color-primary-bg)' : '#fff8e1',
            borderLeft: allComplete ? '3px solid #4CAF50' : hasJob && processingStatus.state === 'failed_permanent' ? '3px solid #f44336' : (isWaiting || isAwaitingWhisper) ? '3px solid #2196F3' : '3px solid #ff9800'
          }}>
            <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'flex-start', justifyContent: 'space-between', gap: '10px' }}>
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: '4px', flexWrap: 'wrap', marginBottom: '6px', fontSize: '13px' }}>
                  {PROCESSING_STEPS.map((label, idx) => {
                    const completed = idx < activeStepIndex || allComplete
                    const active = idx === activeStepIndex && !completed
                    const showSpinner = active && (isRunning || !hasJob || readyButNoVideoYet || displayStepIndex < lastKnownTargetStepRef.current)
                    const showWaiting = active && (isWaiting || isAwaitingWhisper)
                    const showWarning = active && isFailed
                    const activeWeight = 700
                    return (
                      <div key={label} style={{ display: 'flex', alignItems: 'center', gap: '3px' }}>
                        <span style={{
                          fontSize: '13px',
                          color: completed ? 'var(--color-success-mid)' : active ? (showWarning ? 'var(--color-danger)' : 'var(--color-primary)') : '#9e9e9e',
                          fontWeight: active ? activeWeight : 400
                        }}>
                          {completed ? '✓' : showSpinner ? '⌛' : showWaiting ? '⏸' : showWarning ? '⚠' : '○'}
                        </span>
                        <span style={{ color: completed ? 'var(--color-success)' : active ? '#1a1a1a' : '#9e9e9e', fontSize: '13px', fontWeight: active ? activeWeight : 400 }}>{label}</span>
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
                      : isAwaitingWhisper
                        ? "Video downloaded — transcribing audio. This may take several minutes."
                        : isWaiting
                        ? (googleMeetWaitingCopy(sessionSource, processingStatus.last_error_code, processingStatus.state)
                          || (sessionSource === 'teams'
                            ? "Waiting for Microsoft Teams or your tenant to finish processing. We'll keep checking."
                            : "Waiting for the recording provider to finish processing. We'll keep checking."))
                        : processingStatus.state === 'failed_transient'
                          ? `Temporary issue${processingStatus.last_error_code ? ` [${processingStatus.last_error_code}]` : ''}${processingStatus.last_error_message ? ': ' + processingStatus.last_error_message : ''}. We'll retry automatically.`
                          : processingStatus.state === 'failed_permanent'
                            ? (googleMeetTerminalErrorState(processingStatus.last_error_code)?.copy
                              || `Processing failed. ${processingStatus.last_error_message || processingStatus.last_error_code || 'Unknown error'}. Reconnect your account or retry.`)
                            : readyButNoVideoYet
                              ? (videoPollExhausted
                                ? 'Video URL did not appear after several tries. Refresh the page or retry import.'
                                : 'Preparing playback — video will appear on screen shortly…')
                              : processingStatus.state === 'ready'
                                ? 'Import complete.'
                                : processingStatus.state === 'canceled'
                                  ? 'Import canceled.'
                                  : processingStatus.state}
                </div>
              </div>
              <div style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
                {hasJob && (processingStatus.state === 'failed_transient' || processingStatus.state === 'failed_permanent' || processingStatus.state === 'waiting') && !googleMeetTerminalErrorState(processingStatus.last_error_code) && (
                  <button
                    type="button"
                    onClick={retryProcessing}
                    disabled={processingRetrying}
                    style={{
                      padding: '4px 10px',
                      fontSize: '12px',
                      backgroundColor: 'var(--color-primary-mid)',
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
                    href={buildCanonicalSessionUrl(sessionId, { mode: 'edit', zoom: 'connect' })}
                    style={{ fontSize: '12px', color: 'var(--color-primary)', fontWeight: 500 }}
                  >
                    Reconnect Zoom
                  </a>
                )}
                {hasJob && processingStatus.state === 'failed_permanent' && googleMeetShouldShowReconnect(processingStatus.last_error_code) && (
                  <a
                    href={buildCanonicalSessionUrl(sessionId, { mode: 'edit', google_meet: 'connect' })}
                    style={{ fontSize: '12px', color: 'var(--color-primary)', fontWeight: 500 }}
                  >
                    Reconnect Google Meet
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
          backgroundColor: ingestionStatus.state === 'ready' ? 'var(--color-success-bg)' : ingestionStatus.state === 'failed' ? '#ffebee' : '#fff8e1',
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
                padding: '4px 10px',
                fontSize: '12px',
                backgroundColor: 'var(--color-primary-mid)',
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

              {/* SCRUM-188: legacy "Decisions" sidebar block removed; stance entry + counts now live in the top DecisionBar. */}

              {/* Context: Premise, Decision, Outcome */}
              <div className="session-sidebar-block" data-testid="creator-sidebar-context">
                <button
                  type="button"
                  onClick={() => setContextPanelExpanded(e => !e)}
                  className="session-sidebar-block__toggle"
                  aria-expanded={contextPanelExpanded}
                  aria-controls="creator-sidebar-context-region"
                  data-testid="creator-context-toggle"
                >
                  <span style={{ fontSize: '12px', color: '#555' }} aria-hidden>{contextPanelExpanded ? '▼' : '▷'}</span>
                  {' '}<strong>Context</strong>
                </button>
                {contextPanelExpanded && (
                  <div id="creator-sidebar-context-region" style={{ marginTop: '8px' }}>
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
                        <span style={{ fontSize: '12px', color: contextFeedback.type === 'error' ? 'var(--color-danger-dark)' : 'var(--color-success)' }}>{contextFeedback.message}</span>
                      )}
                    </div>
                  </div>
                )}
              </div>

              {/* Members — prominent at top */}
              {authUser && inviteUserToSession && (
                <div className="session-sidebar-block" data-testid="creator-sidebar-members">
                  <button
                    type="button"
                    onClick={() => setMembersPanelExpanded(e => !e)}
                    className="session-sidebar-block__toggle"
                    aria-expanded={membersPanelExpanded}
                    aria-controls="creator-sidebar-members-region"
                    data-testid="creator-members-toggle"
                  >
                    <span style={{ fontSize: '12px', color: '#555' }} aria-hidden>{membersPanelExpanded ? '▼' : '▷'}</span>
                    {' '}<strong>Members</strong>{sessionInvitations?.length > 0 ? ` (${sessionInvitations.length})` : ''}
                  </button>
                  {membersPanelExpanded && (
                    <div id="creator-sidebar-members-region" style={{ marginTop: '8px' }}>
                      <div style={{ display: 'flex', gap: '6px', alignItems: 'center', flexWrap: 'wrap', marginBottom: '6px' }}>
                        <input type="email" value={inviteEmail ?? ''} onChange={e => setInviteEmail?.(e.target.value)} placeholder="user@example.com" style={{ flex: '1', minWidth: '120px', padding: '4px 8px', fontSize: '12px', border: '1px solid #ddd', borderRadius: '3px' }} />
                        <select aria-label="Invite role" value={inviteRole ?? 'participant'} onChange={e => setInviteRole?.(e.target.value)} style={{ padding: '4px 6px', fontSize: '12px', border: '1px solid #ddd', borderRadius: '3px' }}>
                          <option value="participant">Participant</option>
                          <option value="creator">Creator</option>
                          <option value="decision_maker">Decision Maker</option>
                        </select>
                        <button type="button" onClick={inviteUserToSession} disabled={!inviteEmail?.trim() || !isValidEmailFormat(inviteEmail?.trim()) || inviteLoading} style={{ padding: '4px 10px', fontSize: '12px', margin: 0 }}>
                          {inviteLoading ? '…' : 'Invite'}
                        </button>
                      </div>
                      {inviteFeedback?.message && (
                        <div className={inviteFeedback.type} style={{ fontSize: '12px', padding: '4px 6px', marginBottom: '6px' }}>{inviteFeedback.message}</div>
                      )}
                      {sessionInvitations?.length > 0 && (
                        <div style={{ fontSize: '12px', overflowX: 'auto' }}>
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
                                  <td style={{ padding: '4px 6px' }}>{roleLabel(inv.invited_role)}</td>
                                  <td style={{ padding: '4px 6px' }}>{inv.status}</td>
                                  {typeof fetchSessionInvitations === 'function' && (
                                    <td style={{ padding: '4px 6px' }}>
                                      <MemberRowActions
                                        invitation={inv}
                                        apiBaseUrl={apiBaseUrl}
                                        currentUserEmail={authUser?.email}
                                        onFeedback={(fb) => {
                                          if (typeof setInviteFeedback === 'function') {
                                            setInviteFeedback(fb)
                                            const ttl = fb?.type === 'success' ? 3000 : 6000
                                            setTimeout(() => setInviteFeedback({ type: '', message: '' }), ttl)
                                          }
                                        }}
                                        onChanged={(data) => {
                                          if (data?.invitation && typeof setLastInvitationDraft === 'function') {
                                            setLastInvitationDraft(data.invitation)
                                          }
                                          fetchSessionInvitations(currentSession?.session?.id ?? currentSession?.id)
                                          if (refetchSession) refetchSession()
                                        }}
                                        onChangeRole={async (userId, newRole) => {
                                          const sid = currentSession?.session?.id ?? currentSession?.id
                                          if (!sid || apiBaseUrl == null) return { ok: false, error: 'Session not loaded' }
                                          const base = (apiBaseUrl || '').replace(/\/$/, '')
                                          try {
                                            const res = await fetch(`${base}/api/sessions/${sid}/memberships/${userId}`, {
                                              method: 'PATCH',
                                              credentials: 'include',
                                              headers: { 'Content-Type': 'application/json' },
                                              body: JSON.stringify({ role: newRole }),
                                            })
                                            if (!res.ok) {
                                              let msg = `HTTP ${res.status}`
                                              try {
                                                const j = await res.json()
                                                if (j?.error) msg = j.error
                                              } catch { /* ignore */ }
                                              return { ok: false, error: msg }
                                            }
                                            return { ok: true }
                                          } catch (err) {
                                            return { ok: false, error: err?.message || 'Network error' }
                                          }
                                        }}
                                      />
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

              {/* Hidden file input for material uploads. Kept outside the
                  Materials sub-collapse so the ref stays mounted and any
                  programmatic .click() trigger still works. */}
              {sessionId && (
                <input
                  ref={materialFileInputRef}
                  type="file"
                  accept=".pdf,.txt,.md,.docx,.xlsx,.xls,.csv,.pptx,.jpg,.jpeg,.png,.gif,.webp,.bmp,.svg,video/mp4,.mp4,application/pdf,text/plain,text/csv,application/vnd.openxmlformats-officedocument.wordprocessingml.document,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,application/vnd.ms-excel,application/vnd.openxmlformats-officedocument.presentationml.presentation,image/jpeg,image/png,image/gif,image/webp,image/bmp,image/svg+xml"
                  onChange={handleMaterialFileChange}
                  disabled={materialUploading}
                  style={{ display: 'none' }}
                />
              )}

              {/* Materials sub-header. Sibling of Context/Members; toggles only
                  the tree + add-content region, not the column-level collapse. */}
              <div className="session-sidebar-block" data-testid="creator-sidebar-materials">
                <button
                  type="button"
                  onClick={() => setMaterialsTreeExpanded(e => !e)}
                  className="session-sidebar-block__toggle"
                  aria-expanded={materialsTreeExpanded}
                  aria-controls="creator-sidebar-materials-region"
                  data-testid="creator-materials-tree-toggle"
                >
                  <span style={{ fontSize: '12px', color: '#555' }} aria-hidden>{materialsTreeExpanded ? '▼' : '▷'}</span>
                  {' '}<strong>Materials</strong>
                  {(() => {
                    const n = sessionMaterialsCount(currentSession)
                    return n > 0 ? ` (${n})` : ''
                  })()}
                </button>
              </div>

              {materialsTreeExpanded && (
                <div id="creator-sidebar-materials-region">
                  {/* Add content at top: upload files + add links */}
                  {sessionId && (
                    <AddContentSection
                      sessionId={sessionId}
                      apiBaseUrl={apiBaseUrl}
                      refetchSession={refetchSession}
                      onUploadClick={() => materialFileInputRef.current?.click()}
                      uploading={materialUploading}
                      uploadFeedback={materialUploadFeedback}
                      defaultExpanded={false}
                    />
                  )}

                  {/* Materials tree (no duplicate header; sub-header above owns it) */}
                  <MaterialsTreePanel
                    session={currentSession}
                    apiBaseUrl={apiBaseUrl}
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
                    onDeleteVideo={deleteMaterial}
                    onDeleteLink={deleteLink}
                    deletingId={deletingMaterialId}
                    deleteError={deleteMaterialError}
                    /* SCRUM-275: pass the resolved primary descriptor so the
                       panel can badge the current row, and refetch the session
                       when the creator changes the primary so the participant
                       side (and the local center pane) pick up the new value. */
                    currentPrimary={currentSession?.primary || null}
                    onPrimaryChanged={() => refetchSession?.()}
                  />
                </div>
              )}

            </div>
          )}
        </div>

        {/* ===== CENTER PANEL ===== */}
        <div className="creator-center-panel">
          {/* SCRUM-273: PrimaryStage owns the document-vs-video center-pane
             rendering. Behavior-neutral; ParticipantMode adopts this in
             SCRUM-274. The transcript + scrollable content below stays in
             CreatorMode because it is creator-mode-specific (depends on
             session-level transcript state, processing UI). */}
          <PrimaryStage
            selectedDocument={selectedDocument}
            userSelectedVideo={userSelectedVideo}
            apiBaseUrl={apiBaseUrl}
            sessionId={sessionId}
            sessionUpdatedVersion={sessionUpdatedVersion}
            currentSession={currentSession}
            primaryVideoAccessUrl={primaryVideoAccessUrl}
            hasPrimaryR2Video={hasPrimaryR2Video}
            video={video}
            transcriptJobs={transcriptJobs}
            handleVideoPlayerEvent={handleVideoPlayerEvent}
            handleVideoTimeUpdate={handleVideoTimeUpdate}
            currentVideoTime={currentVideoTime}
            isVideoPlaying={isVideoPlaying}
            creatorIdentity={creatorIdentity}
            handlePrimaryVideoMounted={handlePrimaryVideoMounted}
            retryProcessing={retryProcessing}
            processingRetrying={processingRetrying}
          />
          {!selectedDocument && (
          <>

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
                    {/* SCRUM-398: 'ready' here means the status column flipped but the row's
                        transcript_text/segments aren't loaded into this poll yet — transient.
                        Show a neutral "loading" instead of "No transcript yet." which contradicts
                        the header "Transcript: Ready" badge in PrimaryStage. */}
                    Transcript: {video.transcript_status === 'pending' || video.transcript_status === 'processing'
                      ? 'Processing…'
                      : video.transcript_status === 'ready'
                        ? 'Loading…'
                        : 'No transcript yet.'}
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
                  <div style={{ color: '#666', fontStyle: 'italic', fontSize: '13px' }}>No transcript yet. Import a cloud recording (Teams, Zoom, etc.) or upload media to add one.</div>
                ) : transcriptData.status === 'parsing' ? (
                  <div style={{ display: 'flex', alignItems: 'center', gap: '10px', fontSize: '13px' }}>
                    <span style={{ fontSize: '16px' }}>⏳</span>
                    <span>Parsing transcript…</span>
                  </div>
                ) : transcriptData.status === 'failed' ? (
                  <div>
                    <div style={{ color: 'var(--color-danger-dark)', marginBottom: '10px', fontSize: '13px' }}>
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
                          backgroundColor: 'var(--color-primary-mid)',
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
                          backgroundColor: 'var(--color-primary-mid)',
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
                          <span style={{ fontSize: '12px', color: '#666', marginRight: '8px' }}>
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
                  {unreadQuestionIds.length > 0 && <span style={{ marginLeft: '4px', display: 'inline-block', width: '6px', height: '6px', borderRadius: '50%', backgroundColor: 'var(--color-danger)', verticalAlign: 'middle' }} />}
                </span>
              )}
            </button>
            {!rightPanelCollapsed && debugMode && (
              <button onClick={createMockQuestion} disabled={mockQuestionLoading || loading || !currentSession?.session?.id} style={{ fontSize: '12px', padding: '2px 6px', marginLeft: 'auto', margin: '0 0 0 auto' }}>
                {mockQuestionLoading ? '…' : '🧪'}
              </button>
            )}
          </div>
          {!rightPanelCollapsed && (
            <div className="creator-qa-scroll">
              <div data-testid="orchestration-panel" style={{ margin: '10px 12px', padding: '10px', border: '1px solid #e3e3e3', borderRadius: '6px', backgroundColor: '#fafafa' }}>
                <OrchestrationPanelHeader
                  loading={orchestrationLoading}
                  disabled={orchestrationLoading || !currentSession?.session?.id}
                  onRefresh={() => loadOrchestrationRecommendations(currentSession?.session?.id, { sync: true })}
                />
                {orchestrationFeedback?.message && (
                  <div data-testid="orchestration-feedback" className={orchestrationFeedback.type} style={{ marginBottom: '8px', fontSize: '12px' }}>
                    {orchestrationFeedback.message}
                  </div>
                )}
                {orchestrationRecommendations.length === 0 ? (
                  <div data-testid="orchestration-empty" style={{ fontSize: '12px', color: '#666' }}>
                    {getOrchestrationEmptyStateMessage(currentSession, questions)}
                  </div>
                ) : (() => {
                  const groups = groupRecommendations(orchestrationRecommendations)
                  const orderedTypes = getOrderedGroupTypes(groups)
                  const defaultExpandedType = getDefaultExpandedType(orderedTypes)
                  const isGroupExpanded = (type) =>
                    Object.prototype.hasOwnProperty.call(orchestrationGroupExpansions, type)
                      ? orchestrationGroupExpansions[type]
                      : type === defaultExpandedType
                  const renderCard = (rec, options = {}) => {
                    const decisionReadinessComplete = isDecisionReadinessInputsComplete(rec)
                    const actioning = orchestrationActioningId === rec.id
                    const outcomeFormOpen = recordOutcomeRecId != null && String(recordOutcomeRecId) === String(rec.id)
                    // SCRUM-196: cards collapse to summary by default. Two exceptions:
                    // 1) the single-item decision_readiness rendered ungrouped (expandable=false,
                    //    always expanded — its inline outcome textarea must stay first-class)
                    // 2) when the inline outcome form is open on this card (force expanded so
                    //    the user can finish editing without the card collapsing under them)
                    const expandable = options.expandable !== false
                    const defaultExpanded = options.defaultExpanded === true || outcomeFormOpen
                    return (
                      <OrchestrationRecCard
                        key={rec.id}
                        rec={rec}
                        expandable={expandable}
                        defaultExpanded={defaultExpanded}
                      >
                        {outcomeFormOpen && (
                          <div style={{ width: '100%', marginBottom: '8px' }}>
                            <textarea
                              data-testid={`orchestration-outcome-input-${rec.id}`}
                              value={recordOutcomeText}
                              onChange={(e) => setRecordOutcomeText(e.target.value)}
                              rows={3}
                              placeholder="Decision outcome"
                              disabled={actioning}
                              style={{ width: '100%', boxSizing: 'border-box', fontSize: '12px', padding: '6px', borderRadius: '4px', border: '1px solid #ccc', resize: 'vertical' }}
                            />
                            {recordOutcomeError ? (
                              <div data-testid={`orchestration-outcome-error-${rec.id}`} style={{ marginTop: '4px', fontSize: '12px', color: 'var(--color-danger-dark)' }}>
                                {recordOutcomeError}
                              </div>
                            ) : null}
                            <div style={{ marginTop: '6px' }} className={orchestrationActionsStyles.actions}>
                              <button
                                data-testid={`orchestration-save-outcome-${rec.id}`}
                                type="button"
                                disabled={actioning}
                                onClick={() => saveDecisionOutcomeFromOrchestration(currentSession?.session?.id, rec, recordOutcomeText)}
                                className={`${orchestrationActionsStyles.btn} ${orchestrationActionsStyles.btnPrimary} ${orchestrationActionsStyles.btnPrimary_decision_readiness}`}
                              >
                                {actioning ? '…' : 'Save outcome'}
                              </button>
                              <button
                                type="button"
                                disabled={actioning}
                                onClick={() => {
                                  setRecordOutcomeRecId(null)
                                  setRecordOutcomeError('')
                                }}
                                className={`${orchestrationActionsStyles.btn} ${orchestrationActionsStyles.btnGhost}`}
                              >
                                Cancel
                              </button>
                            </div>
                          </div>
                        )}
                        <OrchestrationRecActions
                          rec={rec}
                          actioning={actioning}
                          outcomeFormOpen={outcomeFormOpen}
                          decisionReadinessComplete={decisionReadinessComplete}
                          onApproveDraft={() => approveDraftAnswerFromRecommendation(currentSession?.session?.id, rec)}
                          onDismissDraft={() => dismissDraftAnswerFromRecommendation(currentSession?.session?.id, rec)}
                          onGenerateDraft={() => generateDraftForRecommendation(currentSession?.session?.id, rec)}
                          onRecordOutcome={() => {
                            setRecordOutcomeRecId(rec.id)
                            setRecordOutcomeText(currentSession?.session?.decision_outcome ?? '')
                            setRecordOutcomeError('')
                          }}
                          onMarkComplete={() => updateRecommendationStatus(currentSession?.session?.id, rec.id, 'completed')}
                          onDismiss={() => updateRecommendationStatus(currentSession?.session?.id, rec.id, 'dismissed')}
                        />
                      </OrchestrationRecCard>
                    )
                  }
                  return (
                    <div data-testid="orchestration-groups" style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
                      {orderedTypes.map((type) => {
                        const recsInGroup = groups[type] || []
                        // Single decision_readiness stays first-class — never behind a header
                        // and not collapsible. The inline outcome textarea must stay visible.
                        if (shouldRenderUngrouped(type, recsInGroup)) {
                          return renderCard(recsInGroup[0], { expandable: false })
                        }
                        const expanded = isGroupExpanded(type)
                        // SCRUM-194: bulk dismiss is offered for review_draft_answer and
                        // unanswered_question only. decision_readiness always needs attention,
                        // and unknown types fall through with no bulk action.
                        const supportsBulkDismiss = type === 'review_draft_answer' || type === 'unanswered_question'
                        const bulkDismiss = supportsBulkDismiss
                          ? () => {
                              const sessionId = currentSession?.session?.id
                              if (!sessionId) return
                              for (const r of recsInGroup) {
                                if (r?.id != null) updateRecommendationStatus(sessionId, r.id, 'dismissed')
                              }
                            }
                          : undefined
                        return (
                          <OrchestrationRecGroup
                            key={type}
                            type={type}
                            count={recsInGroup.length}
                            expanded={expanded}
                            onToggle={() => setOrchestrationGroupExpansions((prev) => ({ ...prev, [type]: !expanded }))}
                            onBulkDismiss={bulkDismiss}
                          >
                            {recsInGroup.map(renderCard)}
                          </OrchestrationRecGroup>
                        )
                      })}
                    </div>
                  )
                })()}
                {/* SCRUM-22: Audit trail — collapsed by default, lazy-loaded on first open */}
                <div style={{ marginTop: '10px', borderTop: '1px solid #e3e3e3', paddingTop: '8px' }}>
                  <button
                    data-testid="orchestration-audit-toggle"
                    type="button"
                    onClick={() => {
                      const next = !auditOpen
                      setAuditOpen(next)
                      if (next) loadOrchestrationAudit(currentSession?.session?.id)
                    }}
                    style={{ background: 'none', border: 'none', padding: 0, cursor: 'pointer', fontSize: '12px', color: '#555', display: 'flex', alignItems: 'center', gap: '4px' }}
                  >
                    <span style={{ display: 'inline-block', transition: 'transform 0.15s', transform: auditOpen ? 'rotate(90deg)' : 'rotate(0deg)' }}>›</span>
                    Activity
                  </button>
                  {auditOpen && (
                    <div data-testid="orchestration-audit-list" style={{ marginTop: '6px' }}>
                      {auditLoading ? (
                        <div style={{ fontSize: '12px', color: '#999' }}>Loading…</div>
                      ) : auditEntries.length === 0 ? (
                        <div data-testid="orchestration-audit-empty" style={{ fontSize: '12px', color: '#999' }}>No activity yet.</div>
                      ) : (
                        auditEntries.map((entry, i) => (
                          <div key={i} data-testid={`orchestration-audit-entry-${i}`} style={{ fontSize: '12px', color: '#555', padding: '4px 0', borderBottom: i < auditEntries.length - 1 ? '1px solid #f0f0f0' : 'none' }}>
                            <span style={{ fontWeight: 600, textTransform: 'uppercase' }}>{String(entry.recommendation_type || '').replaceAll('_', ' ')}</span>
                            {' · '}
                            <span>{entry.from_status || '—'} → {entry.to_status}</span>
                            {entry.changed_by && <span style={{ color: '#888' }}> by {entry.changed_by}</span>}
                            {entry.changed_at && <span style={{ color: '#aaa' }}> · {new Date(entry.changed_at).toLocaleString()}</span>}
                          </div>
                        ))
                      )}
                    </div>
                  )}
                </div>
              </div>
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
                      // SCRUM-367: peer tombstone for ~10s after question_deleted WS event.
                      if (q._tombstone) {
                        return (
                          <div
                            key={q.id}
                            data-testid="question-tombstone"
                            style={{
                              marginBottom: depth === 0 ? '8px' : 0,
                              padding: '12px 15px',
                              border: '1px dashed #bbb',
                              borderRadius: '5px',
                              color: '#777',
                              fontStyle: 'italic',
                              fontSize: '13px',
                              backgroundColor: '#fafafa',
                              ...(depth > 0 && { marginLeft: 28 }),
                              opacity: 0.7,
                              transition: 'opacity 0.6s ease',
                            }}
                          >
                            This question was deleted by the creator
                          </div>
                        )
                      }
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
                                      <span style={{ fontSize: '12px', fontWeight: 600, color: 'var(--color-primary)', backgroundColor: 'var(--color-primary-bg)', padding: '2px 6px', borderRadius: '4px' }}>New</span>
                                    )}
                                  </div>
                                ) : (
                                  <>
                                    <div style={{ fontWeight: 'bold', marginBottom: '5px', color: '#333', display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap' }}>
                                      Q: {q.question_text}
                                      {unreadQuestionIds && unreadQuestionIds.includes(String(q.id)) && (
                                        <span style={{ fontSize: '12px', fontWeight: 600, color: 'var(--color-primary)', backgroundColor: 'var(--color-primary-bg)', padding: '2px 6px', borderRadius: '4px' }}>New</span>
                                      )}
                                    </div>
                                    <div style={{ fontSize: '12px', color: '#999', marginTop: '5px', marginBottom: '10px' }}>
                                      Asked: {new Date(q.created_at).toLocaleString()}
                                      {' · asked by '}
                                      {q.asked_by ? <strong>{q.asked_by}</strong> : <span style={{ color: '#999' }}>—</span>}
                                      {q.video_time_seconds !== null && q.video_time_seconds !== undefined && (
                                        <span style={{ marginLeft: '8px', color: 'var(--color-primary-mid)', fontWeight: 'bold' }}>
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
                                                color: q.answer.answer_status === 'answered' ? 'var(--color-success-mid)' :
                                                       q.answer.answer_status === 'not_covered' ? '#ff9800' : 'var(--color-danger)',
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
                                                    <span style={{ color: 'var(--color-success)' }} title="Verified">✓</span>
                                                  ) : (
                                                    <span style={{ color: 'var(--color-danger-dark)' }} title="Not verified">✕</span>
                                                  )}
                                                  <span style={{ marginLeft: '4px' }}>{q.answer.confirmed ? 'Verified' : 'Verify this answer'}</span>
                                                </span>
                                              </label>
                                            )}
                                            {answeringQuestionId !== q.id && (
                                              <button
                                                type="button"
                                                onClick={() => startAnswering(q.id, q.answer)}
                                                style={{ padding: '6px 12px', fontSize: '13px', fontWeight: 600, color: 'var(--color-primary-dark)', backgroundColor: 'var(--color-primary-bg)', border: '1px solid #2196F3', borderRadius: '4px', cursor: 'pointer', margin: 0 }}
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
                                              backgroundColor: answerVoiceRecording ? 'var(--color-danger-mid)' : 'var(--color-primary)',
                                              padding: '8px 12px'
                                            }}
                                          >
                                            {answerVoiceRecording ? 'Stop Mic' : (answerVoiceUploading ? 'Processing…' : 'Mic')}
                                          </button>
                                          <div style={{ fontSize: '13px', color: answerVoiceRecording ? 'var(--color-danger-mid)' : '#666' }}>
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
                              {/* SCRUM-370: trash icon — root question only, creator/admin only. */}
                              {depth === 0 && canDeleteQuestions && requestDeleteQuestion && (
                                <button
                                  type="button"
                                  data-testid={`delete-question-trigger-${q.id}`}
                                  aria-label="Delete question"
                                  onClick={(e) => {
                                    e.stopPropagation()
                                    requestDeleteQuestion(q.id)
                                  }}
                                  style={{
                                    flexShrink: 0,
                                    marginLeft: 'auto',
                                    marginTop: 0,
                                    padding: '4px 8px',
                                    background: 'none',
                                    border: 'none',
                                    cursor: 'pointer',
                                    color: '#666',
                                    lineHeight: 0,
                                    display: 'inline-flex',
                                    alignItems: 'center',
                                  }}
                                >
                                  <TrashIcon />
                                </button>
                              )}
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
