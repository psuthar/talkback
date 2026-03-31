import { useState, useEffect, useRef, useCallback, useMemo } from 'react'
import { VideoPlayer, PlayerEvent } from './VideoPlayer'
import { CreatorMode } from './modes/CreatorMode'
import { ParticipantMode } from './modes/ParticipantMode'
import { useWebSocket } from './hooks/useWebSocket'
import { TranscriptViewer } from './components/TranscriptViewer'
import { AdminUsers } from './components/AdminUsers'
import { LoginPage } from './components/LoginPage'
import { AcceptInvitePage } from './components/AcceptInvitePage'
import { getDefaultApiBaseUrl, getVoiceSilenceMs } from './config'
import { buildInviteMailto, buildInviteMessageBody, isValidEmailFormat } from './utils/inviteMailto'
import {
  parseSessionNavigationFromLocation,
  parseSessionIdFromPathname,
  buildCanonicalSessionUrl,
  historyPathFromLocation,
  isLikelySessionId,
  sessionLoadMessageForStatus,
} from './sessionNavigation'

const API_BASE_URL_STORAGE_KEY = 'talkback.apiBaseUrl'

// Named constants for the create-session source selector.
// When a new provider (e.g. Teams) is added, extend this object and reference the constant
// everywhere instead of duplicating string literals.
const CREATE_SOURCE = /** @type {const} */ ({
  EMPTY: 'empty',
  ZOOM: 'zoom',
  TEAMS: 'teams',
})

// Compact import status + Retry when session has Zoom source but no primary video (Artifact View).
function AppVideoImportStatus({ sessionId, apiBaseUrl, creatorIdentity, onRetry, refetchSession }) {
  const [processingStatus, setProcessingStatus] = useState(null)
  const [retrying, setRetrying] = useState(false)
  const intervalRef = useRef(null)
  useEffect(() => {
    if (!sessionId || apiBaseUrl == null) return
    const base = apiBaseUrl.replace(/\/$/, '')
    const fetchProcessing = () => {
      fetch(`${base}/api/sessions/${sessionId}/processing`, { headers: { 'X-Creator-Identity': creatorIdentity } })
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
    const ms = processingStatus?.state === 'waiting' ? 15000 : 5000
    intervalRef.current = setInterval(fetchProcessing, ms)
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current)
    }
  }, [sessionId, apiBaseUrl, creatorIdentity, processingStatus?.state])
  const handleRetry = () => {
    setRetrying(true)
    Promise.resolve(onRetry?.()).finally(() => {
      setTimeout(() => setRetrying(false), 2000)
      if (typeof refetchSession === 'function') refetchSession()
    })
  }
  const showRetry = processingStatus?.state === 'failed_transient' || processingStatus?.state === 'failed_permanent' || processingStatus?.state === 'waiting'
  if (!processingStatus?.state) {
    return (
      <div style={{ marginTop: '12px', fontSize: '13px', color: '#666' }}>
        Import status: checking…
      </div>
    )
  }
  return (
    <div style={{ marginTop: '12px', padding: '10px 12px', backgroundColor: '#f0f7ff', borderRadius: '6px', border: '1px solid #b3d9ff', fontSize: '13px' }}>
      <span style={{ fontWeight: 600, color: '#333' }}>Import status:</span>{' '}
      <span style={{ color: processingStatus.state === 'ready' ? '#4CAF50' : processingStatus.state.startsWith('failed') ? '#c62828' : '#1976D2' }}>
        {processingStatus.state === 'ready' ? 'Complete' : processingStatus.state.replace(/_/g, ' ')}
      </span>
      {processingStatus.last_error_message && (
        <span style={{ color: '#666', marginLeft: '8px' }}>— {processingStatus.last_error_message}</span>
      )}
      {showRetry && (
        <button
          type="button"
          onClick={handleRetry}
          disabled={retrying}
          style={{ marginLeft: '12px', padding: '4px 10px', fontSize: '12px', backgroundColor: '#2196F3', color: 'white', border: 'none', borderRadius: '4px', cursor: retrying ? 'not-allowed' : 'pointer' }}
        >
          {retrying ? 'Retrying…' : 'Retry import'}
        </button>
      )}
    </div>
  )
}

function App() {
  const [apiBaseUrl, setApiBaseUrl] = useState(() => {
    try {
      const stored = localStorage.getItem(API_BASE_URL_STORAGE_KEY) || localStorage.getItem('talkback.api_base_url')
      if (stored && stored.trim()) return stored.trim()
    } catch (_) { /* ignore */ }
    return getDefaultApiBaseUrl()
  })
  const [artifactId, setArtifactId] = useState('')
  const [videoId, setVideoId] = useState('')
  
  // Form states
  const [artifactTitle, setArtifactTitle] = useState('')
  const [artifactDescription, setArtifactDescription] = useState('')
  const [materialFiles, setMaterialFiles] = useState([]) // Array of File objects for multiple uploads
  const [uploadedMaterials, setUploadedMaterials] = useState([]) // Array of successfully uploaded materials
  const [videoProvider, setVideoProvider] = useState('loom')
  const [videoUrl, setVideoUrl] = useState('')
  const [playbackMode, setPlaybackMode] = useState('embed') // 'embed' or 'direct'
  const [embedUrl, setEmbedUrl] = useState('')
  const [mediaUrl, setMediaUrl] = useState('')
  const [posterUrl, setPosterUrl] = useState('')
  const [durationSeconds, setDurationSeconds] = useState('')
  const [transcriptText, setTranscriptText] = useState('')
  const [questionText, setQuestionText] = useState('')
  const [voiceRecording, setVoiceRecording] = useState(false)
  const [voiceUploading, setVoiceUploading] = useState(false)
  const [voiceFeedback, setVoiceFeedback] = useState({ type: '', message: '' })
  const [voiceTranscribedText, setVoiceTranscribedText] = useState('')
  const [showVoiceConfirm, setShowVoiceConfirm] = useState(false)
  const [voicePolishing, setVoicePolishing] = useState(false)
  const [voicePolishMode, setVoicePolishMode] = useState(null) // 'rules' | 'llm' when polishing
  const [mediaRecorder, setMediaRecorder] = useState(null)
  const [mediaStream, setMediaStream] = useState(null)
  const voiceChunksRef = useRef([])
  const voiceSilenceIntervalRef = useRef(null)
  const voiceContextRef = useRef(null)
  const voiceRecorderRef = useRef(null)
  const materialFileInputRef = useRef(null)
  const lastMaterialUploadAtRef = useRef(0) // skip session_updated refetch for a few seconds after upload so new material doesn't disappear
  const scheduledSessionRefetchTimeoutRef = useRef(null)

  // Phase 3: Session states
  const [sessionTitle, setSessionTitle] = useState('')
  const [currentSession, setCurrentSession] = useState(null)
  const [participantRef, setParticipantRef] = useState('')
  const [viewMode, setViewMode] = useState('session')
  const [sessionMode, setSessionMode] = useState('select') // 'create' or 'select' — default list; effect sets 'create' when no sessions and can create
  const [createSource, setCreateSource] = useState(CREATE_SOURCE.EMPTY) // CREATE_SOURCE.EMPTY | ZOOM | TEAMS when sessionMode === 'create'
  const [zoomPasteUrlExpanded, setZoomPasteUrlExpanded] = useState(false) // collapsible "Or paste Zoom recording URL"
  const [sessionIdInput, setSessionIdInput] = useState('')
  const [sessionSelectFeedback, setSessionSelectFeedback] = useState({ type: '', message: '' })
  
  // Creator/Participant Mode states
  const [currentUser, setCurrentUser] = useState('') // User identifier for mode detection
  const [sessionUserMode, setSessionUserMode] = useState(null) // 'creator' or 'participant' - from API
  const [sessionProcessingReadyVersion, setSessionProcessingReadyVersion] = useState(0) // bumped when WebSocket session_processing_ready; CreatorMode uses to show progress until refetch completes
  const [sessionUpdatedVersion, setSessionUpdatedVersion] = useState(0) // bumped when WebSocket session_updated (e.g. slides ready); SlideDeckViewer refetches slides on change
  const [stanceVersion, setStanceVersion] = useState(0) // bumped when WebSocket stance_updated; mode components use to refetch stances
  /** Bumped on session WebSocket events while creator is in session view; CreatorMode debounces POST /orchestration/recommendations/sync (SCRUM-16). */
  const [orchestrationRefreshTrigger, setOrchestrationRefreshTrigger] = useState(0)
  const [replyingToQuestionId, setReplyingToQuestionId] = useState(null) // Threaded reply: parent question id when user clicked "Reply"

  // TalkBack auth: logged-in user from GET /api/me (cookie or Bearer accept_token for incognito)
  const [authUser, setAuthUser] = useState(null) // { id, email, display_name, global_role, status } or null
  const [authChecked, setAuthChecked] = useState(false) // true after first /api/me request completes
  const [acceptToken, setAcceptToken] = useState(() => {
    try { return sessionStorage.getItem('talkback.accept_token') || null } catch { return null }
  })

  // My sessions (creator): list of sessions created by current user, for session selection
  const [mySessions, setMySessions] = useState([])
  const [mySessionsLoading, setMySessionsLoading] = useState(false)
  const [mySessionsError, setMySessionsError] = useState('')
  // Pending session invitations for the logged-in user (shown in "Pending Invites" section)
  const [pendingInvitations, setPendingInvitations] = useState([])
  const [pendingInvitationsLoading, setPendingInvitationsLoading] = useState(false)
  const [pendingInvitationsFetched, setPendingInvitationsFetched] = useState(false) // true after first successful fetch
  const [acceptingInvitationId, setAcceptingInvitationId] = useState(null)
  const [copyingSessionId, setCopyingSessionId] = useState(null) // session id while copy in progress
  const [copyModalSession, setCopyModalSession] = useState(null) // { id, title } when copy modal is open
  const [copyModalTitle, setCopyModalTitle] = useState('') // optional title for copy
  const [renameSessionId, setRenameSessionId] = useState(null)
  const [renameSessionTitle, setRenameSessionTitle] = useState('')
  const [renameSaving, setRenameSaving] = useState(false)
  
  // Response states - per-section feedback
  const [createArtifactFeedback, setCreateArtifactFeedback] = useState({ type: '', message: '' })
  const [uploadMaterialFeedback, setUploadMaterialFeedback] = useState({ type: '', message: '' })
  const [attachVideoFeedback, setAttachVideoFeedback] = useState({ type: '', message: '' })
  const [submitTranscriptFeedback, setSubmitTranscriptFeedback] = useState({ type: '', message: '' })
  const [askQuestionFeedback, setAskQuestionFeedback] = useState({ type: '', message: '' })
  const [questionHistoryFeedback, setQuestionHistoryFeedback] = useState({ type: '', message: '' })
  const [createSessionFeedback, setCreateSessionFeedback] = useState({ type: '', message: '' })
  const [joinSessionFeedback, setJoinSessionFeedback] = useState({ type: '', message: '' })
  const [inviteEmail, setInviteEmail] = useState('')
  const [inviteRole, setInviteRole] = useState('participant')
  const [inviteFeedback, setInviteFeedback] = useState({ type: '', message: '' })
  const [inviteLoading, setInviteLoading] = useState(false)
  const [sessionInvitations, setSessionInvitations] = useState([])
  const [lastInvitationDraft, setLastInvitationDraft] = useState(null) // for mailto fallback (copy link / copy message)

  // Global states
  const [loading, setLoading] = useState(false)
  const [currentAnswer, setCurrentAnswer] = useState(null)
  const [questions, setQuestions] = useState([])
  const [unreadQuestionIds, setUnreadQuestionIds] = useState([]) // IDs of questions with new/unread content (from GET ?participant_ref=)
  const [pendingSessionQuestions, setPendingSessionQuestions] = useState([]) // Optimistic: show question immediately while waiting for /ask response
  const [mockQuestions, setMockQuestions] = useState([]) // In-memory mock questions (not persisted)
  const [mockQuestionLoading, setMockQuestionLoading] = useState(false)
  const [confirmingAnswerId, setConfirmingAnswerId] = useState(null)
  const [videoFile, setVideoFile] = useState(null) // MP4 file for upload
  const [videoFileUploading, setVideoFileUploading] = useState(false)
  const [loomVideoSource, setLoomVideoSource] = useState(null) // Video source that requires upload (Loom)
  const [transcriptFile, setTranscriptFile] = useState(null) // MP4 file for transcript upload
  const [transcriptFileUploading, setTranscriptFileUploading] = useState(false)
  const [apiHealth, setApiHealth] = useState(null) // null = unknown, true = healthy, false = unhealthy
  const [healthChecking, setHealthChecking] = useState(false)
  const [debugMode, setDebugMode] = useState(false)
  const [urlKey, setUrlKey] = useState(0) // bump to re-read URL after replaceState (React does not re-render on history alone)
  /** Bumped only on browser back/forward so deep-link sync runs without re-fetching on every urlKey bump from in-app replaceState. */
  const [popstateNavKey, setPopstateNavKey] = useState(0)

  useEffect(() => {
    const onPopState = () => setPopstateNavKey((k) => k + 1)
    window.addEventListener('popstate', onPopState)
    return () => window.removeEventListener('popstate', onPopState)
  }, [])
  // Admin panel: section expanded state (collapsed by default; reset on logout; preserved when switching app ↔ admin)
  const [adminUsersExpanded, setAdminUsersExpanded] = useState(false)
  const [adminSessionsExpanded, setAdminSessionsExpanded] = useState(false)
  
  // Video player states. selectedVideoIdRef stores the user's chosen video id so selection survives refetches and effect runs.
  const [selectedVideo, setSelectedVideo] = useState(null)
  const selectedVideoIdRef = useRef(null)
  const setSelectedVideoWithRef = useCallback((v) => {
    const id = v?.id ?? null
    selectedVideoIdRef.current = id
    setSelectedVideo(v)
    setVideoId(id)
  }, [])
  const [videoPlayerKey, setVideoPlayerKey] = useState(0) // Used to force iframe reload for restart
  const [currentVideoTime, setCurrentVideoTime] = useState(0)
  const [isVideoPlaying, setIsVideoPlaying] = useState(false)
  
  // Transcript job states
  const [transcriptJobs, setTranscriptJobs] = useState({}) // Map of videoId -> job

  // Zoom: creator identity (localStorage) and connection status
  const [creatorIdentity, setCreatorIdentityState] = useState(() => {
    try {
      return localStorage.getItem('talkback.creator_identity') || ''
    } catch {
      return ''
    }
  })
  const [zoomConnection, setZoomConnection] = useState(null) // { zoom_user_email, zoom_user_id } or null
  const [zoomUrl, setZoomUrl] = useState('')
  const [zoomTitle, setZoomTitle] = useState('')
  const [zoomImporting, setZoomImporting] = useState(false)
  const [zoomImportError, setZoomImportError] = useState('')
  const [zoomTranscriptStatus, setZoomTranscriptStatus] = useState(null) // 'ready' | 'processing' | 'not_available' | 'recording_not_found' | 'error'
  const [zoomTranscriptMessage, setZoomTranscriptMessage] = useState('')
  const [zoomTranscriptTopic, setZoomTranscriptTopic] = useState(null)
  const [zoomCheckingTranscript, setZoomCheckingTranscript] = useState(false)
  // Zoom recordings list (mission: Import from Zoom panel)
  const [zoomRecordings, setZoomRecordings] = useState([])
  const [zoomRecordingsLoading, setZoomRecordingsLoading] = useState(false)
  const [zoomRecordingsError, setZoomRecordingsError] = useState('')
  const [zoomImportToast, setZoomImportToast] = useState(null) // { message } or null
  const [zoomImportModalRec, setZoomImportModalRec] = useState(null) // recording to import; when set, show session-name modal
  const [zoomImportSessionName, setZoomImportSessionName] = useState('') // proposed/edited session name for modal
  const [zoomImportModalError, setZoomImportModalError] = useState('')

  // Teams (Microsoft): gated by API GET /api/teams/status → enabled
  const [teamsApiEnabled, setTeamsApiEnabled] = useState(false)
  const [teamsConnection, setTeamsConnection] = useState(null) // { teams_email, teams_user_id } or null
  const [teamsRecordings, setTeamsRecordings] = useState([])
  const [teamsRecordingsLoading, setTeamsRecordingsLoading] = useState(false)
  const [teamsRecordingsError, setTeamsRecordingsError] = useState('')
  const [teamsImporting, setTeamsImporting] = useState(false)
  const [teamsImportError, setTeamsImportError] = useState('')
  const [teamsImportToast, setTeamsImportToast] = useState(null) // { message } or null
  const [teamsImportModalRec, setTeamsImportModalRec] = useState(null)
  const [teamsImportSessionName, setTeamsImportSessionName] = useState('')
  const [teamsImportModalError, setTeamsImportModalError] = useState('')

  const setCreatorIdentity = (id) => {
    setCreatorIdentityState(id)
    try {
      if (id) localStorage.setItem('talkback.creator_identity', id)
      else localStorage.removeItem('talkback.creator_identity')
    } catch {
      // ignore
    }
  }

  const clearFeedback = (setter) => {
    setter({ type: '', message: '' })
  }

  const cleanupVoiceMedia = () => {
    if (voiceSilenceIntervalRef.current) {
      clearInterval(voiceSilenceIntervalRef.current)
      voiceSilenceIntervalRef.current = null
    }
    try {
      const ctx = voiceContextRef.current
      if (ctx && ctx.state !== 'closed') ctx.close()
      voiceContextRef.current = null
    } catch { /* ignore */ }
    try {
      if (mediaRecorder && mediaRecorder.state !== 'inactive') {
        mediaRecorder.stop()
      }
    } catch {
      // ignore
    }
    voiceRecorderRef.current = null
    if (mediaStream) {
      mediaStream.getTracks().forEach(t => t.stop())
    }
    setMediaRecorder(null)
    setMediaStream(null)
    setVoiceRecording(false)
    voiceChunksRef.current = []
  }

  // Convert Loom share URL to embed URL
  const getLoomEmbedUrl = (shareUrl) => {
    if (!shareUrl) return null
    // Extract video ID from Loom share URL
    // Format: https://www.loom.com/share/{video-id}
    const match = shareUrl.match(/loom\.com\/share\/([a-zA-Z0-9]+)/)
    if (match && match[1]) {
      return `https://www.loom.com/embed/${match[1]}`
    }
    return null
  }

  // Get embed URL based on provider
  const getVideoEmbedUrl = (video) => {
    if (!video || !video.video_url) return null
    
    switch (video.provider) {
      case 'loom':
        return getLoomEmbedUrl(video.video_url)
      case 'zoom':
        // Zoom videos might need different handling
        return video.video_url
      case 'teams':
        return video.video_url
      case 'other':
        // For other providers, try to use the URL directly
        return video.video_url
      default:
        return video.video_url
    }
  }

  // Video player controls
  const handleVideoPlay = () => {
    const iframe = document.getElementById('video-player-iframe')
    if (iframe && iframe.contentWindow) {
      // For Loom, we can't directly control playback via iframe
      // The iframe has its own controls
      // For other providers, we might need different approaches
    }
  }

  const handleVideoPause = () => {
    const iframe = document.getElementById('video-player-iframe')
    if (iframe && iframe.contentWindow) {
      // Similar to play - depends on provider
    }
  }

  const handleVideoRestart = () => {
    // Force iframe reload by changing key
    setVideoPlayerKey(prev => prev + 1)
  }

  // Video player event handlers
  const handleVideoPlayerEvent = (event) => {
    if (event.type === PlayerEvent.PLAY) {
      setIsVideoPlaying(true)
      if (event.time !== undefined) {
        setCurrentVideoTime(event.time)
      }
    } else if (event.type === PlayerEvent.PAUSE) {
      setIsVideoPlaying(false)
      if (event.time !== undefined) {
        setCurrentVideoTime(event.time)
      }
    } else if (event.type === PlayerEvent.SEEK || event.type === PlayerEvent.TIMEUPDATE) {
      if (event.time !== undefined) {
        setCurrentVideoTime(event.time)
      }
    }
  }

  const handleVideoTimeUpdate = (time) => {
    setCurrentVideoTime(time)
  }

  const checkApiHealth = async (signal) => {
    setHealthChecking(true)
    try {
      const response = await fetch(`${apiBaseUrl}/health`, {
        method: 'GET',
        headers: { 'Content-Type': 'application/json' },
        signal: signal
      })

      if (response.ok) {
        const data = await response.json()
        if (data.status === 'ok') {
          setApiHealth(true)
          return true
        }
      }
      setApiHealth(false)
      return false
    } catch (err) {
      // Don't update state if the request was aborted
      if (err.name === 'AbortError') {
        return false
      }
      setApiHealth(false)
      return false
    } finally {
      setHealthChecking(false)
    }
  }

  // Check health when API URL changes (with debounce and cleanup)
  useEffect(() => {
    if (apiBaseUrl == null) {
      return
    }

    // Reset health status when URL changes
    setApiHealth(null)
    setHealthChecking(true)

    // Create AbortController to cancel previous requests
    const abortController = new AbortController()

    // Debounce the health check by 500ms
    const timeoutId = setTimeout(() => {
      checkApiHealth(abortController.signal)
    }, 500)

    // Cleanup: cancel the request if URL changes again or component unmounts
    return () => {
      clearTimeout(timeoutId)
      abortController.abort()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [apiBaseUrl])

  // Fetch /api/me (TalkBack auth) when API URL or acceptToken changes — cookie or Bearer token (incognito)
  useEffect(() => {
    if (apiBaseUrl == null) {
      setAuthUser(null)
      setAuthChecked(true)
      return
    }
    setAuthChecked(false)
    let cancelled = false
    const headers = {}
    if (acceptToken) headers['Authorization'] = `Bearer ${acceptToken}`
    fetch(`${apiBaseUrl.replace(/\/$/, '')}/api/me`, { credentials: 'include', headers })
      .then(res => {
        if (cancelled) return
        if (res.ok) return res.json()
        setAuthUser(null)
        return null
      })
      .then(data => {
        if (!cancelled) {
          if (data) {
            setAuthUser(data)
            if (data.accept_token) {
              setAcceptToken(data.accept_token)
              try { sessionStorage.setItem('talkback.accept_token', data.accept_token) } catch (_) {}
            }
          } else setAuthUser(null)
          setAuthChecked(true)
        }
      })
      .catch(() => {
        if (!cancelled) {
          setAuthUser(null)
          setAuthChecked(true)
        }
      })
    return () => { cancelled = true }
  }, [apiBaseUrl, acceptToken])

  // After login: if there are sessions, show list first for everyone; if no sessions and user can create (creator/admin), show create first
  useEffect(() => {
    if (!authUser || mySessionsLoading || currentSession) return
    if (mySessions.length > 0) {
      setSessionMode('select')
    } else {
      const canCreate = authUser.global_role !== 'participant'
      setSessionMode(canCreate ? 'create' : 'select')
    }
  }, [authUser, mySessionsLoading, mySessions.length, currentSession])

  // Default participant identity to logged-in user's email (no anonymous users)
  useEffect(() => {
    if (authUser?.email && !participantRef) {
      setParticipantRef(authUser.email)
    }
  }, [authUser?.email])

  // Keep participant URL in sync so refresh shows the same view: when logged in as participant, ensure URL has mode=view (with or without a session).
  // Prefer canonical /app/sessions/:id when a session id is present.
  useEffect(() => {
    if (authUser?.global_role !== 'participant') return
    if (window.location.pathname.replace(/\/$/, '') === '/accept-invite') return // do not overwrite accept-invite URL (would drop token)
    const params = new URLSearchParams(window.location.search)
    if (params.get('mode') === 'view') return
    const pathId = parseSessionIdFromPathname(window.location.pathname)
    const queryId = params.get('session')
    const sessionId = pathId || queryId
    const apiPart = params.get('api')
    if (sessionId) {
      window.history.replaceState(null, '', buildCanonicalSessionUrl(sessionId, { mode: 'view', api: apiPart || undefined }))
    } else {
      window.history.replaceState(null, '', `${window.location.pathname}?mode=view`)
    }
    setUrlKey(k => k + 1)
  }, [authUser?.global_role, apiBaseUrl])

  // If URL has ?mode=admin but user is not admin, clear it so they see session list instead of "Forbidden"
  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    if (params.get('mode') === 'admin' && authUser && authUser.global_role !== 'admin') {
      params.delete('mode')
      const newSearch = params.toString()
      const newUrl = window.location.pathname + (newSearch ? '?' + newSearch : '') + (window.location.hash || '')
      window.history.replaceState(null, '', newUrl)
      setUrlKey(k => k + 1)
    }
  }, [authUser])

  // Admin section expansion: reset to collapsed when user logs out so next login sees collapsed
  useEffect(() => {
    if (!authUser) {
      setAdminUsersExpanded(false)
      setAdminSessionsExpanded(false)
    }
  }, [authUser])

  // Participants must never see edit mode: if auth just loaded and user is participant but we're in creator mode (e.g. opened via ?mode=edit before auth loaded), force participant mode and URL
  useEffect(() => {
    if (authUser?.global_role !== 'participant') return
    if (window.location.pathname.replace(/\/$/, '') === '/accept-invite') return // do not overwrite accept-invite URL (would drop token)
    const params = new URLSearchParams(window.location.search)
    if (params.get('mode') !== 'edit') return
    const sessionId = parseSessionIdFromPathname(window.location.pathname) || params.get('session')
    if (!sessionId) return
    setSessionUserMode('participant')
    setCurrentUser('participant')
    const apiParam = params.get('api')
    window.history.replaceState(null, '', buildCanonicalSessionUrl(sessionId, { mode: 'view', api: apiParam || undefined }))
    setUrlKey(k => k + 1)
  }, [authUser?.global_role, apiBaseUrl])

  const fetchPendingInvitations = useCallback(async () => {
    if (!authUser?.email || apiBaseUrl == null) {
      setPendingInvitations([])
      setPendingInvitationsFetched(false)
      return
    }
    setPendingInvitationsLoading(true)
    setPendingInvitationsFetched(false)
    try {
      const base = apiBaseUrl.replace(/\/$/, '')
      const headers = {}
      if (acceptToken) headers['Authorization'] = `Bearer ${acceptToken}`
      const res = await fetch(`${base}/api/invitations/pending`, { credentials: 'include', headers })
      const data = await res.json().catch(() => ({}))
      if (!res.ok) {
        setPendingInvitations([])
        setPendingInvitationsFetched(true)
        return
      }
      setPendingInvitations(Array.isArray(data.invitations) ? data.invitations : [])
      setPendingInvitationsFetched(true)
    } catch (_) {
      setPendingInvitations([])
      setPendingInvitationsFetched(true)
    } finally {
      setPendingInvitationsLoading(false)
    }
  }, [authUser?.email, apiBaseUrl, acceptToken])

  // Fetch "my sessions" and pending invitations when no session selected and user is logged in (same conditions so both APIs run together)
  useEffect(() => {
    if (!authUser || apiBaseUrl == null || currentSession) {
      if (!authUser && !currentSession) {
        setMySessions([])
        setMySessionsError('')
      }
      return
    }
    // Fetch pending invitations whenever we fetch sessions (so invitations API is always called on selection screen)
    fetchPendingInvitations()
    let cancelled = false
    setMySessionsLoading(true)
    setMySessionsError('')
    const url = apiBaseUrl.replace(/\/$/, '') + '/api/sessions'
    const headers = {}
    if (acceptToken) headers['Authorization'] = `Bearer ${acceptToken}`
    fetch(url, { credentials: 'include', headers })
      .then(res => {
        if (cancelled) return
        if (res.status === 401) {
          setMySessions([])
          setMySessionsError('')
          return []
        }
        if (!res.ok) {
          throw new Error(res.statusText || 'Failed to load sessions')
        }
        return res.json()
      })
      .then(data => {
        if (!cancelled && Array.isArray(data)) {
          setMySessions(data) // [{ session, my_role }, ...]
          setMySessionsError('')
        }
      })
      .catch(err => {
        if (!cancelled) {
          setMySessions([])
          setMySessionsError(err?.message || 'Failed to load your sessions')
        }
      })
      .finally(() => {
        if (!cancelled) setMySessionsLoading(false)
      })
    return () => { cancelled = true }
  }, [authUser, apiBaseUrl, currentSession, acceptToken, fetchPendingInvitations])

  useEffect(() => {
    if (!authUser || currentSession) {
      setPendingInvitations([])
      setPendingInvitationsFetched(false)
      return
    }
    fetchPendingInvitations()
  }, [authUser, currentSession, fetchPendingInvitations])

  // Sync creator identity to logged-in user email so new sessions (e.g. from Zoom) show in "Your sessions"
  useEffect(() => {
    if (authUser?.email) {
      setCreatorIdentity(authUser.email)
    }
  }, [authUser?.email])

  // Poll for transcript job status when videos are pending
  useEffect(() => {
    if (!currentSession || !currentSession.video_sources) {
      return
    }

    // Find videos with pending transcript status
    const pendingVideos = currentSession.video_sources.filter(v => v.transcript_status === 'pending')
    if (pendingVideos.length === 0) {
      return
    }

    // Poll for job status every 5 seconds
    const intervalId = setInterval(async () => {
      for (const video of pendingVideos) {
        const job = await fetchTranscriptJob(video.id)
        if (job) {
          setTranscriptJobs(prev => ({ ...prev, [video.id]: job }))
          
          // If job completed or failed, refresh session
          if (job.status === 'completed' || job.status === 'failed') {
            const sessionId = currentSession.session ? currentSession.session.id : currentSession.id
            await openSession(sessionId)
          }
        }
      }
    }, 5000)

    // Initial fetch
    pendingVideos.forEach(async (video) => {
      const job = await fetchTranscriptJob(video.id)
      if (job) {
        setTranscriptJobs(prev => ({ ...prev, [video.id]: job }))
      }
    })

    return () => clearInterval(intervalId)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currentSession, artifactId, apiBaseUrl])


  const createArtifact = async () => {
    if (!currentSession) {
      setCreateArtifactFeedback({ type: 'error', message: 'Please select or create a session first' })
      return
    }
    
    clearFeedback(setCreateArtifactFeedback)
    setLoading(true)
    
    try {
      const response = await fetch(`${apiBaseUrl}/artifacts`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          session_id: currentSession.session ? currentSession.session.id : currentSession.id,
          title: artifactTitle,
          description: artifactDescription || undefined
        })
      })

      if (!response.ok) {
        const text = await response.text()
        try {
          const json = JSON.parse(text)
          setCreateArtifactFeedback({ type: 'error', message: `Error ${response.status}: ${JSON.stringify(json, null, 2)}` })
        } catch {
          setCreateArtifactFeedback({ type: 'error', message: `Error ${response.status}: ${text}` })
        }
        return
      }

      const data = await response.json()
      setArtifactId(data.id)
      setCreateArtifactFeedback({ type: 'success', message: `Artifact created! ID: ${data.id}` })
      setArtifactTitle('')
      setArtifactDescription('')
    } catch (err) {
      setCreateArtifactFeedback({ type: 'error', message: `Failed to create artifact: ${err.message}` })
    } finally {
      setLoading(false)
    }
  }

  const uploadMaterial = async (file) => {
    if (!currentSession) {
      setUploadMaterialFeedback({ type: 'error', message: 'Please select or create a session first' })
      return
    }
    if (!artifactId) {
      setUploadMaterialFeedback({ type: 'error', message: 'Please create an artifact first' })
      return
    }
    if (!file) {
      setUploadMaterialFeedback({ type: 'error', message: 'Please select a file' })
      return
    }

    clearFeedback(setUploadMaterialFeedback)
    setLoading(true)

    try {
      const formData = new FormData()
      formData.append('file', file)

      const response = await fetch(`${apiBaseUrl}/artifacts/${artifactId}/materials`, {
        method: 'POST',
        body: formData
      })

      if (!response.ok) {
        const text = await response.text()
        try {
          const json = JSON.parse(text)
          setUploadMaterialFeedback({ type: 'error', message: `Error ${response.status}: ${JSON.stringify(json, null, 2)}` })
        } catch {
          setUploadMaterialFeedback({ type: 'error', message: `Error ${response.status}: ${text}` })
        }
        return
      }

      const data = await response.json()
      lastMaterialUploadAtRef.current = Date.now()
      // Add to uploaded materials list
      setUploadedMaterials(prev => [...prev, { ...data, uploadedAt: new Date().toISOString() }])
      // Remove file from materialFiles array
      setMaterialFiles(prev => prev.filter(f => f !== file))
      setUploadMaterialFeedback({ type: 'success', message: `Material uploaded! Filename: ${data.filename}, Text Status: ${data.text_status}` })
      if (materialFileInputRef.current) materialFileInputRef.current.value = ''
      // Refresh session so materials section shows the new file without page reload
      await refetchSession()
    } catch (err) {
      setUploadMaterialFeedback({ type: 'error', message: `Failed to upload material: ${err.message}` })
    } finally {
      setLoading(false)
      if (materialFileInputRef.current) materialFileInputRef.current.value = ''
    }
  }

  const removeMaterialFile = (index) => {
    setMaterialFiles(prev => prev.filter((_, i) => i !== index))
  }

  // Helper function to detect Loom share URLs
  const isLoomShareURL = (url) => {
    if (!url) return false
    const urlLower = url.toLowerCase()
    return urlLower.includes('loom.com/share/') || urlLower.includes('www.loom.com/share/')
  }

  // Helper function to detect direct MP4 URLs
  const isDirectMediaURL = (url) => {
    if (!url) return false
    const urlLower = url.toLowerCase()
    return urlLower.endsWith('.mp4') || urlLower.endsWith('.webm') || urlLower.endsWith('.m4v')
  }

  const ingestVideoFromURL = async (url) => {
    if (!currentSession) {
      setAttachVideoFeedback({ type: 'error', message: 'Please select or create a session first' })
      return
    }

    clearFeedback(setAttachVideoFeedback)
    setLoading(true)

    try {
      const sessionId = currentSession.session ? currentSession.session.id : currentSession.id
      
      const response = await fetch(`${apiBaseUrl}/sessions/${sessionId}/video/from-url`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ url })
      })

      if (!response.ok) {
        const text = await response.text()
        try {
          const json = JSON.parse(text)
          setAttachVideoFeedback({ type: 'error', message: `Error ${response.status}: ${JSON.stringify(json, null, 2)}` })
        } catch {
          setAttachVideoFeedback({ type: 'error', message: `Error ${response.status}: ${text}` })
        }
        return
      }

      const data = await response.json()
      
      if (data.requires_upload) {
        // Loom URL - store video source and show guidance
        setLoomVideoSource(data.video_source)
        setAttachVideoFeedback({ type: 'info', message: data.message || 'Loom share URLs require manual upload. Please download the MP4 from Loom and upload it using the file input above.' })
      } else {
        // Direct URL - success
        setLoomVideoSource(null) // Clear any previous Loom source
        setAttachVideoFeedback({ type: 'success', message: data.message || 'Video ingested successfully. Transcription will begin shortly.' })
        setVideoUrl('') // Clear URL field
      }
      
      // Refresh session to get updated video sources
      if (currentSession) {
        await openSession(sessionId)
      }
    } catch (err) {
      setAttachVideoFeedback({ type: 'error', message: `Failed to ingest video from URL: ${err.message}` })
    } finally {
      setLoading(false)
    }
  }

  const attachVideo = async () => {
    if (!currentSession) {
      setAttachVideoFeedback({ type: 'error', message: 'Please select or create a session first' })
      return
    }
    if (!artifactId) {
      setAttachVideoFeedback({ type: 'error', message: 'Please create an artifact first' })
      return
    }

    const urlToCheck = embedUrl || videoUrl || mediaUrl
    if (!urlToCheck) {
      setAttachVideoFeedback({ type: 'error', message: 'Please enter a video URL' })
      return
    }

    // Smart URL detection
    if (isLoomShareURL(urlToCheck)) {
      // Loom share URL - use smart ingestion endpoint
      await ingestVideoFromURL(urlToCheck)
      return
    }

    if (isDirectMediaURL(urlToCheck)) {
      // Direct media URL - use smart ingestion endpoint
      await ingestVideoFromURL(urlToCheck)
      return
    }

    // Otherwise, use existing embed flow
    // Validate based on playback mode
    if (playbackMode === 'embed' && !embedUrl && !videoUrl) {
      setAttachVideoFeedback({ type: 'error', message: 'Please enter an embed URL' })
      return
    }
    if (playbackMode === 'direct' && !mediaUrl) {
      setAttachVideoFeedback({ type: 'error', message: 'Please enter a media URL (MP4/WebM)' })
      return
    }

    clearFeedback(setAttachVideoFeedback)
    setLoading(true)

    try {
      const requestBody = {
        provider: videoProvider,
        playback_mode: playbackMode
      }

      // Add appropriate URL based on mode
      if (playbackMode === 'embed') {
        requestBody.embed_url = embedUrl || videoUrl // Use embedUrl if set, fallback to videoUrl
        // Keep video_url for backward compatibility
        if (videoUrl) {
          requestBody.video_url = videoUrl
        }
      } else {
        requestBody.media_url = mediaUrl
        if (posterUrl) {
          requestBody.poster_url = posterUrl
        }
        if (durationSeconds) {
          requestBody.duration_seconds = parseInt(durationSeconds)
        }
      }

      const response = await fetch(`${apiBaseUrl}/artifacts/${artifactId}/video`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(requestBody)
      })

      if (!response.ok) {
        const text = await response.text()
        try {
          const json = JSON.parse(text)
          setAttachVideoFeedback({ type: 'error', message: `Error ${response.status}: ${JSON.stringify(json, null, 2)}` })
        } catch {
          setAttachVideoFeedback({ type: 'error', message: `Error ${response.status}: ${text}` })
        }
        return
      }

      const data = await response.json()
      setVideoId(data.id)
      setAttachVideoFeedback({ type: 'success', message: `Video attached! Video ID: ${data.id}` })
      // Clear form fields
      setVideoUrl('')
      setEmbedUrl('')
      setMediaUrl('')
      setPosterUrl('')
      setDurationSeconds('')
      // Refresh session to get updated video sources
      if (currentSession) {
        await openSession(currentSession.session.id)
      }
    } catch (err) {
      setAttachVideoFeedback({ type: 'error', message: `Failed to attach video: ${err.message}` })
    } finally {
      setLoading(false)
    }
  }

  const uploadTranscriptFile = async () => {
    if (!currentSession) {
      setSubmitTranscriptFeedback({ type: 'error', message: 'Please select or create a session first' })
      return
    }
    if (!transcriptFile) {
      setSubmitTranscriptFeedback({ type: 'error', message: 'Please select an MP4 file' })
      return
    }

    // Validate file type
    if (!transcriptFile.name.toLowerCase().endsWith('.mp4') && transcriptFile.type !== 'video/mp4') {
      setSubmitTranscriptFeedback({ type: 'error', message: 'File must be MP4 format' })
      return
    }

    clearFeedback(setSubmitTranscriptFeedback)
    setTranscriptFileUploading(true)
    setLoading(true)

    try {
      const sessionId = currentSession.session ? currentSession.session.id : currentSession.id
      
      const formData = new FormData()
      formData.append('file', transcriptFile)

      const response = await fetch(`${apiBaseUrl}/sessions/${sessionId}/video/transcript/upload`, {
        method: 'POST',
        body: formData
      })

      if (!response.ok) {
        const text = await response.text()
        try {
          const json = JSON.parse(text)
          setSubmitTranscriptFeedback({ type: 'error', message: `Error ${response.status}: ${JSON.stringify(json, null, 2)}` })
        } catch {
          setSubmitTranscriptFeedback({ type: 'error', message: `Error ${response.status}: ${text}` })
        }
        return
      }

      const data = await response.json()
      const jobId = data.job_id
      
      setSubmitTranscriptFeedback({ type: 'info', message: 'Transcription started. Waiting for completion...' })

      // Poll for transcription completion
      const pollInterval = setInterval(async () => {
        try {
          const statusResponse = await fetch(`${apiBaseUrl}/sessions/${sessionId}/transcript-jobs/${jobId}`)
          if (!statusResponse.ok) {
            clearInterval(pollInterval)
            setSubmitTranscriptFeedback({ type: 'error', message: `Failed to check transcription status: ${statusResponse.status}` })
            return
          }

          const statusData = await statusResponse.json()
          
          if (statusData.status === 'completed') {
            clearInterval(pollInterval)
            if (statusData.transcript_text) {
              setTranscriptText(statusData.transcript_text)
              setSubmitTranscriptFeedback({ type: 'success', message: 'Transcription completed! Transcript text has been populated.' })
              setTranscriptFile(null)
            } else {
              setSubmitTranscriptFeedback({ type: 'error', message: 'Transcription completed but no transcript text was returned' })
            }
          } else if (statusData.status === 'failed') {
            clearInterval(pollInterval)
            const errorMsg = statusData.error_message || 'Transcription failed'
            setSubmitTranscriptFeedback({ type: 'error', message: `Transcription failed: ${errorMsg}` })
          }
          // If still processing (queued, downloading, transcribing, saving), continue polling
        } catch (err) {
          clearInterval(pollInterval)
          setSubmitTranscriptFeedback({ type: 'error', message: `Failed to check transcription status: ${err.message}` })
        }
      }, 3000) // Poll every 3 seconds

      // Stop polling after 10 minutes (timeout)
      setTimeout(() => {
        clearInterval(pollInterval)
        if (transcriptFileUploading) {
          setSubmitTranscriptFeedback({ type: 'error', message: 'Transcription timeout - please check status manually' })
        }
      }, 600000) // 10 minutes

    } catch (err) {
      setSubmitTranscriptFeedback({ type: 'error', message: `Failed to upload transcript file: ${err.message}` })
    } finally {
      setTranscriptFileUploading(false)
      setLoading(false)
    }
  }

  const submitTranscript = async () => {
    if (!artifactId || !videoId) {
      setSubmitTranscriptFeedback({ type: 'error', message: 'Please attach a video first' })
      return
    }
    if (!transcriptText) {
      setSubmitTranscriptFeedback({ type: 'error', message: 'Please enter transcript text' })
      return
    }

    clearFeedback(setSubmitTranscriptFeedback)
    setLoading(true)

    try {
      const response = await fetch(`${apiBaseUrl}/artifacts/${artifactId}/video/${videoId}/transcript`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          transcript_text: transcriptText
        })
      })

      if (!response.ok) {
        const text = await response.text()
        try {
          const json = JSON.parse(text)
          setSubmitTranscriptFeedback({ type: 'error', message: `Error ${response.status}: ${JSON.stringify(json, null, 2)}` })
        } catch {
          setSubmitTranscriptFeedback({ type: 'error', message: `Error ${response.status}: ${text}` })
        }
        return
      }

      const data = await response.json()
      setSubmitTranscriptFeedback({ type: 'success', message: `Transcript submitted! Status: ${data.transcript_status}` })
      setTranscriptText('')
      // Refresh session to get updated video source with new transcript status
      if (currentSession) {
        const sessionId = currentSession.session ? currentSession.session.id : currentSession.id
        await openSession(sessionId)
      }
    } catch (err) {
      setSubmitTranscriptFeedback({ type: 'error', message: `Failed to submit transcript: ${err.message}` })
    } finally {
      setLoading(false)
    }
  }

  const fetchTranscriptJob = async (videoId) => {
    if (!artifactId || !videoId) {
      return null
    }

    try {
      const response = await fetch(`${apiBaseUrl}/artifacts/${artifactId}/video/${videoId}/transcript-job`)
      if (!response.ok) {
        return null
      }
      const data = await response.json()
      return data.job || null
    } catch (err) {
      console.error('Failed to fetch transcript job:', err)
      return null
    }
  }

  const uploadVideoFile = async () => {
    if (!currentSession) {
      setAttachVideoFeedback({ type: 'error', message: 'Please select or create a session first' })
      return
    }
    if (!videoFile) {
      setAttachVideoFeedback({ type: 'error', message: 'Please select an MP4 file' })
      return
    }

    // Validate file type
    if (!videoFile.name.toLowerCase().endsWith('.mp4') && videoFile.type !== 'video/mp4') {
      setAttachVideoFeedback({ type: 'error', message: 'File must be MP4 format' })
      return
    }

    clearFeedback(setAttachVideoFeedback)
    setVideoFileUploading(true)
    setLoading(true)

    try {
      const sessionId = currentSession.session ? currentSession.session.id : currentSession.id
      
      const formData = new FormData()
      formData.append('file', videoFile)

      const response = await fetch(`${apiBaseUrl}/sessions/${sessionId}/video/upload`, {
        method: 'POST',
        body: formData
      })

      if (!response.ok) {
        const text = await response.text()
        try {
          const json = JSON.parse(text)
          setAttachVideoFeedback({ type: 'error', message: `Error ${response.status}: ${JSON.stringify(json, null, 2)}` })
        } catch {
          setAttachVideoFeedback({ type: 'error', message: `Error ${response.status}: ${text}` })
        }
        return
      }

      const data = await response.json()
      setAttachVideoFeedback({ type: 'success', message: data.message || 'Video uploaded successfully. Transcription will begin shortly.' })
      setVideoFile(null)
      setLoomVideoSource(null) // Clear Loom source if video was uploaded
      
      // Refresh session to get updated video sources
      if (currentSession) {
        await openSession(sessionId)
      }
    } catch (err) {
      setAttachVideoFeedback({ type: 'error', message: `Failed to upload video: ${err.message}` })
    } finally {
      setVideoFileUploading(false)
      setLoading(false)
    }
  }

  const regenerateTranscript = async (videoId) => {
    if (!artifactId || !videoId) {
      return
    }

    setAttachVideoFeedback({ type: 'info', message: 'Regenerating transcript...' })
    setLoading(true)

    try {
      const response = await fetch(`${apiBaseUrl}/artifacts/${artifactId}/video/${videoId}/transcript-job/regenerate`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' }
      })

      if (!response.ok) {
        const text = await response.text()
        try {
          const json = JSON.parse(text)
          setAttachVideoFeedback({ type: 'error', message: `Error ${response.status}: ${JSON.stringify(json, null, 2)}` })
        } catch {
          setAttachVideoFeedback({ type: 'error', message: `Error ${response.status}: ${text}` })
        }
        return
      }

      const data = await response.json()
      setAttachVideoFeedback({ type: 'success', message: data.message || 'Transcript regeneration started' })
      
      // Refresh session to get updated status
      if (currentSession) {
        const sessionId = currentSession.session ? currentSession.session.id : currentSession.id
        await openSession(sessionId)
      }
    } catch (err) {
      setAttachVideoFeedback({ type: 'error', message: `Failed to regenerate transcript: ${err.message}` })
    } finally {
      setLoading(false)
    }
  }

  const askQuestion = async () => {
    if (!currentSession) {
      setAskQuestionFeedback({ type: 'error', message: 'Please select or create a session first' })
      return
    }
    if (!artifactId) {
      setAskQuestionFeedback({ type: 'error', message: 'Please create an artifact first' })
      return
    }
    if (!questionText) {
      setAskQuestionFeedback({ type: 'error', message: 'Please enter a question' })
      return
    }

    clearFeedback(setAskQuestionFeedback)
    setLoading(true)

    try {
      // Include video_time_seconds if available
      const requestBody = {
        question_text: questionText
      }
      if (currentVideoTime > 0) {
        requestBody.video_time_seconds = Math.floor(currentVideoTime)
      }

      const response = await fetch(`${apiBaseUrl}/artifacts/${artifactId}/questions`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(requestBody)
      })

      if (!response.ok) {
        const text = await response.text()
        try {
          const json = JSON.parse(text)
          setAskQuestionFeedback({ type: 'error', message: `Error ${response.status}: ${JSON.stringify(json, null, 2)}` })
        } catch {
          setAskQuestionFeedback({ type: 'error', message: `Error ${response.status}: ${text}` })
        }
        return
      }

      const data = await response.json()
      setCurrentAnswer(data)
      // Indicate if response was cached (200) or new (201)
      const isCached = response.status === 200
      const statusMsg = isCached 
        ? `Question answered (cached)! Status: ${data.answer.answer_status}`
        : `Question answered! Status: ${data.answer.answer_status}`
      setAskQuestionFeedback({ type: 'success', message: statusMsg })
      setQuestionText('')
      // Refresh questions list
      fetchQuestions()
    } catch (err) {
      setAskQuestionFeedback({ type: 'error', message: `Failed to ask question: ${err.message}` })
    } finally {
      setLoading(false)
    }
  }

  const fetchQuestions = async () => {
    if (!artifactId) {
      setQuestionHistoryFeedback({ type: 'error', message: 'Please create an artifact first' })
      return
    }

    clearFeedback(setQuestionHistoryFeedback)
    setLoading(true)

    try {
      const response = await fetch(`${apiBaseUrl}/artifacts/${artifactId}/questions`)

      if (!response.ok) {
        const text = await response.text()
        try {
          const json = JSON.parse(text)
          setQuestionHistoryFeedback({ type: 'error', message: `Error ${response.status}: ${JSON.stringify(json, null, 2)}` })
        } catch {
          setQuestionHistoryFeedback({ type: 'error', message: `Error ${response.status}: ${text}` })
        }
        return
      }

      const data = await response.json()
      // The API returns separate arrays, but answers are already matched by index
      // Create a map of question ID to answer for easy lookup
      const answerMap = new Map()
      if (data.answers && Array.isArray(data.answers)) {
        data.answers.forEach(answer => {
          if (answer && answer.question_id) {
            answerMap.set(answer.question_id, answer)
          }
        })
      }
      // Combine questions with their answers
      const questionsWithAnswers = (data.questions || []).map(q => {
        const answer = answerMap.get(q.id) || null
        return {
          ...q,
          answer: answer
        }
      })
      setQuestions(questionsWithAnswers)
      setQuestionHistoryFeedback({ type: 'info', message: `Loaded ${questionsWithAnswers.length} questions` })
    } catch (err) {
      setQuestionHistoryFeedback({ type: 'error', message: `Failed to fetch questions: ${err.message}` })
    } finally {
      setLoading(false)
    }
  }

  // Phase 3: Session functions
  const createSession = async () => {
    if (!sessionTitle) {
      setCreateSessionFeedback({ type: 'error', message: 'Please enter a session title' })
      return
    }

    clearFeedback(setCreateSessionFeedback)
    setLoading(true)

    try {
      const response = await fetch(`${apiBaseUrl}/sessions`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({
          title: sessionTitle
        })
      })

      if (!response.ok) {
        const text = await response.text()
        let message
        try {
          const json = JSON.parse(text)
          message = response.status === 409 || /already exists|unique name/i.test(json.error || '')
            ? 'A session with this name already exists. Please use a unique name.'
            : (json.error || `Error ${response.status}`)
        } catch {
          message = `Error ${response.status}: ${text}`
        }
        setCreateSessionFeedback({ type: 'error', message })
        return
      }

      const data = await response.json()
      setCreateSessionFeedback({ type: 'success', message: `Session created! ID: ${data.id}` })
      setSessionTitle('')
      // Creator is the logged-in user; set currentUser so UI shows creator mode
      if (authUser?.email) {
        setCurrentUser(authUser.email)
      }
      // Automatically open the newly created session in creator mode
      await openSession(data.id, 'creator')
    } catch (err) {
      setCreateSessionFeedback({ type: 'error', message: `Failed to create session: ${err.message}` })
    } finally {
      setLoading(false)
    }
  }

  const duplicateTitleMessage = 'A session with this name already exists. Please use a unique name.'

  const copySession = async (sourceSessionId, optionalTitle) => {
    if (apiBaseUrl == null || !sourceSessionId) return
    setCopyingSessionId(sourceSessionId)
    try {
      const base = apiBaseUrl.replace(/\/$/, '')
      const headers = { 'Content-Type': 'application/json' }
      if (acceptToken) headers['Authorization'] = `Bearer ${acceptToken}`
      const body = optionalTitle && optionalTitle.trim() ? JSON.stringify({ title: optionalTitle.trim() }) : undefined
      const res = await fetch(`${base}/api/sessions/${sourceSessionId}/copy`, {
        method: 'POST',
        credentials: 'include',
        headers,
        ...(body ? { body } : {})
      })
      const data = await res.json().catch(() => ({}))
      if (!res.ok) {
        const message = res.status === 409 || /already exists|unique name/i.test(data.error || '')
          ? duplicateTitleMessage
          : (data.error || `Copy failed: ${res.status}`)
        setCreateSessionFeedback({ type: 'error', message })
        return
      }
      setCopyModalSession(null)
      setCopyModalTitle('')
      await openSession(data.id, 'creator')
    } catch (err) {
      setCreateSessionFeedback({ type: 'error', message: `Failed to copy session: ${err.message}` })
    } finally {
      setCopyingSessionId(null)
    }
  }

  const saveRenameSession = async () => {
    if (!renameSessionId || apiBaseUrl == null) return
    const title = renameSessionTitle.trim()
    if (!title) {
      setCreateSessionFeedback({ type: 'error', message: 'Title cannot be empty' })
      return
    }
    setRenameSaving(true)
    setCreateSessionFeedback({ type: '', message: '' })
    try {
      const base = apiBaseUrl.replace(/\/$/, '')
      const headers = { 'Content-Type': 'application/json' }
      if (acceptToken) headers['Authorization'] = `Bearer ${acceptToken}`
      const res = await fetch(`${base}/api/sessions/${renameSessionId}`, {
        method: 'PATCH',
        credentials: 'include',
        headers,
        body: JSON.stringify({ title })
      })
      const data = await res.json().catch(() => ({}))
      if (!res.ok) {
        const message = res.status === 409 || /already exists|unique name/i.test(data.error || '')
          ? duplicateTitleMessage
          : (data.error || `Failed to rename: ${res.status}`)
        setCreateSessionFeedback({ type: 'error', message })
        return
      }
      setRenameSessionId(null)
      setRenameSessionTitle('')
      setMySessions(prev => prev.map(item => {
        const s = item.session || item
        if (s.id === renameSessionId) return { ...item, session: { ...s, title } }
        return item
      }))
      if (currentSession && (currentSession.session?.id === renameSessionId || currentSession.id === renameSessionId)) {
        setCurrentSession(prev => {
          if (!prev) return null
          if (prev.session) return { ...prev, session: { ...prev.session, title } }
          return { ...prev, title }
        })
      }
    } catch (err) {
      setCreateSessionFeedback({ type: 'error', message: `Failed to rename: ${err.message}` })
    } finally {
      setRenameSaving(false)
    }
  }

  const checkZoomTranscript = async () => {
    const url = zoomUrl?.trim()
    if (!url) {
      setZoomImportError('Paste a Zoom recording URL first')
      return
    }
    setZoomCheckingTranscript(true)
    setZoomTranscriptStatus(null)
    setZoomTranscriptMessage('')
    setZoomTranscriptTopic(null)
    setZoomImportError('')
    try {
      const params = new URLSearchParams({ zoom_url: url })
      const response = await fetch(`${apiBaseUrl}/zoom/transcript-status?${params}`, {
        headers: { 'X-Creator-Identity': creatorIdentity }
      })
      const data = await response.json()
      if (!response.ok) {
        setZoomTranscriptStatus(data.code ?? 'error')
        setZoomTranscriptMessage(data.message ?? '')
      } else {
        setZoomTranscriptStatus(data.status ?? 'error')
        setZoomTranscriptMessage(data.message ?? '')
        setZoomTranscriptTopic(data.topic ?? null)
        // Prefill session title with recording topic so user can confirm or edit (required before create)
        setZoomTitle((prev) => (prev?.trim() ? prev : (data.topic || 'Zoom Recording')))
      }
    } catch (err) {
      setZoomTranscriptStatus('error')
      setZoomTranscriptMessage(err.message || 'Could not check transcript status')
    } finally {
      setZoomCheckingTranscript(false)
    }
  }

  const createSessionFromZoom = async () => {
    const url = zoomUrl?.trim()
    if (!url) {
      setZoomImportError('Paste a Zoom recording URL')
      return
    }
    const title = zoomTitle?.trim()
    if (!title) {
      setZoomImportError('Session title is required. Enter a name for this session (you can use the proposed name above).')
      return
    }
    setZoomImporting(true)
    setZoomImportError('')
    try {
      const headers = { 'Content-Type': 'application/json', 'X-Creator-Identity': creatorIdentity }
      const body = JSON.stringify({ zoom_url: url, title })
      const response = await fetch(`${apiBaseUrl}/sessions/from-zoom`, { method: 'POST', headers, body, credentials: 'include' })
      const text = await response.text()
      let data = {}
      try {
        data = JSON.parse(text)
      } catch {
        // Server may return plain text (e.g. http.Error)
        data = { message: text || response.statusText }
      }
      if (!response.ok) {
        const msg = data.message || data.error || response.statusText
        if (response.status === 409) {
          setZoomImportError(msg || 'A session with this name already exists. Please use a unique name.')
        } else {
          setZoomImportError(msg)
        }
        if (data.code === 'transcript_processing') {
          setZoomTranscriptStatus('processing')
          setZoomTranscriptMessage(msg)
        } else if (data.code === 'transcript_not_available') {
          setZoomTranscriptStatus('not_available')
          setZoomTranscriptMessage(msg)
        } else if (data.code === 'zoom_share_link') {
          setZoomTranscriptStatus('zoom_share_link')
          setZoomTranscriptMessage(msg)
        }
        return
      }
      setZoomUrl('')
      setZoomTitle('')
      setZoomImportError('')
      setZoomTranscriptStatus(null)
      setZoomTranscriptMessage('')
      setZoomTranscriptTopic(null)
      await openSession(data.id, 'creator', true)
    } catch (err) {
      setZoomImportError(err.message || 'Failed to import from Zoom')
    } finally {
      setZoomImporting(false)
    }
  }

  const disconnectZoom = async () => {
    try {
      await fetch(`${apiBaseUrl}/api/zoom/disconnect`, {
        method: 'POST',
        headers: { 'X-Creator-Identity': creatorIdentity }
      })
      setZoomConnection(null)
      setZoomRecordings([])
    } catch {
      // ignore
    }
  }

  const fetchZoomRecordings = async () => {
    setZoomRecordingsLoading(true)
    setZoomRecordingsError('')
    try {
      // No from/to = fetch all recordings for the user (last 2 years, sorted most recent first)
      const res = await fetch(`${apiBaseUrl}/api/zoom/recordings`, {
        headers: { 'X-Creator-Identity': creatorIdentity }
      })
      const data = await res.json()
      if (!res.ok) {
        setZoomRecordingsError(data.message || 'Failed to fetch recordings')
        setZoomRecordings([])
        return
      }
      const items = (data.items || []).slice()
      items.sort((a, b) => {
        const ta = a.start_time ? new Date(a.start_time).getTime() : 0
        const tb = b.start_time ? new Date(b.start_time).getTime() : 0
        return tb - ta // most recent first
      })
      setZoomRecordings(items)
    } catch (err) {
      setZoomRecordingsError(err.message || 'Failed to fetch recordings')
      setZoomRecordings([])
    } finally {
      setZoomRecordingsLoading(false)
    }
  }

  const openZoomImportModal = (rec) => {
    setZoomImportModalRec(rec)
    setZoomImportSessionName(rec.meeting_topic || 'Zoom Recording')
    setZoomImportModalError('')
  }

  const closeZoomImportModal = () => {
    setZoomImportModalRec(null)
    setZoomImportSessionName('')
    setZoomImportModalError('')
  }

  const importFromZoomRecording = async (rec, sessionTitle) => {
    const baseTitle = sessionTitle ?? rec.meeting_topic ?? 'Zoom Recording'
    const title = (baseTitle || 'Zoom Recording').trim()
    setZoomImporting(true)
    setZoomImportError('')
    setZoomImportModalError('')
    try {
      const res = await fetch(`${apiBaseUrl}/api/zoom/import`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-Creator-Identity': creatorIdentity },
        credentials: 'include',
        body: JSON.stringify({
          title,
          meeting_uuid: rec.meeting_uuid,
          instance_uuid: rec.instance_uuid || rec.meeting_uuid
        })
      })
      const data = await res.json().catch(() => ({}))
      if (!res.ok) {
        const msg = res.status === 422 && data.message
          ? data.message
          : (data.message || 'Failed to start import')
        setZoomImportModalError(res.status === 409 ? (data.message || 'A session with this name already exists. Please use a unique name.') : msg)
        return
      }
      closeZoomImportModal()
      setZoomImportToast({ message: 'Import started' })
      setTimeout(() => setZoomImportToast(null), 3000)
      await openSession(data.id, 'creator', true)
    } catch (err) {
      setZoomImportModalError(err.message || 'Failed to import')
    } finally {
      setZoomImporting(false)
    }
  }

  const disconnectTeams = async () => {
    try {
      await fetch(`${apiBaseUrl}/api/teams/disconnect`, {
        method: 'POST',
        headers: { 'X-Creator-Identity': creatorIdentity }
      })
      setTeamsConnection(null)
      setTeamsRecordings([])
    } catch (_) { /* ignore */ }
  }

  const fetchTeamsRecordings = async () => {
    setTeamsRecordingsLoading(true)
    setTeamsRecordingsError('')
    try {
      const res = await fetch(`${apiBaseUrl}/api/teams/recordings`, {
        headers: { 'X-Creator-Identity': creatorIdentity }
      })
      const data = await res.json().catch(() => ({}))
      if (!res.ok) {
        setTeamsRecordingsError(data.message || 'Failed to fetch recordings')
        setTeamsRecordings([])
        return
      }
      const items = (data.items || []).slice()
      items.sort((a, b) => {
        const ta = a.start_time ? new Date(a.start_time).getTime() : 0
        const tb = b.start_time ? new Date(b.start_time).getTime() : 0
        return tb - ta
      })
      setTeamsRecordings(items)
    } catch (err) {
      setTeamsRecordingsError(err.message || 'Failed to fetch recordings')
      setTeamsRecordings([])
    } finally {
      setTeamsRecordingsLoading(false)
    }
  }

  const openTeamsImportModal = (rec) => {
    setTeamsImportModalRec(rec)
    setTeamsImportSessionName(rec.subject || 'Teams Recording')
    setTeamsImportModalError('')
  }

  const closeTeamsImportModal = () => {
    setTeamsImportModalRec(null)
    setTeamsImportSessionName('')
    setTeamsImportModalError('')
  }

  const importFromTeamsRecording = async (rec, sessionTitle) => {
    const baseTitle = sessionTitle ?? rec.subject ?? 'Teams Recording'
    const title = (baseTitle || 'Teams Recording').trim()
    setTeamsImporting(true)
    setTeamsImportError('')
    setTeamsImportModalError('')
    try {
      const res = await fetch(`${apiBaseUrl}/api/teams/import`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-Creator-Identity': creatorIdentity },
        credentials: 'include',
        body: JSON.stringify({
          title,
          meeting_id: rec.meeting_id,
          recording_id: rec.recording_id
        })
      })
      const data = await res.json().catch(() => ({}))
      if (!res.ok) {
        const msg = res.status === 422 && data.message
          ? data.message
          : (data.message || 'Failed to start import')
        setTeamsImportModalError(res.status === 409 ? (data.message || 'A session with this name already exists. Please use a unique name.') : msg)
        return
      }
      closeTeamsImportModal()
      setTeamsImportToast({ message: 'Import started' })
      setTimeout(() => setTeamsImportToast(null), 3000)
      await openSession(data.id, 'creator', true)
    } catch (err) {
      setTeamsImportModalError(err.message || 'Failed to import')
    } finally {
      setTeamsImporting(false)
    }
  }

  // Ensure creator identity exists (for Zoom OAuth)
  useEffect(() => {
    if (!creatorIdentity) {
      const uuid = 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (c) => {
        const r = (Math.random() * 16) | 0
        const v = c === 'x' ? r : (r & 0x3) | 0x8
        return v.toString(16)
      })
      setCreatorIdentity(uuid)
    }
  }, [])

  // Handle Zoom / Teams OAuth callbacks: ?zoom=connected|error, ?teams=connected|error
  useEffect(() => {
    const urlParams = new URLSearchParams(window.location.search)
    const zoom = urlParams.get('zoom')
    const teams = urlParams.get('teams')
    const ci = urlParams.get('creator_identity')
    const message = urlParams.get('message')
    if (zoom === 'connected' && ci) {
      setCreatorIdentity(ci)
      setZoomConnection({ zoom_user_email: null, zoom_user_id: null }) // will be filled by /me
      // Restore create-session flow if user left to connect Zoom from "Create new session" -> "From Zoom"
      try {
        if (sessionStorage.getItem('talkback.zoom_return_to_create') === '1') {
          sessionStorage.removeItem('talkback.zoom_return_to_create')
          setSessionMode('create')
          setCreateSource(CREATE_SOURCE.ZOOM)
        }
      } catch (_) { /* ignore */ }
      window.history.replaceState({}, '', window.location.pathname + window.location.hash)
    } else if (zoom === 'error') {
      setZoomImportError(message === 'missing_code_or_state' ? 'Zoom sign-in was cancelled or incomplete.' : message === 'server_not_configured' ? 'Zoom is not configured on the server.' : message === 'exchange_failed' ? 'Could not complete Zoom sign-in.' : message === 'save_failed' ? 'Could not save Zoom connection.' : message || 'Zoom sign-in failed.')
      window.history.replaceState({}, '', window.location.pathname + window.location.hash)
    }
    if (teams === 'connected' && ci) {
      setCreatorIdentity(ci)
      const base = (apiBaseUrl || getDefaultApiBaseUrl()).replace(/\/$/, '')
      fetch(`${base}/api/teams/status`, { headers: { 'X-Creator-Identity': ci } })
        .then((r) => r.json())
        .then((data) => {
          if (data.enabled) {
            setTeamsApiEnabled(true)
            if (data.connected) {
              setTeamsConnection({
                teams_email: data.teams_email || null,
                teams_user_id: data.teams_user_id || null
              })
            }
          }
        })
        .catch(() => {})
      try {
        if (sessionStorage.getItem('talkback.teams_return_to_create') === '1') {
          sessionStorage.removeItem('talkback.teams_return_to_create')
          setSessionMode('create')
          setCreateSource(CREATE_SOURCE.TEAMS)
        }
      } catch (_) { /* ignore */ }
      window.history.replaceState({}, '', window.location.pathname + window.location.hash)
    } else if (teams === 'error') {
      setTeamsImportError(message === 'missing_code_or_state' ? 'Teams sign-in was cancelled or incomplete.' : message === 'server_not_configured' ? 'Teams is not configured on the server.' : message === 'exchange_failed' ? 'Could not complete Teams sign-in.' : message === 'save_failed' ? 'Could not save Teams connection.' : message || 'Teams sign-in failed.')
      window.history.replaceState({}, '', window.location.pathname + window.location.hash)
    }
  }, [])

  // Fetch Zoom connection status when creator identity is set (uses /api/zoom/status)
  useEffect(() => {
    if (!creatorIdentity || apiBaseUrl == null) return
    const ac = new AbortController()
    fetch(`${apiBaseUrl}/api/zoom/status`, {
      signal: ac.signal,
      headers: { 'X-Creator-Identity': creatorIdentity }
    })
      .then((res) => res.json())
      .then((data) => {
        if (data.connected) {
          setZoomConnection({
            zoom_user_email: data.zoom_email || data.zoom_user_email || null,
            zoom_user_id: data.zoom_user_id || null
          })
        } else {
          setZoomConnection(null)
        }
      })
      .catch(() => setZoomConnection(null))
    return () => ac.abort()
  }, [creatorIdentity, apiBaseUrl])

  // Fetch Teams feature flag + connection (GET /api/teams/status)
  useEffect(() => {
    if (!creatorIdentity || apiBaseUrl == null) return
    const ac = new AbortController()
    fetch(`${apiBaseUrl}/api/teams/status`, {
      signal: ac.signal,
      headers: { 'X-Creator-Identity': creatorIdentity }
    })
      .then((res) => res.json())
      .then((data) => {
        setTeamsApiEnabled(data.enabled === true)
        if (data.enabled && data.connected) {
          setTeamsConnection({
            teams_email: data.teams_email || null,
            teams_user_id: data.teams_user_id || null
          })
        } else {
          setTeamsConnection(null)
        }
      })
      .catch(() => {
        setTeamsApiEnabled(false)
        setTeamsConnection(null)
      })
    return () => ac.abort()
  }, [creatorIdentity, apiBaseUrl])

  // Persist API base URL only when debug mode is on (avoids leaking localhost into production)
  useEffect(() => {
    if (!debugMode) return
    try {
      if (apiBaseUrl) localStorage.setItem(API_BASE_URL_STORAGE_KEY, apiBaseUrl)
    } catch (_) { /* ignore */ }
  }, [debugMode, apiBaseUrl])

  // On mount (and when auth loads): apply api from URL; open session when URL has /app/sessions/:id or ?session= (deep link).
  // Path session id wins over ?session= when both are set. Requires auth — do not fetch sessions on the login screen.
  // popstateNavKey: browser history. urlKey is not used here — participant/admin URL normalization bumps urlKey only to re-render.
  useEffect(() => {
    if (!authUser) return

    const nav = parseSessionNavigationFromLocation(window.location)
    const urlParams = new URLSearchParams(window.location.search)
    const apiFromUrl = nav.apiFromQuery || urlParams.get('api_base')
    const sessionId = nav.sessionId
    const mode = nav.mode // 'edit' or 'view' | 'admin' | null

    let apiOriginForSession = null
    if (apiFromUrl) {
      try {
        const u = new URL(apiFromUrl)
        apiOriginForSession = u.origin
        setApiBaseUrl(u.origin)
      } catch (_) { /* ignore invalid */ }
    }

    if (sessionId) {
      // URL contains a session: go to that session (deep link, legacy ?session=, or user navigated here)
      if (mode === 'view') {
        setSessionUserMode('participant')
        setCurrentUser('participant')
        setViewMode('session')
      } else if (mode !== 'edit') {
        if (authUser?.global_role === 'participant') {
          setSessionUserMode('participant')
          setCurrentUser('participant')
          setViewMode('session')
        } else {
          setSessionUserMode('creator')
        }
      } else {
        setSessionUserMode('creator')
      }

      if (mode === 'edit') {
        openSession(sessionId, 'creator', false, apiOriginForSession)
      } else if (mode === 'view') {
        openSession(sessionId, 'participant', false, apiOriginForSession)
      } else {
        const defaultMode = authUser?.global_role === 'participant' ? 'participant' : 'creator'
        openSession(sessionId, defaultMode, false, apiOriginForSession)
      }
    } else {
      // No session in URL: default view — no session selected; creators/admins see session list, participants see "sessions you're part of"
      setCurrentSession(null)
      clearFeedback(setSessionSelectFeedback)
      setViewMode('session')
      if (authUser?.global_role === 'participant') {
        setSessionUserMode('participant')
        setCurrentUser('participant')
      } else {
        setSessionUserMode(null)
        setCurrentUser('')
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [authUser, popstateNavKey])

  const openSession = async (sessionId, forceMode = null, stayInSessionView = false, overrideApiBaseUrl = null, isRefetch = false, overrideParticipantRef = null) => {
    if (!isRefetch) setLoading(true)
    clearFeedback(setSessionSelectFeedback)

    const baseUrl = overrideApiBaseUrl != null ? overrideApiBaseUrl : apiBaseUrl
    let sid = typeof sessionId === 'string' ? sessionId.trim() : String(sessionId || '')
    try {
      sid = decodeURIComponent(sid)
    } catch (_) { /* keep sid */ }

    // Switching sessions: drop previous session immediately so UI cannot show stale content
    if (!isRefetch) {
      setCurrentSession((prev) => {
        const prevId = prev?.session?.id ?? prev?.id
        if (!prevId || prevId === sid) return prev
        return null
      })
    }

    if (!isLikelySessionId(sid)) {
      setSessionSelectFeedback({ type: 'error', message: 'This link does not contain a valid session id.' })
      setViewMode('session')
      if (!isRefetch) setLoading(false)
      return
    }

    // Participants (join-only role) must never get creator mode, even if URL has ?mode=edit
    if (forceMode === 'creator' && authUser?.global_role === 'participant') {
      forceMode = 'participant'
    }

    // Set mode immediately based on forceMode, even if API call fails
    if (forceMode === 'participant') {
      setSessionUserMode('participant')
      setCurrentUser('participant')
      if (overrideParticipantRef) setParticipantRef(overrideParticipantRef)
    } else if (forceMode === 'creator') {
      setSessionUserMode('creator')
    }

    try {
      // When in participant mode, send participant_ref so backend can return unread_material_ids for "new document" marker.
      // Use overrideParticipantRef when opening from invite flow (register/login) since participantRef state may not have updated yet.
      const isParticipant = forceMode === 'participant' || (typeof sessionUserMode !== 'undefined' && sessionUserMode === 'participant')
      const effectiveParticipantRef = overrideParticipantRef ?? participantRef ?? authUser?.email
      const headers = {}
      if (isParticipant && effectiveParticipantRef) {
        headers['X-Participant-Ref'] = effectiveParticipantRef
      }
      // Always fetch fresh session so video_access_url (presigned) is current; avoid cached response on reload.
      // Use credentials so refetch after markMaterialsSeen sends cookie and returns updated unread_material_ids.
      const response = await fetch(`${baseUrl}/sessions/${sid}`, { headers, cache: 'no-store', credentials: 'include' })
      if (!response.ok) {
        setSessionSelectFeedback({ type: 'error', message: sessionLoadMessageForStatus(response.status) })
        setViewMode('session')
        if (!isRefetch) setCurrentSession(null)
        if (!isRefetch) setLoading(false)
        return
      }

      const data = await response.json()
      setCurrentSession(data)
      if (isRefetch) {
        // Preserve user's video selection when refetching; resolve by ref so we get fresh object (transcript etc.) from new data
        const primary = data.primary_video ?? (data.video_sources?.length > 0 ? data.video_sources[0] : null)
        const sources = data.video_sources ?? []
        const preferredId = selectedVideoIdRef.current
        const found = preferredId ? sources.find(vs => String(vs.id) === String(preferredId)) : null
        const resolved = found ?? primary
        if (resolved) {
          if (!found) selectedVideoIdRef.current = resolved?.id ?? null
          setSelectedVideo(resolved)
          setVideoId(resolved?.id ?? null)
        }
      } else {
        // When session has primary video (R2/local), use that for playback; clear any stale Zoom selection so we don't show 410
        if (data?.session?.primary_video_artifact_id) {
          selectedVideoIdRef.current = null
          setSelectedVideo(null)
          setVideoId(null)
        }
      }
      // If session has no video_sources yet on initial load (e.g. Zoom import just finished or refresh race), retry once after a short delay so video player appears.
      // Guard with !isRefetch: refetches triggered by WebSocket events must not queue a new retry or PPTX-only sessions loop forever.
      if (!isRefetch && data.session && (!data.video_sources || data.video_sources.length === 0) && !data.session?.primary_video_artifact_id) {
        const loadedId = data.session.id || sid
        setTimeout(() => {
          fetch(`${baseUrl}/sessions/${sid}`, { headers, cache: 'no-store', credentials: 'include' })
            .then((r) => r.ok ? r.json() : null)
            .then((retryData) => {
              const currentId = loadedId
              setCurrentSession((prev) => {
                const prevId = prev?.session?.id || prev?.id
                if (prevId !== currentId) return prev
                if (retryData?.video_sources?.length > 0 && !retryData?.session?.primary_video_artifact_id) {
                  const primary = retryData.primary_video ?? retryData.video_sources[0]
                  queueMicrotask(() => {
                    setVideoId(primary.id)
                    setSelectedVideo(primary)
                  })
                  return retryData
                }
                if (retryData) return retryData
                return prev
              })
            })
            .catch(() => {})
        }, 2500)
      }
      
      // Automatically set currentUser based on mode:
      // - Creator mode: use session.created_by (if available) to match backend logic
      // - Participant mode: use a non-matching identifier
      if (forceMode === 'creator') {
        // Set currentUser to session.created_by to ensure creator mode
        if (data.session && data.session.created_by) {
          setCurrentUser(data.session.created_by)
        } else {
          // Fallback: use a default creator identifier
          setCurrentUser('creator')
        }
        setSessionUserMode('creator')
        // Update URL to reflect creator mode
        window.history.replaceState({}, '', buildCanonicalSessionUrl(sid, { mode: 'edit' }))
      } else if (forceMode === 'participant') {
        // Set currentUser to something that won't match created_by
        setCurrentUser('participant')
        setSessionUserMode('participant')
        // Update URL to reflect participant mode (preserve api= only if already in URL, e.g. from shared link)
        const apiQ = new URLSearchParams(window.location.search).get('api')
        window.history.replaceState({}, '', buildCanonicalSessionUrl(sid, { mode: 'view', api: apiQ || undefined }))
      } else {
        // No explicit mode, determine from URL or default to creator (unless user is participant role)
        const urlParams = new URLSearchParams(window.location.search)
        const urlMode = urlParams.get('mode')
        const mustBeParticipant = authUser?.global_role === 'participant'

        if (urlMode === 'view' || mustBeParticipant) {
          setCurrentUser('participant')
          setSessionUserMode('participant')
          const apiQ = urlParams.get('api')
          window.history.replaceState({}, '', buildCanonicalSessionUrl(sid, { mode: 'view', api: apiQ || undefined }))
        } else {
          // Default to creator mode
          if (data.session && data.session.created_by) {
            setCurrentUser(data.session.created_by)
          } else {
            setCurrentUser('creator')
          }
          setSessionUserMode('creator')
          window.history.replaceState({}, '', buildCanonicalSessionUrl(sid, { mode: 'edit' }))
        }
      }
      
      // Set artifact ID if there are artifacts in the session
      if (data.artifacts && data.artifacts.length > 0) {
        setArtifactId(data.artifacts[0].id)
      }
      // Pre-populate video selection only on initial open (not refetch)
      if (!isRefetch) {
        const primaryVideo = data.primary_video ?? (data.video_sources?.length > 0 ? data.video_sources[0] : null)
        if (primaryVideo) {
          selectedVideoIdRef.current = primaryVideo.id
          setVideoId(primaryVideo.id)
          setSelectedVideo(primaryVideo)
        } else {
          selectedVideoIdRef.current = null
          setVideoId(null)
          setSelectedVideo(null)
        }
      }
      setViewMode('session')
      setSessionIdInput('') // Clear input
      
      // Load session questions
      fetchSessionQuestions(sid)
      setSessionSelectFeedback({ type: 'success', message: `Session loaded: ${data.session.title}` })
    } catch (err) {
      setSessionSelectFeedback({ type: 'error', message: err?.message ? `Could not load session: ${err.message}` : 'Could not load session.' })
      if (!isRefetch) setCurrentSession(null)
    } finally {
      if (!isRefetch) setLoading(false)
    }
  }

  const acceptPendingInvitation = useCallback(async (invitationId) => {
    if (apiBaseUrl == null || !invitationId) return
    setAcceptingInvitationId(invitationId)
    try {
      const base = apiBaseUrl.replace(/\/$/, '')
      const headers = { 'Content-Type': 'application/json' }
      if (acceptToken) headers['Authorization'] = `Bearer ${acceptToken}`
      const res = await fetch(`${base}/api/invitations/${invitationId}/accept`, {
        method: 'POST',
        credentials: 'include',
        headers,
        body: JSON.stringify({})
      })
      const data = await res.json().catch(() => ({}))
      if (res.ok && data.session_id) {
        setPendingInvitations((prev) => prev.filter((inv) => inv.id !== invitationId))
        await openSession(data.session_id, 'participant')
      }
    } finally {
      setAcceptingInvitationId(null)
    }
  }, [apiBaseUrl, openSession, acceptToken])

  const inviteUserToSession = async () => {
    const email = inviteEmail?.trim()?.toLowerCase()
    if (!email || !currentSession?.session?.id) return
    if (!isValidEmailFormat(email)) {
      setInviteFeedback({ type: 'error', message: 'Please enter a valid email address.' })
      return
    }
    setInviteLoading(true)
    setInviteFeedback({ type: '', message: '' })
    setLastInvitationDraft(null)
    try {
      const base = apiBaseUrl.replace(/\/$/, '')
      const response = await fetch(`${base}/api/sessions/${currentSession.session.id}/invitations`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, role: inviteRole || 'participant' })
      })
      const data = response.ok ? await response.json().catch(() => ({})) : await response.json().catch(() => ({}))
      if (response.status === 201) {
        setInviteEmail('')
        const inv = data?.invitation
        setLastInvitationDraft(inv || null)
        setInviteFeedback({ type: 'success', message: 'Invitation created. Use "Open email draft" in the Invitations table to open in your email app.' })
        setTimeout(() => setInviteFeedback({ type: '', message: '' }), 5000)
        fetchSessionInvitations(currentSession.session.id)
      } else if (response.status === 409) {
        setInviteFeedback({ type: 'error', message: (data && data.error) || 'That user is already a member of this session.' })
      } else {
        setInviteFeedback({ type: 'error', message: (data && data.error) || 'Failed to send invitation.' })
      }
    } catch (err) {
      setInviteFeedback({ type: 'error', message: err?.message || 'Failed to send invitation.' })
    } finally {
      setInviteLoading(false)
    }
  }

  const fetchSessionInvitations = useCallback(async (sessionId) => {
    if (!sessionId || !authUser) return
    try {
      const base = apiBaseUrl.replace(/\/$/, '')
      const res = await fetch(`${base}/api/sessions/${sessionId}/invitations`, { credentials: 'include' })
      if (res.ok) {
        const json = await res.json()
        setSessionInvitations(json.invitations || [])
      } else {
        setSessionInvitations([])
      }
    } catch (_) {
      setSessionInvitations([])
    }
  }, [apiBaseUrl, authUser])

  useEffect(() => {
    const id = currentSession?.session?.id ?? currentSession?.id
    if (id && authUser) fetchSessionInvitations(id)
    else setSessionInvitations([])
  }, [currentSession?.session?.id, currentSession?.id, authUser, fetchSessionInvitations])

  const refetchSession = useCallback(async (overrideSessionId) => {
    const id = overrideSessionId ?? currentSession?.session?.id ?? currentSession?.id
    if (!id) return
    await openSession(id, sessionUserMode, true, null, true)
  }, [currentSession?.session?.id, currentSession?.id, sessionUserMode, openSession])

  const setPrimaryVideoSource = useCallback(async (videoSourceId) => {
    const sessionId = currentSession?.session?.id ?? currentSession?.id
    if (!sessionId || apiBaseUrl == null || !videoSourceId) return
    const base = apiBaseUrl.replace(/\/$/, '')
    const res = await fetch(`${base}/api/sessions/${sessionId}/set-primary-video`, {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ video_source_id: videoSourceId })
    })
    if (res.ok) await refetchSession()
  }, [currentSession?.session?.id, currentSession?.id, apiBaseUrl, refetchSession])

  const markMaterialsSeen = useCallback(async (materialIds) => {
    const sessionId = currentSession?.session?.id || currentSession?.id
    const effectiveParticipantRef = participantRef || authUser?.email
    const ids = Array.isArray(materialIds) ? materialIds.map((id) => String(id)).filter(Boolean) : []
    if (!sessionId || !effectiveParticipantRef || ids.length === 0) return
    try {
      const res = await fetch(`${apiBaseUrl}/sessions/${sessionId}/materials/seen`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ participant_ref: effectiveParticipantRef, material_ids: ids })
      })
      if (!res.ok) return
      await refetchSession()
    } catch (_) { /* ignore */ }
  }, [currentSession?.session?.id, currentSession?.id, participantRef, authUser?.email, apiBaseUrl, refetchSession])

  const loadSessionById = async (mode = null) => {
    if (!sessionIdInput.trim()) {
      setSessionSelectFeedback({ type: 'error', message: 'Please enter a session ID' })
      return
    }
    await openSession(sessionIdInput.trim(), mode)
  }

  const joinSession = async () => {
    if (!currentSession) {
      setJoinSessionFeedback({ type: 'error', message: 'No session selected' })
      return
    }
    if (!participantRef) {
      setJoinSessionFeedback({ type: 'error', message: authUser ? 'Your email is required to join this session.' : 'Please log in to join this session.' })
      return
    }

    clearFeedback(setJoinSessionFeedback)
    setLoading(true)

    try {
      const response = await fetch(`${apiBaseUrl}/sessions/${currentSession.session.id}/participants`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          participant_ref: participantRef
        })
      })

      if (!response.ok) {
        const text = await response.text()
        try {
          const json = JSON.parse(text)
          setJoinSessionFeedback({ type: 'error', message: `Error ${response.status}: ${JSON.stringify(json, null, 2)}` })
        } catch {
          setJoinSessionFeedback({ type: 'error', message: `Error ${response.status}: ${text}` })
        }
        return
      }

      setJoinSessionFeedback({ type: 'success', message: `Joined session as ${participantRef}` })
      await refetchSession()
    } catch (err) {
      setJoinSessionFeedback({ type: 'error', message: `Failed to join session: ${err.message}` })
    } finally {
      setLoading(false)
    }
  }

  const askSessionQuestion = async () => {
    if (!currentSession) {
      setAskQuestionFeedback({ type: 'error', message: 'No session selected' })
      return
    }
    if (!questionText) {
      setAskQuestionFeedback({ type: 'error', message: 'Please enter a question' })
      return
    }

    await submitSessionQuestion(questionText, 'text', replyingToQuestionId)
  }

  const submitSessionQuestion = async (text, askedVia = 'text', parentQuestionId = null) => {
    clearFeedback(setAskQuestionFeedback)
    setLoading(true)

    try {
      const body = { question_text: text, asked_via: askedVia }
      if (parentQuestionId) body.parent_question_id = parentQuestionId
      const response = await fetch(`${apiBaseUrl}/api/sessions/${currentSession.session.id}/ask`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body)
      })

      if (!response.ok) {
        const respText = await response.text()
        let msg = `Error ${response.status}: ${respText}`
        if (response.status === 401) {
          msg = 'Please log in to ask a question.'
        } else {
          try {
            const json = JSON.parse(respText)
            msg = json.message || json.error || msg
          } catch { /* ignore */ }
        }
        setAskQuestionFeedback({ type: 'error', message: msg })
        return
      }

      const data = await response.json()
      const isCached = response.status === 200
      setCurrentAnswer({
        question: { question_text: data.question.question_text, created_at: data.question.created_at },
        answer: {
          answer_text: data.answer.answer_text,
          answer_status: 'answered',
          confidence: 1,
          citations: (data.answer.citations || []).map(c => ({
            citation_id: c.citation_id,
            chunk_id: c.chunk_id,
            source_type: c.source_type,
            source_id: c.source_id,
            anchor: c.anchor,
            label: c.label || (c.source_type + ' ' + (c.anchor?.start_ms != null ? formatCitationAnchor(c.anchor) : c.anchor?.block != null ? `block ${c.anchor.block}` : c.chunk_id)),
            excerpt: (c.excerpt || c.snippet || '').slice(0, 300),
            navigation: c.navigation
          }))
        }
      })
      setAskQuestionFeedback({ type: 'success', message: isCached ? 'Question answered (cached)!' : 'Question answered!' })
      setQuestionText('')
      setReplyingToQuestionId(null)
      await fetchSessionQuestions(currentSession.session.id)
    } catch (err) {
      setAskQuestionFeedback({ type: 'error', message: `Failed to ask question: ${err.message}` })
    } finally {
      setLoading(false)
    }
  }

  const formatCitationAnchor = (anchor) => {
    if (anchor.start_ms == null || anchor.end_ms == null) return ''
    const m1 = Math.floor(anchor.start_ms / 60000)
    const s1 = Math.floor((anchor.start_ms % 60000) / 1000)
    const m2 = Math.floor(anchor.end_ms / 60000)
    const s2 = Math.floor((anchor.end_ms % 60000) / 1000)
    return `${m1}:${String(s1).padStart(2, '0')}–${m2}:${String(s2).padStart(2, '0')}`
  }

  const toggleVoiceRecording = async () => {
    if (!currentSession) {
      setVoiceFeedback({ type: 'error', message: 'No session selected' })
      return
    }

    clearFeedback(setVoiceFeedback)

    // Stop recording
    if (voiceRecording) {
      try {
        // Immediately show "processing" state while MediaRecorder flushes/stops and we begin upload.
        setVoiceUploading(true)
        if (mediaRecorder && mediaRecorder.state !== 'inactive') {
          // Ensure we flush any buffered audio before stopping.
          try { mediaRecorder.requestData() } catch { /* ignore */ }
          mediaRecorder.stop()
        }
      } catch (err) {
        setVoiceFeedback({ type: 'error', message: `Failed to stop recording: ${err.message}` })
        cleanupVoiceMedia()
        setVoiceUploading(false)
      }
      return
    }

    // Start recording — pause video first so it doesn't compete with the mic
    setIsVideoPlaying(false)
    try {
      if (!navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) {
        setVoiceFeedback({ type: 'error', message: 'Microphone is not supported in this browser.' })
        return
      }

      const stream = await navigator.mediaDevices.getUserMedia({ audio: true })
      setMediaStream(stream)

      if (!window.MediaRecorder) {
        setVoiceFeedback({ type: 'error', message: 'MediaRecorder is not supported in this browser.' })
        stream.getTracks().forEach(t => t.stop())
        setMediaStream(null)
        return
      }

      // Prefer Opus in WebM/OGG when available.
      const preferredTypes = [
        'audio/webm;codecs=opus',
        'audio/ogg;codecs=opus',
        'audio/webm',
        'audio/ogg'
      ]
      const chosenType = preferredTypes.find(t => window.MediaRecorder.isTypeSupported && window.MediaRecorder.isTypeSupported(t))
      const recorder = chosenType ? new MediaRecorder(stream, { mimeType: chosenType }) : new MediaRecorder(stream)
      setMediaRecorder(recorder)
      voiceRecorderRef.current = recorder
      voiceChunksRef.current = []
      setVoiceRecording(true)

      recorder.ondataavailable = (e) => {
        if (e.data && e.data.size > 0) {
          voiceChunksRef.current.push(e.data)
        }
      }

      recorder.onstop = async () => {
        if (voiceSilenceIntervalRef.current) {
          clearInterval(voiceSilenceIntervalRef.current)
          voiceSilenceIntervalRef.current = null
        }
        try {
          const ctx = voiceContextRef.current
          if (ctx && ctx.state !== 'closed') ctx.close()
          voiceContextRef.current = null
        } catch { /* ignore */ }
        voiceRecorderRef.current = null
        setVoiceRecording(false)
        const chunks = voiceChunksRef.current || []
        const mime = recorder.mimeType
        voiceChunksRef.current = []
        await transcribeVoiceChunks(chunks, mime)
        // Always stop mic after we have audio
        stream.getTracks().forEach(t => t.stop())
        setMediaStream(null)
        setMediaRecorder(null)
      }

      recorder.start()

      // Silence detection: auto-stop after N ms of silence (only after user has spoken)
      const silenceMs = getVoiceSilenceMs()
      const SILENCE_CHECK_MS = 100
      const SILENCE_THRESHOLD = 15 // average frequency bucket value below which we consider silence
      try {
        const ctx = new (window.AudioContext || window.webkitAudioContext)()
        voiceContextRef.current = ctx
        const source = ctx.createMediaStreamSource(stream)
        const analyser = ctx.createAnalyser()
        analyser.fftSize = 256
        analyser.smoothingTimeConstant = 0.6
        source.connect(analyser)
        const data = new Uint8Array(analyser.frequencyBinCount)
        let hasSpoken = false
        let silenceStart = null
        voiceSilenceIntervalRef.current = setInterval(() => {
          const rec = voiceRecorderRef.current
          if (!rec || rec.state !== 'recording') {
            if (voiceSilenceIntervalRef.current) {
              clearInterval(voiceSilenceIntervalRef.current)
              voiceSilenceIntervalRef.current = null
            }
            return
          }
          analyser.getByteFrequencyData(data)
          let sum = 0
          for (let i = 0; i < data.length; i++) sum += data[i]
          const avg = data.length ? sum / data.length : 0
          if (avg > SILENCE_THRESHOLD) {
            hasSpoken = true
            silenceStart = null
          } else if (hasSpoken) {
            const now = Date.now()
            if (silenceStart == null) silenceStart = now
            if (now - silenceStart >= silenceMs) {
              if (voiceSilenceIntervalRef.current) {
                clearInterval(voiceSilenceIntervalRef.current)
                voiceSilenceIntervalRef.current = null
              }
              try {
                rec.requestData()
                rec.stop()
              } catch (_) { /* ignore */ }
            }
          }
        }, SILENCE_CHECK_MS)
      } catch (_) {
        // No silence detection (e.g. AudioContext not supported); user must click Stop
      }
    } catch (err) {
      const msg = err && err.name === 'NotAllowedError'
        ? 'Microphone permission denied. Please allow microphone access and try again.'
        : `Failed to start microphone: ${err.message}`
      setVoiceFeedback({ type: 'error', message: msg })
      cleanupVoiceMedia()
    }
  }

  const transcribeVoiceChunks = async (chunks, mimeType) => {
    try {
      if (!chunks || chunks.length === 0) {
        setVoiceFeedback({ type: 'error', message: 'No audio captured. Please try again.' })
        return
      }

      setVoiceUploading(true)
      clearFeedback(setVoiceFeedback)

      const blobType = mimeType || (chunks[0] && chunks[0].type) || 'audio/webm'
      const audioBlob = new Blob(chunks, { type: blobType })

      const form = new FormData()
      form.append('file', audioBlob, 'voice-question.webm')

      const response = await fetch(`${apiBaseUrl}/sessions/${currentSession.session.id}/questions/voice`, {
        method: 'POST',
        body: form
      })

      if (!response.ok) {
        const text = await response.text()
        setVoiceFeedback({ type: 'error', message: `Transcription failed (${response.status}): ${text}` })
        return
      }

      const data = await response.json()
      let text = (data && data.transcribed_text) ? data.transcribed_text : ''
      if (!text.trim()) {
        setVoiceFeedback({ type: 'error', message: 'Transcription was empty. Please try again or type your question.' })
        return
      }

      // Run rule-based cleanup by default
      try {
        const polishUrl = `${apiBaseUrl}/api/sessions/${currentSession.session.id}/questions/polish`
        const polishRes = await fetch(polishUrl, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ text: text.trim() })
        })
        if (polishRes.ok) {
          const polishData = await polishRes.json()
          const polished = (polishData && polishData.polished_text != null) ? String(polishData.polished_text).trim() : text
          if (polished) text = polished
        }
      } catch (_) {
        // keep original text if cleanup fails
      }

      setVoiceTranscribedText(text)
      setShowVoiceConfirm(true)
      setVoiceFeedback({ type: 'success', message: 'Transcription ready. Review and submit.' })
    } catch (err) {
      setVoiceFeedback({ type: 'error', message: `Transcription failed: ${err.message}` })
    } finally {
      setVoiceUploading(false)
      voiceChunksRef.current = []
    }
  }

  const cancelVoiceReview = () => {
    setShowVoiceConfirm(false)
    setVoiceTranscribedText('')
    setVoicePolishing(false)
    setVoicePolishMode(null)
  }

  const confirmVoiceQuestion = async () => {
    if (!voiceTranscribedText.trim()) {
      setVoiceFeedback({ type: 'error', message: 'Please enter a question before submitting.' })
      return
    }
    setShowVoiceConfirm(false)
    const text = voiceTranscribedText.trim()
    setVoiceTranscribedText('')
    await submitSessionQuestion(text, 'voice', replyingToQuestionId)
    // Questions will be refreshed by submitSessionQuestion
  }

  const polishVoiceQuestion = async (useLLM = false) => {
    const text = voiceTranscribedText.trim()
    if (!text || !currentSession?.session?.id) return
    setVoicePolishing(true)
    setVoicePolishMode(useLLM ? 'llm' : 'rules')
    clearFeedback(setVoiceFeedback)
    try {
      const url = `${apiBaseUrl}/api/sessions/${currentSession.session.id}/questions/polish${useLLM ? '?mode=llm' : ''}`
      const response = await fetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ text })
      })
      if (!response.ok) {
        const errText = await response.text()
        setVoiceFeedback({ type: 'error', message: `Clean up failed: ${response.status} ${errText}` })
        return
      }
      const data = await response.json()
      const polished = (data && data.polished_text != null) ? String(data.polished_text).trim() : text
      setVoiceTranscribedText(polished)
      setVoiceFeedback({ type: 'success', message: useLLM ? 'Polished with AI.' : 'Cleaned up fillers and repetition.' })
    } catch (err) {
      setVoiceFeedback({ type: 'error', message: `Clean up failed: ${err.message}` })
    } finally {
      setVoicePolishing(false)
      setVoicePolishMode(null)
    }
  }

  const effectiveParticipantRefForQuestions = participantRef || authUser?.email || null

  const fetchSessionQuestions = async (sessionId, participantRefForUnread = null) => {
    const ref = participantRefForUnread ?? effectiveParticipantRefForQuestions
    try {
      const url = ref
        ? `${apiBaseUrl}/sessions/${sessionId}/questions?participant_ref=${encodeURIComponent(ref)}`
        : `${apiBaseUrl}/sessions/${sessionId}/questions`
      const response = await fetch(url, { credentials: 'include' })
      if (!response.ok) {
        return
      }

      const data = await response.json()
      const answerMap = new Map()
      if (data.answers && Array.isArray(data.answers)) {
        data.answers.forEach(answer => {
          if (answer && answer.question_id) {
            answerMap.set(answer.question_id, answer)
          }
        })
      }
      const questionsWithAnswers = (data.questions || []).map(q => {
        const answer = answerMap.get(q.id) || null
        return {
          ...q,
          answer: answer
        }
      })
      // Always use server response so enriched citations (e.g. source_id for document open) are applied
      if (questionsWithAnswers.length > 0 || data.questions?.length === 0) {
        setQuestions(questionsWithAnswers)
      }
      if (ref && Array.isArray(data.unread_question_ids)) {
        setUnreadQuestionIds(data.unread_question_ids)
      } else if (!ref) {
        setUnreadQuestionIds([])
      }
    } catch (err) {
      // Silently fail
    }
  }

  const markQuestionViewed = useCallback(async (sessionId, questionId) => {
    const ref = effectiveParticipantRefForQuestions
    if (!ref || !sessionId || !questionId) return
    try {
      const res = await fetch(`${apiBaseUrl}/sessions/${sessionId}/questions/${questionId}/view`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ participant_ref: ref })
      })
      if (res.ok) {
        await fetchSessionQuestions(sessionId, ref)
      }
    } catch (_) { /* ignore */ }
  }, [apiBaseUrl, effectiveParticipantRefForQuestions])

  // Build participant URL for upper right corner link (include api so it works in new window/refresh)
  const sessionId = currentSession?.session?.id || currentSession?.id
  const participantUrl = sessionId
    ? buildCanonicalSessionUrl(sessionId, { mode: 'view', ...(apiBaseUrl ? { api: apiBaseUrl } : {}) })
    : null
  const hasValidSession = currentSession && sessionId

  // Check URL mode as fallback to determine if we're in participant mode
  const navForUrl = parseSessionNavigationFromLocation(window.location)
  const urlParams = new URLSearchParams(window.location.search)
  const urlMode = navForUrl.mode || urlParams.get('mode')
  const isAdminMode = urlMode === 'admin'
  const showAdminView = isAdminMode && authUser?.global_role === 'admin'
  const showAdminForbidden = isAdminMode && (!authUser || authUser.global_role !== 'admin')
  // Participant role can only join sessions; hide create-session UI (create form, Zoom, mode toggle).
  const canCreateSessions = !authUser || authUser.global_role !== 'participant'
  // Render participant view when session mode is participant, URL is view, or user role is participant (so ?mode=edit never shows edit UI)
  const isParticipantMode = sessionUserMode === 'participant' || urlMode === 'view' || authUser?.global_role === 'participant'
  // Use session from URL so participant tab can connect to WebSocket before openSession() completes
  const urlSessionId = navForUrl.sessionId
  const effectiveSessionId = sessionId || (urlMode === 'view' && urlSessionId ? urlSessionId : null)

  // WebSocket connection for real-time updates
  const wsUrl = effectiveSessionId
    ? (() => {
        // Convert http:// to ws:// or https:// to wss://
        let wsBaseUrl = apiBaseUrl
        if (apiBaseUrl.startsWith('http://')) {
          wsBaseUrl = apiBaseUrl.replace('http://', 'ws://')
        } else if (apiBaseUrl.startsWith('https://')) {
          wsBaseUrl = apiBaseUrl.replace('https://', 'wss://')
        }
        return `${wsBaseUrl}/ws/session?session=${effectiveSessionId}`
      })()
    : null

  // Handle WebSocket messages - use useCallback to keep it stable
  const handleWebSocketMessage = useCallback((message) => {
    if (!message || !message.type) {
      console.log('WebSocket: Received message without type:', message)
      return
    }
    const maybeBumpOrchestrationRefresh = () => {
      if (isParticipantMode || viewMode !== 'session' || !currentSession) return
      setOrchestrationRefreshTrigger((v) => v + 1)
    }
    const data = message.data ?? message.Data
    if (!data && (message.type === 'question_created' || message.type === 'answer_created' || message.type === 'answer_updated')) {
      console.warn('WebSocket: Message missing data', message)
      return
    }

    console.log('WebSocket: Received message:', message.type, 'for session:', effectiveSessionId)

    if (message.type === 'question_created' && data) {
      console.log('WebSocket: Question created, refreshing questions...')
      
      // Check if this is a mock question (has model: "mock" in answer or question text contains "Mock question")
      const question = data
      const isMockQuestion = question.question_text && question.question_text.includes('Mock question asked on')
      
      if (isMockQuestion) {
        // For mock questions, add to in-memory list instead of fetching from database
        console.log('WebSocket: Mock question received, adding to in-memory list')
        setMockQuestions(prev => {
          // Check if question already exists (avoid duplicates)
          const exists = prev.some(q => q.id === question.id)
          if (exists) return prev
          // Create question object (answer will come in separate message)
          return [...prev, {
            ...question,
            answer: null // Answer will be added when answer_created message arrives
          }]
        })
      } else {
        // For real questions, refresh from database
        if (effectiveSessionId) {
          fetchSessionQuestions(effectiveSessionId)
        }
      }
      maybeBumpOrchestrationRefresh()
    } else if (message.type === 'session_processing_ready') {
      const msgSessionId = message.SessionID ?? message.session_id
      if (msgSessionId && (msgSessionId === effectiveSessionId || msgSessionId === (currentSession?.session?.id || currentSession?.id))) {
        console.log('WebSocket: Session processing ready, refetching session...')
        setSessionProcessingReadyVersion((v) => v + 1)
        refetchSession()
        maybeBumpOrchestrationRefresh()
      }
    } else if (message.type === 'session_updated') {
      const msgSessionId = message.SessionID ?? message.session_id ?? (message.data && message.data.session_id)
      if (msgSessionId && (msgSessionId === effectiveSessionId || msgSessionId === (currentSession?.session?.id || currentSession?.id))) {
        maybeBumpOrchestrationRefresh()
        const now = Date.now()
        const withinGuard = (now - lastMaterialUploadAtRef.current) < 4000
        // Always bump the version so any open slide viewer refetches.
        setSessionUpdatedVersion((v) => v + 1)
        if (withinGuard) {
          // Don't refetch immediately (avoid "materials disappear" UX), but do schedule one soon so
          // slide readiness flags (material_slides_ready) update and the item becomes selectable.
          const elapsed = now - lastMaterialUploadAtRef.current
          const delayMs = Math.max(0, (4000 - elapsed)) + 250 // buffer so manifest write lands
          if (scheduledSessionRefetchTimeoutRef.current) {
            clearTimeout(scheduledSessionRefetchTimeoutRef.current)
          }
          scheduledSessionRefetchTimeoutRef.current = setTimeout(() => {
            scheduledSessionRefetchTimeoutRef.current = null
            if (typeof refetchSession === 'function') refetchSession()
          }, delayMs)
          return
        }
        console.log('WebSocket: Session updated (e.g. materials, slides ready), refetching session...')
        refetchSession()
      }
    } else if (message.type === 'session_deleted') {
      const deletedId = message.SessionID ?? message.session_id ?? (message.data && message.data.session_id)
      if (!deletedId) return
      const currentId = currentSession?.session?.id || currentSession?.id
      if (currentId === deletedId) {
        window.alert('This session was deleted.')
        setCurrentSession(null)
        setSessionMode('select')
      }
      setMySessions((prev) => prev.filter((item) => (item.session?.id ?? item.session_id ?? item.id) !== deletedId))
    } else if (message.type === 'invitation_accepted') {
      const msgSessionId = message.SessionID ?? message.session_id ?? (message.data && message.data.session_id)
      if (
        msgSessionId &&
        (msgSessionId === effectiveSessionId || msgSessionId === (currentSession?.session?.id || currentSession?.id)) &&
        typeof fetchSessionInvitations === 'function'
      ) {
        console.log('WebSocket: Invitation accepted, refetching invitations...')
        fetchSessionInvitations(msgSessionId)
        maybeBumpOrchestrationRefresh()
      }
    } else if (message.type === 'stance_updated') {
      // Bump stanceVersion so ParticipantMode and CreatorMode refetch GET /stances and update responses list + aggregate in real time
      console.log('WebSocket: Stance updated, bumping stanceVersion...')
      setStanceVersion((v) => v + 1)
      maybeBumpOrchestrationRefresh()
    } else if (message.type === 'answer_created' || message.type === 'answer_updated') {
      console.log('WebSocket: Answer created/updated, refreshing questions...')
      
      const answer = data
      // Check if this is for a mock question
      const isMockAnswer = answer.model === 'mock' || (answer.answer_text && answer.answer_text.includes('mock answer for testing'))
      
      if (isMockAnswer) {
        // Update mock question with answer
        console.log('WebSocket: Mock answer received, updating in-memory question')
        setMockQuestions(prev => {
          const questionExists = prev.some(q => q.id === answer.question_id)
          if (questionExists) {
            // Update existing question with answer
            return prev.map(q => 
              q.id === answer.question_id ? { ...q, answer: answer } : q
            )
          } else {
            // Answer arrived before question (unlikely but possible) - add question with answer
            // We'll need to create a minimal question object
            console.log('WebSocket: Mock answer received before question, creating placeholder question')
            return [...prev, {
              id: answer.question_id,
              question_text: 'Mock question (answer received first)',
              answer: answer
            }]
          }
        })
      } else {
        // For real answers, refresh from database
        if (effectiveSessionId) {
          fetchSessionQuestions(effectiveSessionId)
        }
      }
      maybeBumpOrchestrationRefresh()
    }
  }, [effectiveSessionId, fetchSessionQuestions, refetchSession, fetchSessionInvitations, currentSession?.session?.id, currentSession?.id, currentSession, isParticipantMode, viewMode, setStanceVersion, setCurrentSession, setSessionMode, setMySessions])

  // Clear all question state when session changes so we never show the previous session's questions
  useEffect(() => {
    setMockQuestions([])
    setPendingSessionQuestions([])
    setQuestions([])
    setUnreadQuestionIds([])
  }, [effectiveSessionId])

  useEffect(() => {
    setOrchestrationRefreshTrigger(0)
  }, [effectiveSessionId])

  // Server questions + mock, sorted by created_at. No optimistic pending so only one entry per question.
  const displayQuestions = useMemo(() => {
    const combined = [...questions, ...mockQuestions]
    combined.sort((a, b) => new Date(a.created_at || 0) - new Date(b.created_at || 0))
    return combined
  }, [questions, mockQuestions])

  // Resolve selected video from session using ref (user's chosen id) so selection survives refetches; sync ref when we fall back to primary
  useEffect(() => {
    const primary = currentSession?.primary_video ?? currentSession?.video_sources?.[0]
    const sources = currentSession?.video_sources ?? []
    const preferredId = selectedVideoIdRef.current
    const found = preferredId ? sources.find(vs => String(vs.id) === String(preferredId)) : null
    const resolved = found ?? primary
    if (!resolved) {
      setSelectedVideo(null)
      setVideoId(null)
      selectedVideoIdRef.current = null
      return
    }
    if (!found) selectedVideoIdRef.current = resolved?.id ?? null
    setSelectedVideo(resolved)
    setVideoId(resolved?.id ?? null)
  }, [currentSession])

  const { connected: wsConnected } = useWebSocket(wsUrl, handleWebSocketMessage, (error) => {
    console.error('WebSocket error:', error)
  })

  // Log WebSocket connection status
  useEffect(() => {
    if (effectiveSessionId) {
      console.log(`WebSocket: Connection status for session ${effectiveSessionId}:`, wsConnected ? 'CONNECTED' : 'DISCONNECTED')
      if (wsUrl) {
        console.log('WebSocket: URL:', wsUrl)
      }
    }
  }, [wsConnected, effectiveSessionId, wsUrl])

  // Accept-invite page: /accept-invite?token=... (SPA route; server must serve index.html for this path)
  const acceptInvitePath = window.location.pathname.replace(/\/$/, '') === '/accept-invite'
  const acceptInviteToken = acceptInvitePath ? new URLSearchParams(window.location.search).get('token') : null
  if (acceptInvitePath) {
    return (
      <AcceptInvitePage
        apiBaseUrl={apiBaseUrl}
        token={acceptInviteToken}
        authUser={authUser}
        authChecked={authChecked}
        onGoToSession={(sessionId) => {
          window.history.replaceState(null, '', buildCanonicalSessionUrl(sessionId, { mode: 'view' }))
          openSession(sessionId, 'participant')
        }}
        onLoginSuccess={(data, options) => {
          window.history.replaceState(null, '', window.location.pathname + (acceptInviteToken ? `?token=${encodeURIComponent(acceptInviteToken)}` : ''))
          setAuthUser(data)
          if (data.accept_token) {
            setAcceptToken(data.accept_token)
            try { sessionStorage.setItem('talkback.accept_token', data.accept_token) } catch (_) {}
          } else {
            fetch(`${apiBaseUrl.replace(/\/$/, '')}/api/me`, { credentials: 'include' })
              .then((r) => r.ok ? r.json() : null)
              .then((me) => {
                if (me?.accept_token) {
                  setAcceptToken(me.accept_token)
                  try { sessionStorage.setItem('talkback.accept_token', me.accept_token) } catch (_) {}
                }
              })
              .catch(() => {})
          }
          if (options?.goToSessionId) {
            window.history.replaceState(null, '', buildCanonicalSessionUrl(options.goToSessionId, { mode: 'view' }))
            openSession(options.goToSessionId, 'participant', false, null, false, data.user?.email)
          }
        }}
        onRegisterSuccess={({ user, sessionId, acceptToken: tok }) => {
          if (!user || !sessionId) return
          window.history.replaceState(null, '', buildCanonicalSessionUrl(sessionId, { mode: 'view' }))
          setAuthUser(user)
          setAcceptToken(tok || null)
          try { if (tok) sessionStorage.setItem('talkback.accept_token', tok) } catch (_) {}
          openSession(sessionId, 'participant', false, null, false, user.email)
        }}
        onSignOut={async () => {
          try {
            await fetch(`${apiBaseUrl.replace(/\/$/, '')}/api/auth/logout`, { method: 'POST', credentials: 'include' })
          } catch (_) { /* ignore */ }
          setAuthUser(null)
          setAcceptToken(null)
          try { sessionStorage.removeItem('talkback.accept_token') } catch (_) {}
        }}
      />
    )
  }

  // Require login: show login page until auth is checked and user is logged in
  if (!authChecked) {
    return (
      <div className="container" style={{ padding: '40px', textAlign: 'center' }}>
        <p style={{ color: '#666' }}>Loading…</p>
      </div>
    )
  }
  if (!authUser) {
    return (
      <LoginPage
        apiBaseUrl={apiBaseUrl}
        onLoginSuccess={(data) => {
          window.history.replaceState(null, '', historyPathFromLocation(window.location))
          setUrlKey((k) => k + 1)
          setAuthUser(data)
          if (data.accept_token) {
            setAcceptToken(data.accept_token)
            try { sessionStorage.setItem('talkback.accept_token', data.accept_token) } catch (_) {}
          } else {
            // Cookie may be set; fetch /api/me to get accept_token (needed for /api/sessions in incognito)
            fetch(`${apiBaseUrl.replace(/\/$/, '')}/api/me`, { credentials: 'include' })
              .then((r) => r.ok ? r.json() : null)
              .then((me) => {
                if (me?.accept_token) {
                  setAcceptToken(me.accept_token)
                  try { sessionStorage.setItem('talkback.accept_token', me.accept_token) } catch (_) {}
                }
              })
              .catch(() => {})
          }
        }}
      />
    )
  }

  return (
    <div className={`container${showAdminView ? ' admin-full-width' : currentSession && isParticipantMode ? ' participant-full-width' : currentSession && !isParticipantMode ? ' creator-session-pinned' : ''}`}>
      {zoomImportToast && (
        <div style={{
          position: 'fixed',
          top: 20,
          left: '50%',
          transform: 'translateX(-50%)',
          padding: '12px 24px',
          backgroundColor: '#4CAF50',
          color: 'white',
          borderRadius: '8px',
          boxShadow: '0 2px 8px rgba(0,0,0,0.2)',
          zIndex: 9999,
          fontWeight: '600',
          fontSize: '14px'
        }}>
          {zoomImportToast.message}
        </div>
      )}
      {teamsImportToast && (
        <div style={{
          position: 'fixed',
          bottom: '24px',
          right: '24px',
          padding: '12px 18px',
          backgroundColor: '#6264A7',
          color: 'white',
          borderRadius: '8px',
          boxShadow: '0 2px 8px rgba(0,0,0,0.2)',
          zIndex: 9999,
          fontWeight: '600',
          fontSize: '14px'
        }}>
          {teamsImportToast.message}
        </div>
      )}

      {/* Copy session modal: optional title */}
      {copyModalSession && (
        <div style={{ position: 'fixed', inset: 0, zIndex: 1000, display: 'flex', alignItems: 'center', justifyContent: 'center', background: 'rgba(0,0,0,0.5)' }} onClick={() => { setCopyModalSession(null); setCopyModalTitle('') }}>
          <div style={{ background: '#fff', padding: '24px', borderRadius: '8px', boxShadow: '0 4px 20px rgba(0,0,0,0.2)', maxWidth: '400px', width: '90%' }} onClick={e => e.stopPropagation()}>
            <h3 style={{ marginTop: 0, marginBottom: '12px' }}>Copy session</h3>
            <p style={{ marginBottom: '12px', color: '#555', fontSize: '14px' }}>Source: {copyModalSession.title}</p>
            <label style={{ display: 'block', marginBottom: '6px', fontSize: '14px' }}>New session title (optional)</label>
            <input
              type="text"
              value={copyModalTitle}
              onChange={e => setCopyModalTitle(e.target.value)}
              placeholder="Copy of …"
              style={{ width: '100%', padding: '8px', marginBottom: '16px', boxSizing: 'border-box' }}
            />
            <div style={{ display: 'flex', gap: '12px', justifyContent: 'flex-end' }}>
              <button type="button" onClick={() => { setCopyModalSession(null); setCopyModalTitle('') }} style={{ padding: '8px 16px' }}>Cancel</button>
              <button
                type="button"
                onClick={() => copySession(copyModalSession.id, copyModalTitle.trim() || undefined)}
                disabled={copyingSessionId === copyModalSession.id}
                style={{ padding: '8px 16px', cursor: copyingSessionId === copyModalSession.id ? 'not-allowed' : 'pointer' }}
              >
                {copyingSessionId === copyModalSession.id ? 'Copying…' : 'Copy'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Zoom import: prompt for session name (proposed name checked for duplicates) */}
      {zoomImportModalRec && (
        <div style={{ position: 'fixed', inset: 0, zIndex: 1000, display: 'flex', alignItems: 'center', justifyContent: 'center', background: 'rgba(0,0,0,0.5)' }} onClick={closeZoomImportModal}>
          <div style={{ background: '#fff', padding: '24px', borderRadius: '8px', boxShadow: '0 4px 20px rgba(0,0,0,0.2)', maxWidth: '400px', width: '90%' }} onClick={e => e.stopPropagation()}>
            <h3 style={{ marginTop: 0, marginBottom: '12px' }}>Import Zoom recording</h3>
            <p style={{ marginBottom: '12px', color: '#555', fontSize: '14px' }}>Recording: {zoomImportModalRec.meeting_topic || 'Untitled'}</p>
            <label style={{ display: 'block', marginBottom: '6px', fontSize: '14px' }}>Session name (required)</label>
            <input
              type="text"
              value={zoomImportSessionName}
              onChange={e => { setZoomImportSessionName(e.target.value); setZoomImportModalError('') }}
              placeholder="e.g., Weekly review"
              style={{ width: '100%', padding: '8px', marginBottom: '12px', boxSizing: 'border-box' }}
            />
            {zoomImportModalError && (
              <div className="error" style={{ marginBottom: '12px', fontSize: '13px' }}>{zoomImportModalError}</div>
            )}
            <div style={{ display: 'flex', gap: '12px', justifyContent: 'flex-end' }}>
              <button type="button" onClick={closeZoomImportModal} style={{ padding: '8px 16px' }}>Cancel</button>
              <button
                type="button"
                onClick={() => importFromZoomRecording(zoomImportModalRec, zoomImportSessionName)}
                disabled={zoomImporting || !zoomImportSessionName?.trim()}
                style={{ padding: '8px 16px', cursor: zoomImporting || !zoomImportSessionName?.trim() ? 'not-allowed' : 'pointer' }}
              >
                {zoomImporting ? 'Importing…' : 'Import'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Teams import: session name (duplicate check on server) */}
      {teamsImportModalRec && (
        <div style={{ position: 'fixed', inset: 0, zIndex: 1000, display: 'flex', alignItems: 'center', justifyContent: 'center', background: 'rgba(0,0,0,0.5)' }} onClick={closeTeamsImportModal}>
          <div style={{ background: '#fff', padding: '24px', borderRadius: '8px', boxShadow: '0 4px 20px rgba(0,0,0,0.2)', maxWidth: '400px', width: '90%' }} onClick={e => e.stopPropagation()}>
            <h3 style={{ marginTop: 0, marginBottom: '12px' }}>Import Teams recording</h3>
            <p style={{ marginBottom: '12px', color: '#555', fontSize: '14px' }}>Recording: {teamsImportModalRec.subject || 'Untitled'}</p>
            <label style={{ display: 'block', marginBottom: '6px', fontSize: '14px' }}>Session name (required)</label>
            <input
              type="text"
              value={teamsImportSessionName}
              onChange={e => { setTeamsImportSessionName(e.target.value); setTeamsImportModalError('') }}
              placeholder="e.g., Weekly review"
              style={{ width: '100%', padding: '8px', marginBottom: '12px', boxSizing: 'border-box' }}
            />
            {teamsImportModalError && (
              <div className="error" style={{ marginBottom: '12px', fontSize: '13px' }}>{teamsImportModalError}</div>
            )}
            <div style={{ display: 'flex', gap: '12px', justifyContent: 'flex-end' }}>
              <button type="button" onClick={closeTeamsImportModal} style={{ padding: '8px 16px' }}>Cancel</button>
              <button
                type="button"
                onClick={() => importFromTeamsRecording(teamsImportModalRec, teamsImportSessionName)}
                disabled={teamsImporting || !teamsImportSessionName?.trim()}
                style={{ padding: '8px 16px', cursor: teamsImporting || !teamsImportSessionName?.trim() ? 'not-allowed' : 'pointer' }}
              >
                {teamsImporting ? 'Importing…' : 'Import'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Rename session modal */}
      {renameSessionId && (
        <div style={{ position: 'fixed', inset: 0, zIndex: 1000, display: 'flex', alignItems: 'center', justifyContent: 'center', background: 'rgba(0,0,0,0.5)' }} onClick={() => { setRenameSessionId(null); setRenameSessionTitle('') }}>
          <div style={{ background: '#fff', padding: '24px', borderRadius: '8px', boxShadow: '0 4px 20px rgba(0,0,0,0.2)', maxWidth: '400px', width: '90%' }} onClick={e => e.stopPropagation()}>
            <h3 style={{ marginTop: 0, marginBottom: '12px' }}>Rename session</h3>
            <label style={{ display: 'block', marginBottom: '6px', fontSize: '14px' }}>Title</label>
            <input
              type="text"
              value={renameSessionTitle}
              onChange={e => setRenameSessionTitle(e.target.value)}
              placeholder="Session title"
              style={{ width: '100%', padding: '8px', marginBottom: '16px', boxSizing: 'border-box' }}
            />
            <div style={{ display: 'flex', gap: '12px', justifyContent: 'flex-end' }}>
              <button type="button" onClick={() => { setRenameSessionId(null); setRenameSessionTitle('') }} disabled={renameSaving} style={{ padding: '8px 16px' }}>Cancel</button>
              <button type="button" onClick={saveRenameSession} disabled={renameSaving || !renameSessionTitle.trim()} style={{ padding: '8px 16px', cursor: renameSaving ? 'not-allowed' : 'pointer' }}>
                {renameSaving ? 'Saving…' : 'Save'}
              </button>
            </div>
          </div>
        </div>
      )}

      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: '12px', marginBottom: '20px', minWidth: 0 }}>
        <h1 style={{ margin: 0, minWidth: 0 }}>TalkBack</h1>
        <div style={{ display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: '12px', minWidth: 0 }}>
          {/* TalkBack auth: logged-in state + Log out */}
          <span style={{ fontSize: '13px', color: '#555', display: 'flex', alignItems: 'center', gap: '6px' }}>
            Logged in as {authUser.display_name || authUser.email}
            {authUser.global_role === 'admin' && (
              <span title="Admin" style={{ display: 'inline-flex', alignItems: 'center', padding: '2px 6px', borderRadius: '4px', backgroundColor: '#1976d2', color: '#fff', fontSize: '11px', fontWeight: '600' }}>Admin</span>
            )}
          </span>
          <button
            type="button"
            onClick={async () => {
              try {
                await fetch(`${apiBaseUrl.replace(/\/$/, '')}/api/auth/logout`, { method: 'POST', credentials: 'include' })
              } catch (_) { /* ignore */ }
              setAuthUser(null)
              // Clear session and view state so next login always lands on default (no session selected)
              setCurrentSession(null)
              setViewMode('session')
              setSessionUserMode(null)
              setCurrentUser('')
              setSessionSelectFeedback({ type: '', message: '' })
              // Clear session route / query so next login lands on home (not /app/sessions/... or ?mode=admin)
              window.history.replaceState(null, '', '/')
              setUrlKey(k => k + 1)
            }}
            style={{ fontSize: '13px', padding: '4px 10px', cursor: 'pointer', background: 'none', border: '1px solid #999', borderRadius: '4px', color: '#555' }}
          >
            Log out
          </button>
          {/* Admin: link on all screens when user is admin; Back to app when in admin mode */}
          {authUser?.global_role === 'admin' && (
            showAdminView ? (
              <a href="?" style={{ fontSize: '14px', fontWeight: '600' }}>Back to app</a>
            ) : (
              <a href="?mode=admin" style={{ fontSize: '14px', fontWeight: '600' }}>Admin</a>
            )
          )}
          {/* Debug mode toggle - Upper right */}
          <label style={{ display: 'flex', alignItems: 'center', gap: '6px', fontSize: '14px', cursor: 'pointer', userSelect: 'none' }}>
            <input
              type="checkbox"
              checked={debugMode}
              onChange={(e) => setDebugMode(e.target.checked)}
              style={{ width: '16px', height: '16px', cursor: 'pointer' }}
              aria-label="Show debug panel"
            />
            <span>Debug</span>
          </label>
          {/* Participant View Link */}
          {hasValidSession && participantUrl && sessionUserMode === 'creator' && (
            <a
              href={participantUrl}
              target="_blank"
              rel="noopener noreferrer"
              style={{
                padding: '8px 16px',
                backgroundColor: '#4CAF50',
                color: 'white',
                textDecoration: 'none',
                borderRadius: '4px',
                fontSize: '14px',
                fontWeight: '600',
                display: 'inline-flex',
                alignItems: 'center',
                gap: '8px',
                transition: 'all 0.2s',
                whiteSpace: 'nowrap',
                boxShadow: '0 2px 4px rgba(0,0,0,0.1)'
              }}
              onMouseEnter={(e) => {
                e.currentTarget.style.backgroundColor = '#45a049'
                e.currentTarget.style.boxShadow = '0 2px 6px rgba(0,0,0,0.15)'
              }}
              onMouseLeave={(e) => {
                e.currentTarget.style.backgroundColor = '#4CAF50'
                e.currentTarget.style.boxShadow = '0 2px 4px rgba(0,0,0,0.1)'
              }}
            >
              <span style={{ fontSize: '16px' }}>👁️</span>
              <span>View as Participant</span>
              <span style={{ fontSize: '12px', opacity: 0.9 }}>↗</span>
            </a>
          )}
        </div>
      </div>

      {showAdminView && (
              <AdminUsers
                apiBaseUrl={apiBaseUrl}
                debugMode={debugMode}
                usersExpanded={adminUsersExpanded}
                onUsersExpandedChange={setAdminUsersExpanded}
                sessionsExpanded={adminSessionsExpanded}
                onSessionsExpandedChange={setAdminSessionsExpanded}
              />
            )}
      {showAdminForbidden && (
        <div className="section" style={{ padding: '24px' }}>
          <p className="error">Forbidden. Admin access required.</p>
          <a href="?">Back to app</a>
        </div>
      )}
      {!showAdminView && !showAdminForbidden && (
      <>
      {debugMode && (
        <div className="section">
          <div className="form-group">
            <label>API Base URL:</label>
            <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
              <input
                type="text"
                value={apiBaseUrl}
                onChange={(e) => setApiBaseUrl(e.target.value)}
                placeholder={getDefaultApiBaseUrl()}
                style={{ flex: 1 }}
              />
              <button 
                onClick={() => checkApiHealth(new AbortController().signal)} 
                disabled={healthChecking}
                style={{ marginTop: 0 }}
              >
                {healthChecking ? 'Checking...' : 'Check Health'}
              </button>
              <button
                type="button"
                onClick={() => setApiBaseUrl(getDefaultApiBaseUrl())}
                style={{ marginTop: 0, fontSize: '12px' }}
              >
                Reset to default
              </button>
            </div>
            <div style={{ fontSize: '12px', color: '#666', marginTop: '4px' }}>
              Default: <code>{getDefaultApiBaseUrl()}</code> (override in UI or set <code>VITE_API_BASE_URL</code> at build time for production)
            </div>
          </div>
          <div style={{ marginTop: '10px' }}>
            {apiHealth === null && !healthChecking && (
              <div className="info">API status: Unknown - Click "Check Health" to verify</div>
            )}
            {apiHealth === true && (
              <div className="success" style={{ marginTop: 0 }}>
                ✓ API is healthy and reachable
              </div>
            )}
            {apiHealth === false && (
              <div className="error" style={{ marginTop: 0 }}>
                ✗ API is not reachable - Check if the server is running on {apiBaseUrl}
              </div>
            )}
          </div>
          {artifactId && (
            <div className="info" style={{ marginTop: '10px' }}>
              Current Artifact ID: <span className="artifact-id">{artifactId}</span>
            </div>
          )}
          {videoId && (
            <div className="info" style={{ marginTop: '10px' }}>
              Current Video ID: <span className="artifact-id">{videoId}</span>
            </div>
          )}
          {currentSession?.session && (
            <div className="success" style={{ marginTop: '10px' }}>
              ✓ Active Session: <span className="artifact-id">{currentSession.session.title}</span> (ID: {currentSession.session.id})
            </div>
          )}
          {/* TalkBack auth /api/me */}
          <div style={{ marginTop: '10px', fontSize: '13px' }}>
            {authUser ? (
              <span className="success">✓ Logged in: {authUser.display_name} ({authUser.email}) · {authUser.global_role} · {authUser.status}</span>
            ) : (
              <span className="info">Not logged in (GET /api/me returns 401 without cookie)</span>
            )}
          </div>
        </div>
      )}

      {/* Session Selector - when no session: show picker. When session open in creator mode, CreatorMode shows the single session header (no duplicate here). */}
      {(!currentSession || !isParticipantMode) && !(currentSession && viewMode === 'session' && !isParticipantMode) && (
        <>
          {/* Pending Invites: only show when there are pending invites (hide when empty or in create flow) */}
          {!currentSession && authUser && sessionMode !== 'create' && pendingInvitationsFetched && pendingInvitations.length > 0 && (
            <div style={{ marginBottom: '20px', padding: '14px', backgroundColor: '#fff3e0', borderRadius: '8px', border: '2px solid #ff9800' }}>
              <h3 style={{ margin: '0 0 10px 0', fontSize: '1.1rem' }}>Pending Invites</h3>
              <div style={{ maxHeight: '200px', overflowY: 'auto' }}>
                  {[...pendingInvitations]
                    .sort((a, b) => new Date(b.created_at || 0) - new Date(a.created_at || 0))
                    .map((inv) => (
                    <div
                      key={inv.id}
                      style={{
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'space-between',
                        gap: '10px',
                        padding: '10px',
                        marginBottom: '6px',
                        border: '1px solid #ffcc80',
                        borderRadius: '5px',
                        backgroundColor: '#fff'
                      }}
                    >
                      <div style={{ flex: 1, minWidth: 0 }}>
                        <div style={{ fontWeight: '600' }}>{inv.session_title || 'Untitled session'}</div>
                        <div style={{ fontSize: '12px', color: '#666' }}>
                          Invited by {inv.inviter_name || '—'} · {inv.invited_role || 'participant'}
                          {inv.created_at && (
                            <span style={{ marginLeft: '6px', color: '#888' }}>
                              · Sent {new Date(inv.created_at).toLocaleString()}
                            </span>
                          )}
                        </div>
                      </div>
                      <button
                        type="button"
                        disabled={acceptingInvitationId === inv.id}
                        onClick={() => acceptPendingInvitation(inv.id)}
                        style={{
                          flexShrink: 0,
                          padding: '6px 14px',
                          backgroundColor: '#ff9800',
                          color: '#fff',
                          border: 'none',
                          borderRadius: '4px',
                          cursor: acceptingInvitationId === inv.id ? 'not-allowed' : 'pointer',
                          fontWeight: '600',
                          fontSize: '13px'
                        }}
                      >
                        {acceptingInvitationId === inv.id ? 'Accepting…' : 'Accept & open'}
                      </button>
                    </div>
                  ))}
                </div>
            </div>
          )}

          <h2>{authUser?.global_role === 'participant' && !currentSession ? "Sessions you're part of" : 'Session Selection (Required)'}</h2>
          {!currentSession && urlSessionId && loading && (
            <div className="info" style={{ marginBottom: '12px' }}>Loading session…</div>
          )}
          {!currentSession && urlSessionId && !loading && sessionSelectFeedback.type === 'error' && sessionSelectFeedback.message && (
            <div className="section error" style={{ marginBottom: '16px', padding: '14px', borderRadius: '8px' }}>
              <div style={{ marginBottom: '12px', fontWeight: '500' }}>{sessionSelectFeedback.message}</div>
              <button
                type="button"
                onClick={() => {
                  const nav = parseSessionNavigationFromLocation(window.location)
                  const id = nav.sessionId
                  if (!id) return
                  if (nav.mode === 'edit') openSession(id, 'creator')
                  else if (nav.mode === 'view') openSession(id, 'participant')
                  else openSession(id, authUser?.global_role === 'participant' ? 'participant' : 'creator')
                }}
                style={{ padding: '8px 16px', fontSize: '14px', cursor: 'pointer', backgroundColor: '#1976d2', color: '#fff', border: 'none', borderRadius: '4px' }}
              >
                Try again
              </button>
              <button
                type="button"
                onClick={() => {
                  clearFeedback(setSessionSelectFeedback)
                  const apiQ = new URLSearchParams(window.location.search).get('api')
                  const apiSuffix = apiQ ? `&api=${encodeURIComponent(apiQ)}` : ''
                  const mode = authUser?.global_role === 'participant' ? 'view' : 'edit'
                  window.history.replaceState(null, '', `/?mode=${mode}${apiSuffix}`)
                  setPopstateNavKey((k) => k + 1)
                }}
                style={{ marginLeft: '10px', padding: '8px 16px', fontSize: '14px', cursor: 'pointer', backgroundColor: '#757575', color: '#fff', border: 'none', borderRadius: '4px' }}
              >
                Back to sessions
              </button>
            </div>
          )}
          <div className="section" style={{ border: '2px solid #2196F3', backgroundColor: '#e3f2fd' }}>
            {!currentSession ? (
          <>
            {/* Show Create vs Use Existing toggle only when no sessions and user can create; otherwise show list first */}
            {canCreateSessions && mySessions.length === 0 && !mySessionsLoading && (
            <div style={{ marginBottom: '15px' }}>
              <label style={{ fontWeight: 'bold', marginBottom: '10px', display: 'block' }}>Select Mode:</label>
              <div style={{ display: 'flex', gap: '10px', marginBottom: '15px' }}>
                <button
                  onClick={() => setSessionMode('create')}
                  style={{
                    backgroundColor: sessionMode === 'create' ? '#2196F3' : '#e0e0e0',
                    color: sessionMode === 'create' ? 'white' : 'black',
                    padding: '8px 16px',
                    border: 'none',
                    borderRadius: '4px',
                    cursor: 'pointer'
                  }}
                >
                  Create New Session
                </button>
                <button
                  onClick={() => setSessionMode('select')}
                  style={{
                    backgroundColor: sessionMode === 'select' ? '#2196F3' : '#e0e0e0',
                    color: sessionMode === 'select' ? 'white' : 'black',
                    padding: '8px 16px',
                    border: 'none',
                    borderRadius: '4px',
                    cursor: 'pointer'
                  }}
                >
                  Use Existing Session
                </button>
              </div>
            </div>
            )}

            {/* Use Existing Session: session list (shown first when sessions exist; creators can switch to create via link) */}
            {(sessionMode === 'select' || !canCreateSessions) && (
              <div>
                {authUser && (
                  <div style={{ marginBottom: '15px', padding: '10px', backgroundColor: '#fff', borderRadius: '5px' }}>
                    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: '8px', marginBottom: '10px' }}>
                      <span style={{ fontWeight: 'bold' }}>{canCreateSessions ? 'Sessions' : "Sessions you're part of"}</span>
                      {canCreateSessions && mySessions.length > 0 && (
                        <button
                          type="button"
                          onClick={() => setSessionMode('create')}
                          style={{ fontSize: '13px', padding: '4px 10px', cursor: 'pointer' }}
                        >
                          Create new session
                        </button>
                      )}
                    </div>
                    {mySessionsLoading ? (
                      <div className="info" style={{ marginTop: '8px' }}>Loading sessions…</div>
                    ) : mySessionsError ? (
                      <div className="error" style={{ marginTop: '8px', fontSize: '13px' }}>{mySessionsError}</div>
                    ) : mySessions.length === 0 ? (
                      <div className="info" style={{ marginTop: '8px' }}>
                        {canCreateSessions ? 'No sessions. Switch to Create New Session above to create one, or get invited to a session.' : 'There are no sessions to which you have been invited.'}
                      </div>
                    ) : (
                      <div style={{ maxHeight: '280px', overflowY: 'auto', marginTop: '8px' }}>
                        {mySessions.map((item) => {
                          const session = item.session || item
                          const myRole = item.my_role || (session.created_by === authUser?.email ? 'creator' : 'participant')
                          return (
                            <div
                              key={session.id}
                              style={{
                                padding: '10px',
                                marginBottom: '8px',
                                border: '1px solid #ddd',
                                borderRadius: '5px',
                                cursor: 'pointer',
                                backgroundColor: session.status === 'open' ? '#e8f5e9' : '#f5f5f5',
                                transition: 'background-color 0.2s'
                              }}
                              onMouseEnter={(e) => { e.currentTarget.style.backgroundColor = session.status === 'open' ? '#c8e6c9' : '#e0e0e0' }}
                              onMouseLeave={(e) => { e.currentTarget.style.backgroundColor = session.status === 'open' ? '#e8f5e9' : '#f5f5f5' }}
                              onClick={() => openSession(session.id, myRole === 'participant' ? 'participant' : 'creator')}
                            >
                              <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '5px' }}>
                                <span style={{ fontWeight: 'bold' }}>{session.title || 'Untitled session'}</span>
                                <span
                                  style={{
                                    fontSize: '10px',
                                    padding: '2px 6px',
                                    borderRadius: '4px',
                                    backgroundColor: myRole === 'admin' ? '#1976d2' : myRole === 'creator' ? '#2e7d32' : '#757575',
                                    color: '#fff',
                                    fontWeight: '600',
                                    textTransform: 'capitalize'
                                  }}
                                >
                                  {myRole}
                                </span>
                              </div>
                              <div style={{ fontSize: '11px', color: '#666', marginBottom: '5px' }}>
                                ID: <code style={{ fontSize: '10px' }}>{session.id}</code>
                              </div>
                              <div style={{ fontSize: '12px', color: '#666' }}>
                                Status: <span style={{ color: session.status === 'open' ? '#4CAF50' : '#999', fontWeight: 'bold' }}>{session.status}</span>
                                {session.updated_at && ` · Updated ${new Date(session.updated_at).toLocaleDateString()}`}
                              </div>
                              {canCreateSessions && (myRole === 'creator' || myRole === 'admin') && (
                                <div style={{ marginTop: '8px', display: 'flex', gap: '8px', flexWrap: 'wrap' }} onClick={(e) => e.stopPropagation()}>
                                  <button
                                    type="button"
                                    disabled={copyingSessionId === session.id}
                                    onClick={() => { setCopyModalSession({ id: session.id, title: session.title || 'Untitled session' }); setCopyModalTitle('Copy of ' + (session.title || 'Untitled session')) }}
                                    style={{ fontSize: '12px', padding: '4px 10px', cursor: copyingSessionId === session.id ? 'not-allowed' : 'pointer' }}
                                  >
                                    {copyingSessionId === session.id ? 'Copying…' : 'Copy session'}
                                  </button>
                                  {(myRole === 'creator' || myRole === 'admin') && (
                                    <button
                                      type="button"
                                      onClick={() => { setRenameSessionId(session.id); setRenameSessionTitle(session.title || '') }}
                                      style={{ fontSize: '12px', padding: '4px 10px', cursor: 'pointer' }}
                                    >
                                      Rename
                                    </button>
                                  )}
                                </div>
                              )}
                            </div>
                          )
                        })}
                      </div>
                    )}
                  </div>
                )}
              </div>
            )}

            {/* Create New Session: sub-choice From Zoom | Empty session */}
            {sessionMode === 'create' && canCreateSessions && (
              <div style={{ marginBottom: '20px', padding: '12px', backgroundColor: '#f9f9f9', borderRadius: '8px', border: '1px solid #e0e0e0' }}>
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '15px', flexWrap: 'wrap', gap: '10px' }}>
                  <span style={{ fontWeight: 'bold', fontSize: '1rem' }}>Create new session</span>
                  <button
                    type="button"
                    onClick={() => setSessionMode('select')}
                    style={{
                      padding: '8px 16px',
                      fontSize: '14px',
                      cursor: 'pointer',
                      border: '1px solid #999',
                      borderRadius: '4px',
                      background: '#fff',
                      color: '#555',
                      fontWeight: '500'
                    }}
                  >
                    Cancel
                  </button>
                </div>
                <div style={{ marginBottom: '15px' }}>
                  <label style={{ fontWeight: 'bold', marginBottom: '10px', display: 'block' }}>How to create:</label>
                  <div style={{ display: 'flex', gap: '10px', marginBottom: '15px', flexWrap: 'wrap' }}>
                    <button
                      onClick={() => setCreateSource(CREATE_SOURCE.ZOOM)}
                      style={{
                        backgroundColor: createSource === CREATE_SOURCE.ZOOM ? '#2196F3' : '#e0e0e0',
                        color: createSource === CREATE_SOURCE.ZOOM ? 'white' : 'black',
                        padding: '8px 16px',
                        border: 'none',
                        borderRadius: '4px',
                        cursor: 'pointer'
                      }}
                    >
                      From Zoom
                    </button>
                    {teamsApiEnabled && (
                      <button
                        type="button"
                        onClick={() => setCreateSource(CREATE_SOURCE.TEAMS)}
                        style={{
                          backgroundColor: createSource === CREATE_SOURCE.TEAMS ? '#6264A7' : '#e0e0e0',
                          color: createSource === CREATE_SOURCE.TEAMS ? 'white' : 'black',
                          padding: '8px 16px',
                          border: 'none',
                          borderRadius: '4px',
                          cursor: 'pointer'
                        }}
                      >
                        From Teams
                      </button>
                    )}
                    <button
                      onClick={() => setCreateSource(CREATE_SOURCE.EMPTY)}
                      style={{
                        backgroundColor: createSource === CREATE_SOURCE.EMPTY ? '#2196F3' : '#e0e0e0',
                        color: createSource === CREATE_SOURCE.EMPTY ? 'white' : 'black',
                        padding: '8px 16px',
                        border: 'none',
                        borderRadius: '4px',
                        cursor: 'pointer'
                      }}
                    >
                      Empty session
                    </button>
                  </div>
                </div>

                {createSource === CREATE_SOURCE.EMPTY && (
                  <div style={{ marginBottom: '15px', padding: '10px', backgroundColor: '#fff', borderRadius: '5px' }}>
                    <div style={{ fontWeight: 'bold', marginBottom: '10px' }}>Create New Session</div>
                    <div className="form-group">
                      <label>Session Title:</label>
                      <input
                        type="text"
                        value={sessionTitle}
                        onChange={(e) => setSessionTitle(e.target.value)}
                        placeholder="e.g., Weekly review - Jan 2026"
                        style={{ marginBottom: '10px' }}
                      />
                    </div>
                    <button onClick={createSession} disabled={!sessionTitle || loading}>
                      Create Session
                    </button>
                    {createSessionFeedback.message && (
                      <div className={createSessionFeedback.type} style={{ marginTop: '10px' }}>
                        {createSessionFeedback.message}
                      </div>
                    )}
                  </div>
                )}

                {createSource === CREATE_SOURCE.ZOOM && (
                  <div style={{ marginBottom: '20px', padding: '12px', backgroundColor: '#f5f5f5', borderRadius: '6px', border: '1px solid #e0e0e0' }}>
                    <div style={{ fontWeight: 'bold', marginBottom: '8px' }}>Zoom</div>
                    {zoomConnection ? (
                      <div style={{ marginBottom: '10px', display: 'flex', alignItems: 'center', gap: '10px', flexWrap: 'wrap' }}>
                        <span style={{ color: '#2e7d32', fontSize: '14px' }}>
                          Connected as {zoomConnection.zoom_user_email || zoomConnection.zoom_user_id || 'Zoom user'}
                        </span>
                        <button type="button" onClick={disconnectZoom} style={{ padding: '4px 10px', fontSize: '13px' }}>
                          Disconnect
                        </button>
                      </div>
                    ) : (
                      <div style={{ marginBottom: '10px' }}>
                        <button
                          type="button"
                          onClick={() => {
                            try { sessionStorage.setItem('talkback.zoom_return_to_create', '1') } catch (_) {}
                            window.location.href = `${apiBaseUrl}/auth/zoom/start?creator_identity=${encodeURIComponent(creatorIdentity)}`
                          }}
                          style={{ padding: '6px 12px', backgroundColor: '#2D8CFF', color: 'white', border: 'none', borderRadius: '4px', fontSize: '14px', cursor: 'pointer' }}
                        >
                          Connect Zoom
                        </button>
                      </div>
                    )}
                    {zoomConnection && (
                      <div style={{ marginTop: '10px' }}>
                        {/* Primary: Load recordings */}
                        <div style={{ marginBottom: '15px', paddingBottom: '15px', borderBottom: '1px solid #e0e0e0' }}>
                          <div style={{ fontWeight: 'bold', marginBottom: '10px' }}>Your Zoom recordings</div>
                          <div style={{ display: 'flex', gap: '10px', marginBottom: '10px', flexWrap: 'wrap', alignItems: 'center' }}>
                            <button
                              type="button"
                              onClick={fetchZoomRecordings}
                              disabled={zoomRecordingsLoading}
                              style={{ padding: '4px 12px', fontSize: '13px' }}
                            >
                              {zoomRecordingsLoading ? 'Loading…' : 'Load recordings'}
                            </button>
                          </div>
                          {zoomRecordingsError && (
                            <div className="error" style={{ marginBottom: '10px', fontSize: '13px' }}>{zoomRecordingsError}</div>
                          )}
                          {zoomRecordings.length > 0 && (
                            <div style={{ maxHeight: '280px', overflow: 'auto', border: '1px solid #ddd', borderRadius: '4px', padding: '8px', backgroundColor: '#fafafa' }}>
                              {zoomRecordings.map((rec, idx) => (
                                <div key={idx} style={{ padding: '10px', marginBottom: '8px', backgroundColor: '#fff', borderRadius: '4px', border: '1px solid #eee' }}>
                                  <div style={{ fontWeight: 'bold', marginBottom: '4px', fontSize: '14px' }}>{rec.meeting_topic || 'Untitled'}</div>
                                  <div style={{ fontSize: '12px', color: '#666', marginBottom: '6px' }}>
                                    {rec.start_time ? new Date(rec.start_time).toLocaleString() : '—'} · {rec.duration_minutes ?? 0} min
                                  </div>
                                  <div style={{ marginBottom: '8px', display: 'flex', gap: '8px', flexWrap: 'wrap' }}>
                                    {rec.has_video && <span style={{ padding: '2px 6px', backgroundColor: '#e3f2fd', borderRadius: '4px', fontSize: '11px' }}>Video</span>}
                                    {rec.has_transcript ? (
                                      <span style={{ padding: '2px 6px', backgroundColor: '#e8f5e9', color: '#2e7d32', borderRadius: '4px', fontSize: '11px', fontWeight: 500 }}>Transcript</span>
                                    ) : (
                                      <span style={{ padding: '2px 6px', backgroundColor: '#f5f5f5', color: '#757575', borderRadius: '4px', fontSize: '11px' }}>No transcript</span>
                                    )}
                                  </div>
                                  <button
                                    type="button"
                                    onClick={() => openZoomImportModal(rec)}
                                    disabled={zoomImporting || !rec.has_transcript}
                                    style={{ padding: '4px 12px', fontSize: '12px', backgroundColor: '#2196F3', color: 'white', border: 'none', borderRadius: '4px', cursor: zoomImporting || !rec.has_transcript ? 'not-allowed' : 'pointer' }}
                                  >
                                    Import
                                  </button>
                                </div>
                              ))}
                            </div>
                          )}
                          {!zoomRecordingsLoading && zoomRecordings.length === 0 && zoomRecordingsError === '' && (
                            <div style={{ fontSize: '13px', color: '#666', fontStyle: 'italic' }}>Click "Load recordings" to fetch Zoom cloud recordings (most recent first)</div>
                          )}
                        </div>

                        {/* Secondary: collapsible paste URL */}
                        <div>
                          <button
                            type="button"
                            onClick={() => {
                              setZoomPasteUrlExpanded((v) => !v)
                              if (!zoomPasteUrlExpanded && !zoomTitle?.trim()) setZoomTitle('Zoom Recording')
                            }}
                            style={{ background: 'none', border: 'none', padding: 0, cursor: 'pointer', fontSize: '13px', color: '#2196F3', textDecoration: 'underline', marginBottom: '8px' }}
                          >
                            {zoomPasteUrlExpanded ? '▼' : '▶'} Or paste a Zoom recording URL
                          </button>
                          {zoomPasteUrlExpanded && (
                            <div style={{ marginTop: '8px' }}>
                              <input
                                type="text"
                                value={zoomUrl}
                                onChange={(e) => {
                                  setZoomUrl(e.target.value)
                                  setZoomImportError('')
                                  setZoomTranscriptStatus(null)
                                  setZoomTranscriptMessage('')
                                  setZoomTranscriptTopic(null)
                                }}
                                placeholder="https://zoom.us/rec/play/... or zoom.us/recording/detail?meeting_id=..."
                                style={{ width: '100%', marginBottom: '6px', padding: '8px' }}
                              />
                              <div style={{ display: 'flex', gap: '8px', marginBottom: '8px', flexWrap: 'wrap', alignItems: 'center' }}>
                                <button type="button" onClick={checkZoomTranscript} disabled={zoomCheckingTranscript || !zoomUrl?.trim()} style={{ padding: '4px 10px', fontSize: '12px' }}>
                                  {zoomCheckingTranscript ? 'Checking…' : 'Check transcript'}
                                </button>
                                <button type="button" onClick={createSessionFromZoom} disabled={zoomImporting || !zoomTitle?.trim()}>
                                  {zoomImporting ? 'Importing…' : 'Create session / Import transcript'}
                                </button>
                              </div>
                              {zoomTranscriptStatus === 'ready' && (
                                <div style={{ marginTop: '8px', padding: '8px', backgroundColor: '#e8f5e9', borderRadius: '4px', fontSize: '13px', color: '#2e7d32' }}>
                                  ✓ {zoomTranscriptMessage}
                                  {zoomTranscriptTopic && <span style={{ display: 'block', marginTop: '4px', opacity: 0.9 }}>Recording: {zoomTranscriptTopic}</span>}
                                </div>
                              )}
                              {zoomTranscriptStatus === 'processing' && (
                                <div style={{ marginTop: '8px', padding: '8px', backgroundColor: '#fff8e1', borderRadius: '4px', fontSize: '13px', color: '#f57c00' }}>
                                  ⏳ {zoomTranscriptMessage}
                                  <button type="button" onClick={checkZoomTranscript} disabled={zoomCheckingTranscript} style={{ marginLeft: '8px', padding: '2px 8px', fontSize: '12px' }}>
                                    Try again
                                  </button>
                                </div>
                              )}
                              {zoomTranscriptStatus === 'not_available' && (
                                <div style={{ marginTop: '8px', padding: '8px', backgroundColor: '#ffebee', borderRadius: '4px', fontSize: '13px', color: '#c62828' }}>
                                  Transcript not available: {zoomTranscriptMessage}
                                </div>
                              )}
                              {zoomTranscriptStatus === 'recording_not_found' && (
                                <div style={{ marginTop: '8px', padding: '8px', backgroundColor: '#ffebee', borderRadius: '4px', fontSize: '13px', color: '#c62828' }}>
                                  {zoomTranscriptMessage}
                                </div>
                              )}
                              {zoomTranscriptStatus === 'zoom_share_link' && (
                                <div style={{ marginTop: '8px', padding: '8px', backgroundColor: '#fff8e1', borderRadius: '4px', fontSize: '13px', color: '#f57c00' }}>
                                  {zoomTranscriptMessage}
                                </div>
                              )}
                              {(zoomTranscriptStatus === 'error' || zoomTranscriptStatus === 'api_error') && zoomTranscriptMessage && (
                                <div style={{ marginTop: '8px', padding: '8px', backgroundColor: '#ffebee', borderRadius: '4px', fontSize: '13px', color: '#c62828' }}>
                                  {zoomTranscriptMessage}
                                </div>
                              )}
                              <label style={{ display: 'block', marginBottom: '4px', fontSize: '13px', marginTop: '8px' }}>Session title (required):</label>
                              <input
                                type="text"
                                value={zoomTitle}
                                onChange={(e) => setZoomTitle(e.target.value)}
                                placeholder={zoomTranscriptTopic || 'e.g., Weekly review'}
                                style={{ width: '100%', marginBottom: '8px', padding: '8px' }}
                              />
                              {zoomImportError && (
                                <div className="error" style={{ marginTop: '8px', fontSize: '13px' }}>{zoomImportError}</div>
                              )}
                            </div>
                          )}
                        </div>
                      </div>
                    )}
                  </div>
                )}

                {createSource === CREATE_SOURCE.TEAMS && !teamsApiEnabled && (
                  <div style={{ marginBottom: '15px', padding: '10px', backgroundColor: '#fff3e0', borderRadius: '5px', fontSize: '13px' }}>
                    Microsoft Teams import is not enabled on this server (the API must set ENABLE_TEAMS=true).
                  </div>
                )}

                {createSource === CREATE_SOURCE.TEAMS && teamsApiEnabled && (
                  <div style={{ marginBottom: '20px', padding: '12px', backgroundColor: '#f3f2ff', borderRadius: '6px', border: '1px solid #e0e0e0' }}>
                    <div style={{ fontWeight: 'bold', marginBottom: '8px' }}>Microsoft Teams</div>
                    {teamsConnection ? (
                      <div style={{ marginBottom: '10px', display: 'flex', alignItems: 'center', gap: '10px', flexWrap: 'wrap' }}>
                        <span style={{ color: '#2e7d32', fontSize: '14px' }}>
                          Connected as {teamsConnection.teams_email || teamsConnection.teams_user_id || 'Teams user'}
                        </span>
                        <button type="button" onClick={disconnectTeams} style={{ padding: '4px 10px', fontSize: '13px' }}>
                          Disconnect
                        </button>
                      </div>
                    ) : (
                      <div style={{ marginBottom: '10px' }}>
                        <button
                          type="button"
                          onClick={() => {
                            try { sessionStorage.setItem('talkback.teams_return_to_create', '1') } catch (_) {}
                            window.location.href = `${apiBaseUrl}/auth/teams/start?creator_identity=${encodeURIComponent(creatorIdentity)}`
                          }}
                          style={{ padding: '6px 12px', backgroundColor: '#6264A7', color: 'white', border: 'none', borderRadius: '4px', fontSize: '14px', cursor: 'pointer' }}
                        >
                          Connect Microsoft Teams
                        </button>
                      </div>
                    )}
                    {teamsImportError && (
                      <div className="error" style={{ marginBottom: '10px', fontSize: '13px' }}>{teamsImportError}</div>
                    )}
                    {teamsConnection && (
                      <div style={{ marginTop: '10px' }}>
                        <div style={{ fontWeight: 'bold', marginBottom: '10px' }}>Your Teams recordings</div>
                        <div style={{ display: 'flex', gap: '10px', marginBottom: '10px', flexWrap: 'wrap', alignItems: 'center' }}>
                          <button
                            type="button"
                            onClick={fetchTeamsRecordings}
                            disabled={teamsRecordingsLoading}
                            style={{ padding: '4px 12px', fontSize: '13px' }}
                          >
                            {teamsRecordingsLoading ? 'Loading…' : 'Load recordings'}
                          </button>
                        </div>
                        {teamsRecordingsError && (
                          <div className="error" style={{ marginBottom: '10px', fontSize: '13px' }}>{teamsRecordingsError}</div>
                        )}
                        {teamsRecordings.length > 0 && (
                          <div style={{ maxHeight: '280px', overflow: 'auto', border: '1px solid #ddd', borderRadius: '4px', padding: '8px', backgroundColor: '#fafafa' }}>
                            {teamsRecordings.map((rec, idx) => (
                              <div key={`${rec.meeting_id}-${rec.recording_id}-${idx}`} style={{ padding: '10px', marginBottom: '8px', backgroundColor: '#fff', borderRadius: '4px', border: '1px solid #eee' }}>
                                <div style={{ fontWeight: 'bold', marginBottom: '4px', fontSize: '14px' }}>{rec.subject || 'Untitled'}</div>
                                <div style={{ fontSize: '12px', color: '#666', marginBottom: '6px' }}>
                                  {rec.start_time ? new Date(rec.start_time).toLocaleString() : '—'}
                                </div>
                                <button
                                  type="button"
                                  onClick={() => openTeamsImportModal(rec)}
                                  disabled={teamsImporting}
                                  style={{ padding: '4px 12px', fontSize: '12px', backgroundColor: '#6264A7', color: 'white', border: 'none', borderRadius: '4px', cursor: teamsImporting ? 'not-allowed' : 'pointer' }}
                                >
                                  Import
                                </button>
                              </div>
                            ))}
                          </div>
                        )}
                        {!teamsRecordingsLoading && teamsRecordings.length === 0 && teamsRecordingsError === '' && (
                          <div style={{ fontSize: '13px', color: '#666', fontStyle: 'italic' }}>Click &quot;Load recordings&quot; to list recent Teams meetings with recordings (Graph API).</div>
                        )}
                      </div>
                    )}
                  </div>
                )}
              </div>
            )}
          </>
        ) : (
          <div style={{ padding: '15px', backgroundColor: '#fff', borderRadius: '5px' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <div>
                <div style={{ fontWeight: 'bold', marginBottom: '5px', display: 'flex', alignItems: 'center', gap: '8px' }}>
                  Active Session: {currentSession?.session?.title ?? 'Loading...'}
                  {currentSession?.session && authUser?.email && (
                    <span
                      style={{
                        fontSize: '10px',
                        padding: '2px 6px',
                        borderRadius: '4px',
                        backgroundColor: currentSession.session.created_by === authUser.email ? '#2e7d32' : '#757575',
                        color: '#fff',
                        fontWeight: '600',
                        textTransform: 'capitalize'
                      }}
                    >
                      {currentSession.session.created_by === authUser.email ? 'Creator' : 'Participant'}
                    </span>
                  )}
                </div>
                <div style={{ fontSize: '12px', color: '#666' }}>
                  ID: <code style={{ fontSize: '11px' }}>{currentSession?.session?.id ?? '—'}</code> | 
                  Status: <span style={{ 
                    color: currentSession?.session?.status === 'open' ? '#4CAF50' : '#999',
                    fontWeight: 'bold'
                  }}>{currentSession?.session?.status ?? '—'}</span>
                </div>
              </div>
              <button 
                onClick={() => { 
                  setCurrentSession(null)
                  setSessionSelectFeedback({ type: '', message: '' })
                  const apiQ = new URLSearchParams(window.location.search).get('api')
                  const apiSuffix = apiQ ? `&api=${encodeURIComponent(apiQ)}` : ''
                  const mode = authUser?.global_role === 'participant' ? 'view' : 'edit'
                  window.history.replaceState(null, '', `/?mode=${mode}${apiSuffix}`)
                }} 
                style={{ 
                  backgroundColor: '#f44336',
                  color: 'white',
                  padding: '8px 16px'
                }}
              >
                Clear Session
              </button>
            </div>
            {authUser && (
              <div style={{ marginTop: '12px', paddingTop: '12px', borderTop: '1px solid #eee' }}>
                <label style={{ display: 'block', marginBottom: '6px', fontSize: '13px', fontWeight: '500' }}>invite user by their email address</label>
                <div style={{ display: 'flex', gap: '8px', alignItems: 'center', flexWrap: 'wrap' }}>
                  <input
                    type="email"
                    value={inviteEmail}
                    onChange={(e) => setInviteEmail(e.target.value)}
                    placeholder="user@example.com"
                    style={{ flex: '1', minWidth: '180px', padding: '6px 10px', fontSize: '13px' }}
                  />
                  <select value={inviteRole} onChange={(e) => setInviteRole(e.target.value)} style={{ padding: '6px 10px', fontSize: '13px' }}>
                    <option value="participant">Participant</option>
                    <option value="creator">Creator</option>
                  </select>
                  <button type="button" onClick={inviteUserToSession} disabled={!inviteEmail?.trim() || !isValidEmailFormat(inviteEmail?.trim()) || inviteLoading}>
                    {inviteLoading ? 'Sending…' : 'Invite'}
                  </button>
                </div>
                {inviteFeedback.message && (
                  <div className={inviteFeedback.type} style={{ marginTop: '8px', fontSize: '13px' }}>
                    {inviteFeedback.message}
                  </div>
                )}
              </div>
            )}
          </div>
        )}
      </div>
        </>
      )}

      {/* Session View - Mode-based rendering. Only show when a session is open so participants without a session see only the session list (no materials/player). */}
      {viewMode === 'session' && currentSession && (
        <>
          {/* Session header is now inside each mode component */}
          {/* Use isParticipantMode to determine which component to render */}
          {!isParticipantMode ? (
            <CreatorMode
              currentSession={currentSession}
              sessionProcessingReadyVersion={sessionProcessingReadyVersion}
              sessionUpdatedVersion={sessionUpdatedVersion}
              stanceVersion={stanceVersion}
              orchestrationRefreshTrigger={orchestrationRefreshTrigger}
              refetchSession={refetchSession}
              artifactId={artifactId}
              setArtifactId={setArtifactId}
              videoId={videoId}
              setVideoId={setVideoId}
              selectedVideo={selectedVideo}
              selectedVideoIdRef={selectedVideoIdRef}
              setSelectedVideo={setSelectedVideoWithRef}
              videoPlayerKey={videoPlayerKey}
              setVideoPlayerKey={setVideoPlayerKey}
              currentVideoTime={currentVideoTime}
              setCurrentVideoTime={setCurrentVideoTime}
              isVideoPlaying={isVideoPlaying}
              setIsVideoPlaying={setIsVideoPlaying}
              handleVideoPlayerEvent={handleVideoPlayerEvent}
              handleVideoTimeUpdate={handleVideoTimeUpdate}
              getVideoEmbedUrl={getVideoEmbedUrl}
              transcriptJobs={transcriptJobs}
              regenerateTranscript={regenerateTranscript}
              questions={displayQuestions}
              unreadQuestionIds={unreadQuestionIds}
              markQuestionViewed={markQuestionViewed}
              fetchSessionQuestions={fetchSessionQuestions}
              loading={loading}
              apiBaseUrl={apiBaseUrl}
              creatorIdentity={creatorIdentity}
              authUser={authUser}
              inviteEmail={inviteEmail}
              setInviteEmail={setInviteEmail}
              inviteRole={inviteRole}
              setInviteRole={setInviteRole}
              inviteFeedback={inviteFeedback}
              setInviteFeedback={setInviteFeedback}
              inviteLoading={inviteLoading}
              inviteUserToSession={inviteUserToSession}
              sessionInvitations={sessionInvitations}
              fetchSessionInvitations={fetchSessionInvitations}
              lastInvitationDraft={lastInvitationDraft}
              setLastInvitationDraft={setLastInvitationDraft}
              setPrimaryVideoSource={setPrimaryVideoSource}
              onClearSession={() => {
                setCurrentSession(null)
                setSessionSelectFeedback({ type: '', message: '' })
                const apiQ = new URLSearchParams(window.location.search).get('api')
                const apiSuffix = apiQ ? `&api=${encodeURIComponent(apiQ)}` : ''
                const mode = authUser?.global_role === 'participant' ? 'view' : 'edit'
                window.history.replaceState(null, '', `/?mode=${mode}${apiSuffix}`)
              }}
              debugMode={debugMode}
            />
          ) : (
            <div className="participant-layout-root">
              <ParticipantMode
                authUser={authUser}
                currentSession={currentSession}
                selectedVideo={selectedVideo}
                selectedVideoIdRef={selectedVideoIdRef}
                setSelectedVideo={setSelectedVideoWithRef}
                setVideoId={setVideoId}
              videoPlayerKey={videoPlayerKey}
              setVideoPlayerKey={setVideoPlayerKey}
                currentVideoTime={currentVideoTime}
                setCurrentVideoTime={setCurrentVideoTime}
                isVideoPlaying={isVideoPlaying}
                setIsVideoPlaying={setIsVideoPlaying}
                handleVideoPlayerEvent={handleVideoPlayerEvent}
                handleVideoTimeUpdate={handleVideoTimeUpdate}
                getVideoEmbedUrl={getVideoEmbedUrl}
                transcriptJobs={transcriptJobs}
                questions={displayQuestions}
                unreadQuestionIds={unreadQuestionIds}
                markQuestionViewed={markQuestionViewed}
                fetchSessionQuestions={fetchSessionQuestions}
                loading={loading}
                apiBaseUrl={apiBaseUrl}
                creatorIdentity={creatorIdentity}
                questionText={questionText}
                setQuestionText={setQuestionText}
                askSessionQuestion={askSessionQuestion}
                askQuestionFeedback={askQuestionFeedback}
                currentAnswer={currentAnswer}
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
              refetchSession={refetchSession}
              markMaterialsSeen={markMaterialsSeen}
              sessionLoadError={sessionSelectFeedback.type === 'error' ? sessionSelectFeedback.message : ''}
              sessionIdFromUrl={urlSessionId}
              onRetryLoadSession={urlSessionId ? () => openSession(urlSessionId, 'participant') : null}
              replyingToQuestionId={replyingToQuestionId}
              setReplyingToQuestionId={setReplyingToQuestionId}
              currentAskerName={authUser?.email ?? undefined}
              stanceVersion={stanceVersion}
              sessionUpdatedVersion={sessionUpdatedVersion}
              sessionInvitations={sessionInvitations}
              onClearSession={() => {
                setCurrentSession(null)
                setSessionSelectFeedback({ type: '', message: '' })
                const apiQ = new URLSearchParams(window.location.search).get('api')
                const apiSuffix = apiQ ? `&api=${encodeURIComponent(apiQ)}` : ''
                window.history.replaceState(null, '', `/?mode=view${apiSuffix}`)
              }}
              onCitationClick={(citation) => {
                const seekMs = citation?.navigation?.type === 'video' && citation.navigation.seek_ms != null
                  ? citation.navigation.seek_ms
                  : citation?.anchor?.start_ms
                if (seekMs != null) {
                  setCurrentVideoTime(seekMs / 1000)
                }
              }}
              />
            </div>
          )}
        </>
      )}

      </>
      )}
    </div>
  )
}

export default App
