import { useState, useEffect, useRef, useCallback } from 'react'
import { VideoPlayer, PlayerEvent } from './VideoPlayer'
import { CreatorMode } from './modes/CreatorMode'
import { ParticipantMode } from './modes/ParticipantMode'
import { useWebSocket } from './hooks/useWebSocket'

const API_BASE_URL_DEFAULT = 'http://localhost:8081'

function App() {
  const [apiBaseUrl, setApiBaseUrl] = useState(API_BASE_URL_DEFAULT)
  const [artifactId, setArtifactId] = useState('')
  const [videoId, setVideoId] = useState('')
  
  // Form states
  const [artifactTitle, setArtifactTitle] = useState('')
  const [artifactDescription, setArtifactDescription] = useState('')
  const [materialFiles, setMaterialFiles] = useState([]) // Array of File objects for multiple uploads
  const [uploadedMaterials, setUploadedMaterials] = useState([]) // Array of successfully uploaded materials
  const [materialKind, setMaterialKind] = useState('document')
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
  const [mediaRecorder, setMediaRecorder] = useState(null)
  const [mediaStream, setMediaStream] = useState(null)
  const voiceChunksRef = useRef([])
  
  // Phase 3: Session states
  const [sessionTitle, setSessionTitle] = useState('')
  const [sessionCreatedBy, setSessionCreatedBy] = useState('anonymous')
  const [sessions, setSessions] = useState([])
  const [currentSession, setCurrentSession] = useState(null)
  const [participantRef, setParticipantRef] = useState('anonymous')
  const [viewMode, setViewMode] = useState('artifact') // 'artifact' or 'session'
  const [sessionMode, setSessionMode] = useState('create') // 'create' or 'select'
  const [sessionIdInput, setSessionIdInput] = useState('')
  const [sessionSelectFeedback, setSessionSelectFeedback] = useState({ type: '', message: '' })
  
  // Creator/Participant Mode states
  const [currentUser, setCurrentUser] = useState('') // User identifier for mode detection
  const [sessionUserMode, setSessionUserMode] = useState(null) // 'creator' or 'participant' - from API
  
  // Response states - per-section feedback
  const [createArtifactFeedback, setCreateArtifactFeedback] = useState({ type: '', message: '' })
  const [uploadMaterialFeedback, setUploadMaterialFeedback] = useState({ type: '', message: '' })
  const [attachVideoFeedback, setAttachVideoFeedback] = useState({ type: '', message: '' })
  const [submitTranscriptFeedback, setSubmitTranscriptFeedback] = useState({ type: '', message: '' })
  const [askQuestionFeedback, setAskQuestionFeedback] = useState({ type: '', message: '' })
  const [questionHistoryFeedback, setQuestionHistoryFeedback] = useState({ type: '', message: '' })
  const [resetFeedback, setResetFeedback] = useState({ type: '', message: '' })
  const [createSessionFeedback, setCreateSessionFeedback] = useState({ type: '', message: '' })
  const [joinSessionFeedback, setJoinSessionFeedback] = useState({ type: '', message: '' })
  
  // Global states
  const [loading, setLoading] = useState(false)
  const [currentAnswer, setCurrentAnswer] = useState(null)
  const [questions, setQuestions] = useState([])
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
  const [resetConfirmText, setResetConfirmText] = useState('')
  const [showResetConfirm, setShowResetConfirm] = useState(false)
  
  // Video player states
  const [selectedVideo, setSelectedVideo] = useState(null)
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
    try {
      if (mediaRecorder && mediaRecorder.state !== 'inactive') {
        mediaRecorder.stop()
      }
    } catch {
      // ignore
    }
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
    if (!apiBaseUrl) {
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
      // Switch to artifact view mode to show upload sections
      setViewMode('artifact')
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
      formData.append('kind', materialKind)

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
      // Add to uploaded materials list
      setUploadedMaterials(prev => [...prev, { ...data, uploadedAt: new Date().toISOString() }])
      // Remove file from materialFiles array
      setMaterialFiles(prev => prev.filter(f => f !== file))
      setUploadMaterialFeedback({ type: 'success', message: `Material uploaded! Filename: ${data.filename}, Text Status: ${data.text_status}` })
    } catch (err) {
      setUploadMaterialFeedback({ type: 'error', message: `Failed to upload material: ${err.message}` })
    } finally {
      setLoading(false)
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
        body: JSON.stringify({
          title: sessionTitle,
          created_by: sessionCreatedBy || undefined
        })
      })

      if (!response.ok) {
        const text = await response.text()
        try {
          const json = JSON.parse(text)
          setCreateSessionFeedback({ type: 'error', message: `Error ${response.status}: ${JSON.stringify(json, null, 2)}` })
        } catch {
          setCreateSessionFeedback({ type: 'error', message: `Error ${response.status}: ${text}` })
        }
        return
      }

      const data = await response.json()
      setCreateSessionFeedback({ type: 'success', message: `Session created! ID: ${data.id}` })
      setSessionTitle('')
      // Set currentUser to match the created_by so we're in creator mode
      if (sessionCreatedBy) {
        setCurrentUser(sessionCreatedBy)
      }
      // Automatically open the newly created session in creator mode
      await openSession(data.id, 'creator')
    } catch (err) {
      setCreateSessionFeedback({ type: 'error', message: `Failed to create session: ${err.message}` })
    } finally {
      setLoading(false)
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
    setZoomImporting(true)
    setZoomImportError('')
    try {
      const headers = { 'Content-Type': 'application/json', 'X-Creator-Identity': creatorIdentity }
      const body = JSON.stringify({ zoom_url: url, title: zoomTitle?.trim() || undefined })
      const response = await fetch(`${apiBaseUrl}/sessions/from-zoom`, { method: 'POST', headers, body })
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
        setZoomImportError(msg)
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
      await fetch(`${apiBaseUrl}/auth/zoom/disconnect`, {
        method: 'POST',
        headers: { 'X-Creator-Identity': creatorIdentity }
      })
      setZoomConnection(null)
    } catch {
      // ignore
    }
  }

  const fetchSessions = async () => {
    // Note: Sessions are now independent, so we don't fetch by artifact
    // If needed in the future, we can add a GET /sessions endpoint
    // For now, sessions are created and opened individually
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

  // Handle Zoom OAuth callback: ?zoom=connected&creator_identity= or ?zoom=error&message=
  useEffect(() => {
    const urlParams = new URLSearchParams(window.location.search)
    const zoom = urlParams.get('zoom')
    const ci = urlParams.get('creator_identity')
    const message = urlParams.get('message')
    if (zoom === 'connected' && ci) {
      setCreatorIdentity(ci)
      setZoomConnection({ zoom_user_email: null, zoom_user_id: null }) // will be filled by /me
      window.history.replaceState({}, '', window.location.pathname + window.location.hash)
    } else if (zoom === 'error') {
      setZoomImportError(message === 'missing_code_or_state' ? 'Zoom sign-in was cancelled or incomplete.' : message === 'server_not_configured' ? 'Zoom is not configured on the server.' : message === 'exchange_failed' ? 'Could not complete Zoom sign-in.' : message === 'save_failed' ? 'Could not save Zoom connection.' : message || 'Zoom sign-in failed.')
      window.history.replaceState({}, '', window.location.pathname + window.location.hash)
    }
  }, [])

  // Fetch Zoom connection status when creator identity is set
  useEffect(() => {
    if (!creatorIdentity || !apiBaseUrl) return
    const ac = new AbortController()
    fetch(`${apiBaseUrl}/auth/zoom/me?creator_identity=${encodeURIComponent(creatorIdentity)}`, { signal: ac.signal })
      .then((res) => res.json())
      .then((data) => {
        if (data.connected) {
          setZoomConnection({
            zoom_user_email: data.zoom_user_email || null,
            zoom_user_id: data.zoom_user_id || null
          })
        } else {
          setZoomConnection(null)
        }
      })
      .catch(() => setZoomConnection(null))
    return () => ac.abort()
  }, [creatorIdentity, apiBaseUrl])

  // Check URL for session and mode on mount
  useEffect(() => {
    const urlParams = new URLSearchParams(window.location.search)
    const sessionId = urlParams.get('session')
    const mode = urlParams.get('mode') // 'edit' or 'view'
    
    if (sessionId) {
      // Set mode immediately based on URL, even before API call completes
      if (mode === 'view') {
        setSessionUserMode('participant')
        setCurrentUser('participant')
        setViewMode('session') // Participant mode should always use session view
      } else {
        // Default to creator mode
        setSessionUserMode('creator')
      }
      
      // Then try to load the session
      if (mode === 'edit') {
        openSession(sessionId, 'creator')
      } else if (mode === 'view') {
        openSession(sessionId, 'participant')
      } else {
        // No mode specified, default to creator view
        openSession(sessionId, 'creator')
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const openSession = async (sessionId, forceMode = null, stayInSessionView = false) => {
    setLoading(true)
    clearFeedback(setSessionSelectFeedback)
    
    // Set mode immediately based on forceMode, even if API call fails
    if (forceMode === 'participant') {
      setSessionUserMode('participant')
      setCurrentUser('participant')
    } else if (forceMode === 'creator') {
      setSessionUserMode('creator')
    }
    
    try {
      // First, fetch session without user header to get session data
      const response = await fetch(`${apiBaseUrl}/sessions/${sessionId}`)
      if (!response.ok) {
        setSessionSelectFeedback({ type: 'error', message: `Failed to load session: ${response.status}` })
        // Mode is already set above, so UI will still hide/show correct sections
        // Set viewMode to 'session' so ParticipantMode can render even if session load failed
        setViewMode('session')
        setLoading(false)
        return
      }

      const data = await response.json()
      setCurrentSession(data)
      
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
        window.history.replaceState({}, '', `?session=${sessionId}&mode=edit`)
      } else if (forceMode === 'participant') {
        // Set currentUser to something that won't match created_by
        setCurrentUser('participant')
        setSessionUserMode('participant')
        // Update URL to reflect participant mode
        window.history.replaceState({}, '', `?session=${sessionId}&mode=view`)
      } else {
        // No explicit mode, determine from URL or default to creator
        const urlParams = new URLSearchParams(window.location.search)
        const urlMode = urlParams.get('mode')
        
        if (urlMode === 'view') {
          setCurrentUser('participant')
          setSessionUserMode('participant')
          window.history.replaceState({}, '', `?session=${sessionId}&mode=view`)
        } else {
          // Default to creator mode
          if (data.session && data.session.created_by) {
            setCurrentUser(data.session.created_by)
          } else {
            setCurrentUser('creator')
          }
          setSessionUserMode('creator')
          window.history.replaceState({}, '', `?session=${sessionId}&mode=edit`)
        }
      }
      
      // Set artifact ID if there are artifacts in the session
      if (data.artifacts && data.artifacts.length > 0) {
        setArtifactId(data.artifacts[0].id)
        // Pre-populate video ID if there are video sources
        if (data.video_sources && data.video_sources.length > 0) {
          setVideoId(data.video_sources[0].id)
        }
        // Stay in session view when opening after Zoom import so user sees session + video; otherwise switch to artifact view
        if (stayInSessionView || (forceMode === 'participant') || sessionUserMode === 'participant') {
          setViewMode('session')
        } else {
          setViewMode('artifact')
        }
      } else {
        // No artifacts yet, stay in session view mode
        setViewMode('session')
      }
      setSessionIdInput('') // Clear input
      
      // Load session questions
      fetchSessionQuestions(sessionId)
      setSessionSelectFeedback({ type: 'success', message: `Session loaded: ${data.session.title}` })
    } catch (err) {
      setSessionSelectFeedback({ type: 'error', message: `Failed to load session: ${err.message}` })
    } finally {
      setLoading(false)
    }
  }

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
      setJoinSessionFeedback({ type: 'error', message: 'Please enter participant name' })
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

    await submitSessionQuestion(questionText)
  }

  const submitSessionQuestion = async (text) => {
    clearFeedback(setAskQuestionFeedback)
    setLoading(true)

    try {
      const requestBody = {
        question_text: text
      }
      if (currentVideoTime > 0) {
        requestBody.video_time_seconds = Math.floor(currentVideoTime)
      }

      const response = await fetch(`${apiBaseUrl}/sessions/${currentSession.session.id}/questions`, {
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
      const isCached = response.status === 200
      const statusMsg = isCached 
        ? `Question answered (cached)! Status: ${data.answer.answer_status}`
        : `Question answered! Status: ${data.answer.answer_status}`
      setAskQuestionFeedback({ type: 'success', message: statusMsg })
      setQuestionText('')
      // Immediately refresh questions to show the new question
      await fetchSessionQuestions(currentSession.session.id)
    } catch (err) {
      setAskQuestionFeedback({ type: 'error', message: `Failed to ask question: ${err.message}` })
    } finally {
      setLoading(false)
    }
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

    // Start recording
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
      voiceChunksRef.current = []
      setVoiceRecording(true)

      recorder.ondataavailable = (e) => {
        if (e.data && e.data.size > 0) {
          voiceChunksRef.current.push(e.data)
        }
      }

      recorder.onstop = async () => {
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
      const text = (data && data.transcribed_text) ? data.transcribed_text : ''
      if (!text.trim()) {
        setVoiceFeedback({ type: 'error', message: 'Transcription was empty. Please try again or type your question.' })
        return
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

  const confirmVoiceQuestion = async () => {
    if (!voiceTranscribedText.trim()) {
      setVoiceFeedback({ type: 'error', message: 'Please enter a question before submitting.' })
      return
    }
    setShowVoiceConfirm(false)
    const text = voiceTranscribedText.trim()
    setVoiceTranscribedText('')
    await submitSessionQuestion(text)
    // Questions will be refreshed by submitSessionQuestion
  }

  const fetchSessionQuestions = async (sessionId) => {
    try {
      const response = await fetch(`${apiBaseUrl}/sessions/${sessionId}/questions`)
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
      // Only update if questions actually changed to avoid unnecessary re-renders
      setQuestions(prevQuestions => {
        // Simple comparison: if count or IDs changed, update
        if (prevQuestions.length !== questionsWithAnswers.length) {
          return questionsWithAnswers
        }
        // Check if any question IDs changed (new questions added)
        const prevQuestionIds = new Set(prevQuestions.map(q => q.id))
        const newQuestionIds = new Set(questionsWithAnswers.map(q => q.id))
        if (prevQuestionIds.size !== newQuestionIds.size || [...prevQuestionIds].some(id => !newQuestionIds.has(id))) {
          return questionsWithAnswers
        }
        // Check if any question IDs or answer IDs changed
        const prevIds = new Set(prevQuestions.map(q => `${q.id}-${q.answer?.id || 'no-answer'}`))
        const newIds = new Set(questionsWithAnswers.map(q => `${q.id}-${q.answer?.id || 'no-answer'}`))
        if (prevIds.size !== newIds.size || [...prevIds].some(id => !newIds.has(id))) {
          return questionsWithAnswers
        }
        // Check if any answer text changed
        for (let i = 0; i < prevQuestions.length; i++) {
          const prevQ = prevQuestions[i]
          const newQ = questionsWithAnswers[i]
          if (prevQ.answer?.answer_text !== newQ.answer?.answer_text ||
              prevQ.answer?.answer_status !== newQ.answer?.answer_status) {
            return questionsWithAnswers
          }
        }
        // Check if any question text changed (in case questions are edited)
        for (let i = 0; i < prevQuestions.length; i++) {
          const prevQ = prevQuestions[i]
          const newQ = questionsWithAnswers[i]
          if (prevQ.question_text !== newQ.question_text) {
            return questionsWithAnswers
          }
        }
        return prevQuestions // No changes, keep previous state
      })
    } catch (err) {
      // Silently fail
    }
  }

  // Fetch sessions when artifact changes
  useEffect(() => {
    if (artifactId) {
      fetchSessions()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [artifactId])

  // Build participant URL for upper right corner link
  const sessionId = currentSession?.session?.id || currentSession?.id
  const participantUrl = sessionId 
    ? `${window.location.origin}${window.location.pathname}?session=${sessionId}&mode=view`
    : null
  const hasValidSession = currentSession && sessionId

  // Check URL mode as fallback to determine if we're in participant mode
  const urlParams = new URLSearchParams(window.location.search)
  const urlMode = urlParams.get('mode')
  const isParticipantMode = sessionUserMode === 'participant' || urlMode === 'view'

  // WebSocket connection for real-time updates
  const wsUrl = sessionId 
    ? (() => {
        // Convert http:// to ws:// or https:// to wss://
        let wsBaseUrl = apiBaseUrl
        if (apiBaseUrl.startsWith('http://')) {
          wsBaseUrl = apiBaseUrl.replace('http://', 'ws://')
        } else if (apiBaseUrl.startsWith('https://')) {
          wsBaseUrl = apiBaseUrl.replace('https://', 'wss://')
        }
        return `${wsBaseUrl}/ws/session?session=${sessionId}`
      })()
    : null

  // Handle WebSocket messages - use useCallback to keep it stable
  const handleWebSocketMessage = useCallback((message) => {
    if (!message || !message.type) {
      console.log('WebSocket: Received message without type:', message)
      return
    }

    console.log('WebSocket: Received message:', message.type, 'for session:', sessionId)

    if (message.type === 'question_created' && message.data) {
      console.log('WebSocket: Question created, refreshing questions...')
      
      // Check if this is a mock question (has model: "mock" in answer or question text contains "Mock question")
      const question = message.data
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
        if (sessionId) {
          fetchSessionQuestions(sessionId)
        }
      }
    } else if (message.type === 'answer_created' || message.type === 'answer_updated') {
      console.log('WebSocket: Answer created/updated, refreshing questions...')
      
      const answer = message.data
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
        if (sessionId) {
          fetchSessionQuestions(sessionId)
        }
      }
    }
  }, [sessionId, fetchSessionQuestions])

  // Clear mock questions when session changes
  useEffect(() => {
    setMockQuestions([])
  }, [sessionId])

  const { connected: wsConnected } = useWebSocket(wsUrl, handleWebSocketMessage, (error) => {
    console.error('WebSocket error:', error)
  })

  // Log WebSocket connection status
  useEffect(() => {
    if (sessionId) {
      console.log(`WebSocket: Connection status for session ${sessionId}:`, wsConnected ? 'CONNECTED' : 'DISCONNECTED')
      if (wsUrl) {
        console.log('WebSocket: URL:', wsUrl)
      }
    }
  }, [wsConnected, sessionId, wsUrl])

  return (
    <div className="container">
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '20px' }}>
        <h1 style={{ margin: 0 }}>TalkBack Phase 3 - Web UI</h1>
        <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
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
      
      {debugMode && (
        <div className="section">
          <div className="form-group">
            <label>API Base URL:</label>
            <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
              <input
                type="text"
                value={apiBaseUrl}
                onChange={(e) => setApiBaseUrl(e.target.value)}
                placeholder="http://localhost:8081"
                style={{ flex: 1 }}
              />
              <button 
                onClick={() => checkApiHealth(new AbortController().signal)} 
                disabled={healthChecking}
                style={{ marginTop: 0 }}
              >
                {healthChecking ? 'Checking...' : 'Check Health'}
              </button>
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
        </div>
      )}

      {/* Session Selector - Only shown in creator mode, not in participant mode */}
      {!isParticipantMode && (
        <>
          <h2>Session Selection (Required)</h2>
          <div className="section" style={{ border: '2px solid #2196F3', backgroundColor: '#e3f2fd' }}>
            {!currentSession ? (
          <>
            {/* Zoom: Connect / Disconnect and Create session from Zoom */}
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
                  <a
                    href={`${apiBaseUrl}/auth/zoom/start?creator_identity=${encodeURIComponent(creatorIdentity)}`}
                    style={{ padding: '6px 12px', backgroundColor: '#2D8CFF', color: 'white', textDecoration: 'none', borderRadius: '4px', fontSize: '14px', display: 'inline-block' }}
                  >
                    Connect Zoom
                  </a>
                </div>
              )}
              {zoomConnection && (
                <div style={{ marginTop: '10px' }}>
                  <label style={{ display: 'block', marginBottom: '4px', fontSize: '13px' }}>Zoom recording URL (paste link):</label>
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
                  <div style={{ display: 'flex', gap: '8px', marginBottom: '8px', flexWrap: 'wrap' }}>
                    <button type="button" onClick={checkZoomTranscript} disabled={zoomCheckingTranscript || !zoomUrl?.trim()}>
                      {zoomCheckingTranscript ? 'Checking…' : 'Check transcript'}
                    </button>
                    <button type="button" onClick={createSessionFromZoom} disabled={zoomImporting}>
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
                  <label style={{ display: 'block', marginBottom: '4px', fontSize: '13px', marginTop: '8px' }}>Session title (optional):</label>
                  <input
                    type="text"
                    value={zoomTitle}
                    onChange={(e) => setZoomTitle(e.target.value)}
                    placeholder="e.g., Weekly review"
                    style={{ width: '100%', marginBottom: '8px', padding: '8px' }}
                  />
                  {zoomImportError && (
                    <div className="error" style={{ marginTop: '8px', fontSize: '13px' }}>{zoomImportError}</div>
                  )}
                </div>
              )}
            </div>

            <div style={{ marginBottom: '15px' }}>
              <label style={{ fontWeight: 'bold', marginBottom: '10px', display: 'block' }}>
                Select Mode:
              </label>
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

            {sessionMode === 'create' ? (
              <div>
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
                  <div className="form-group">
                    <label>Created By (optional):</label>
                    <input
                      type="text"
                      value={sessionCreatedBy}
                      onChange={(e) => setSessionCreatedBy(e.target.value)}
                      placeholder="anonymous"
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
              </div>
            ) : (
              <div>
                <div style={{ marginBottom: '15px', padding: '10px', backgroundColor: '#fff', borderRadius: '5px' }}>
                  <div style={{ fontWeight: 'bold', marginBottom: '10px' }}>Use Existing Session</div>
                  <div className="form-group">
                    <label>Enter Session ID:</label>
                    <div style={{ display: 'flex', gap: '10px' }}>
                      <input
                        type="text"
                        value={sessionIdInput}
                        onChange={(e) => setSessionIdInput(e.target.value)}
                        placeholder="Enter session UUID"
                        style={{ flex: 1 }}
                      />
                      <button onClick={() => loadSessionById('creator')} disabled={!sessionIdInput.trim() || loading}>
                        Load Session
                      </button>
                    </div>
                  </div>
                  {sessionSelectFeedback.message && (
                    <div className={sessionSelectFeedback.type} style={{ marginTop: '10px' }}>
                      {sessionSelectFeedback.message}
                    </div>
                  )}
                </div>

                {/* List sessions for current artifact */}
                {artifactId && (
                  <div style={{ marginTop: '15px', padding: '10px', backgroundColor: '#fff', borderRadius: '5px' }}>
                    <div style={{ fontWeight: 'bold', marginBottom: '10px' }}>
                      Sessions for Current Artifact ({sessions.length}):
                    </div>
                    {sessions.length === 0 ? (
                      <div className="info">No sessions found for this artifact. Create one above.</div>
                    ) : (
                      <div style={{ maxHeight: '200px', overflowY: 'auto' }}>
                        {sessions.map(session => (
                          <div key={session.id} style={{ 
                            padding: '10px', 
                            marginBottom: '8px', 
                            border: '1px solid #ddd', 
                            borderRadius: '5px',
                            cursor: 'pointer',
                            backgroundColor: session.status === 'open' ? '#e8f5e9' : '#f5f5f5',
                            transition: 'background-color 0.2s'
                          }} 
                          onMouseEnter={(e) => e.currentTarget.style.backgroundColor = session.status === 'open' ? '#c8e6c9' : '#e0e0e0'}
                          onMouseLeave={(e) => e.currentTarget.style.backgroundColor = session.status === 'open' ? '#e8f5e9' : '#f5f5f5'}
                          onClick={() => openSession(session.id)}>
                            <div style={{ fontWeight: 'bold', marginBottom: '5px' }}>{session.title}</div>
                            <div style={{ fontSize: '11px', color: '#666', marginBottom: '5px' }}>
                              ID: <code style={{ fontSize: '10px' }}>{session.id}</code>
                            </div>
                            <div style={{ fontSize: '12px', color: '#666' }}>
                              Status: <span style={{ 
                                color: session.status === 'open' ? '#4CAF50' : '#999',
                                fontWeight: 'bold'
                              }}>{session.status}</span>
                              {session.created_by && ` | Created by: ${session.created_by}`}
                            </div>
                          </div>
                        ))}
                      </div>
                    )}
                    {/* Sessions are now independent - no fetch needed */}
                    <button 
                      onClick={() => {}} 
                      disabled={true}
                      style={{ marginTop: '10px', fontSize: '12px', padding: '5px 10px', display: 'none' }}
                    >
                      Refresh List
                    </button>
                  </div>
                )}
                {!artifactId && (
                  <div className="info" style={{ marginTop: '10px' }}>
                    Create an artifact below to see its sessions, or enter a session ID directly above.
                  </div>
                )}
              </div>
            )}
          </>
        ) : (
          <div style={{ padding: '15px', backgroundColor: '#fff', borderRadius: '5px' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <div>
                <div style={{ fontWeight: 'bold', marginBottom: '5px' }}>
                  Active Session: {currentSession?.session?.title ?? 'Loading...'}
                </div>
                <div style={{ fontSize: '12px', color: '#666' }}>
                  ID: <code style={{ fontSize: '11px' }}>{currentSession?.session?.id ?? '—'}</code> | 
                  {currentSession?.artifacts && currentSession.artifacts.length > 0 ? (
                    <>Artifacts: {currentSession.artifacts.map(a => a.title).join(', ')} |</>
                  ) : (
                    <>No artifacts |</>
                  )} 
                  Status: <span style={{ 
                    color: currentSession?.session?.status === 'open' ? '#4CAF50' : '#999',
                    fontWeight: 'bold'
                  }}>{currentSession?.session?.status ?? '—'}</span>
                </div>
              </div>
              <button 
                onClick={() => { 
                  setCurrentSession(null)
                  setViewMode('artifact')
                  setSessionSelectFeedback({ type: '', message: '' })
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
          </div>
        )}
      </div>
        </>
      )}

      {/* Artifact Creation - Only visible when NOT in participant mode */}
      {!isParticipantMode && (
        <>
          <h2>1. Create Artifact</h2>
          <div className="section">
            <div className="form-group">
              <label>Title:</label>
              <input
                type="text"
                value={artifactTitle}
                onChange={(e) => setArtifactTitle(e.target.value)}
                placeholder="My Artifact Title"
              />
            </div>
            <div className="form-group">
              <label>Description (optional):</label>
              <textarea
                value={artifactDescription}
                onChange={(e) => setArtifactDescription(e.target.value)}
                placeholder="Optional description"
              />
            </div>
            <button onClick={createArtifact} disabled={!artifactTitle || loading}>
              Create Artifact
            </button>
            {createArtifactFeedback.message && (
              <div className={createArtifactFeedback.type} style={{ marginTop: '10px' }}>
                {createArtifactFeedback.message}
              </div>
            )}
            {loading && <div className="loading" style={{ marginTop: '10px' }}>Loading...</div>}
          </div>
        </>
      )}

      {/* Phase 3: Sessions Section - Hidden when session is selected, shown in artifact view, only in creator mode */}
      {artifactId && viewMode === 'artifact' && !currentSession && !isParticipantMode && (
        <div className="section">
          <h2>Phase 3: Sessions</h2>
          
          <div className="form-group">
            <label>Create New Session:</label>
            <input
              type="text"
              value={sessionTitle}
              onChange={(e) => setSessionTitle(e.target.value)}
              placeholder="Session title (e.g., Weekly review - Jan 2026)"
              style={{ marginBottom: '10px' }}
            />
            <input
              type="text"
              value={sessionCreatedBy}
              onChange={(e) => setSessionCreatedBy(e.target.value)}
              placeholder="Created by (optional)"
              style={{ marginBottom: '10px' }}
            />
            <button onClick={createSession} disabled={!sessionTitle || loading}>
              Create Session
            </button>
            {createSessionFeedback.message && (
              <div className={createSessionFeedback.type} style={{ marginTop: '10px' }}>
                {createSessionFeedback.message}
              </div>
            )}
          </div>

          {sessions.length > 0 && (
            <div style={{ marginTop: '20px' }}>
              <label><strong>Sessions for this Artifact ({sessions.length}):</strong></label>
              <div style={{ marginTop: '10px' }}>
                {sessions.map(session => (
                  <div key={session.id} style={{ 
                    padding: '12px', 
                    marginBottom: '10px', 
                    border: '1px solid #ddd', 
                    borderRadius: '5px',
                    cursor: 'pointer',
                    backgroundColor: session.status === 'open' ? '#e8f5e9' : '#f5f5f5',
                    transition: 'background-color 0.2s'
                  }} 
                  onMouseEnter={(e) => e.currentTarget.style.backgroundColor = session.status === 'open' ? '#c8e6c9' : '#e0e0e0'}
                  onMouseLeave={(e) => e.currentTarget.style.backgroundColor = session.status === 'open' ? '#e8f5e9' : '#f5f5f5'}
                  onClick={() => openSession(session.id)}>
                    <div style={{ fontWeight: 'bold', marginBottom: '5px' }}>{session.title}</div>
                    <div style={{ fontSize: '12px', color: '#666' }}>
                      Status: <span style={{ 
                        color: session.status === 'open' ? '#4CAF50' : '#999',
                        fontWeight: 'bold'
                      }}>{session.status}</span> | 
                      {session.created_by && `Created by: ${session.created_by} | `}
                      Created: {new Date(session.created_at).toLocaleString()}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}
          {sessions.length === 0 && artifactId && (
            <div style={{ marginTop: '20px', padding: '10px', backgroundColor: '#fff3cd', borderRadius: '5px', fontSize: '14px' }}>
              No sessions yet. Create one above to get started!
            </div>
          )}
        </div>
      )}

      {/* Session View - Mode-based rendering */}
      {/* Render mode components when viewMode is 'session', even if currentSession is null (e.g., API error) */}
      {viewMode === 'session' && (
        <>
          {/* Session header is now inside each mode component */}
          {/* Use isParticipantMode to determine which component to render */}
          {!isParticipantMode ? (
            <CreatorMode
              currentSession={currentSession}
              artifactId={artifactId}
              setArtifactId={setArtifactId}
              videoId={videoId}
              setVideoId={setVideoId}
              selectedVideo={selectedVideo}
              setSelectedVideo={setSelectedVideo}
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
              questions={[...questions, ...mockQuestions]}
              fetchSessionQuestions={fetchSessionQuestions}
              loading={loading}
              apiBaseUrl={apiBaseUrl}
              creatorIdentity={creatorIdentity}
              viewMode={viewMode}
              setViewMode={setViewMode}
              materialFiles={materialFiles}
              setMaterialFiles={setMaterialFiles}
              uploadedMaterials={uploadedMaterials}
              setUploadedMaterials={setUploadedMaterials}
              removeMaterialFile={removeMaterialFile}
              materialKind={materialKind}
              setMaterialKind={setMaterialKind}
              uploadMaterial={uploadMaterial}
              uploadMaterialFeedback={uploadMaterialFeedback}
              videoProvider={videoProvider}
              setVideoProvider={setVideoProvider}
              videoUrl={videoUrl}
              setVideoUrl={setVideoUrl}
              playbackMode={playbackMode}
              setPlaybackMode={setPlaybackMode}
              embedUrl={embedUrl}
              setEmbedUrl={setEmbedUrl}
              mediaUrl={mediaUrl}
              setMediaUrl={setMediaUrl}
              posterUrl={posterUrl}
              setPosterUrl={setPosterUrl}
              durationSeconds={durationSeconds}
              setDurationSeconds={setDurationSeconds}
              attachVideo={attachVideo}
              attachVideoFeedback={attachVideoFeedback}
              videoFile={videoFile}
              setVideoFile={setVideoFile}
              videoFileUploading={videoFileUploading}
              uploadVideoFile={uploadVideoFile}
              loomVideoSource={loomVideoSource}
              setLoomVideoSource={setLoomVideoSource}
              transcriptText={transcriptText}
              setTranscriptText={setTranscriptText}
              submitTranscript={submitTranscript}
              submitTranscriptFeedback={submitTranscriptFeedback}
              transcriptFile={transcriptFile}
              setTranscriptFile={setTranscriptFile}
              transcriptFileUploading={transcriptFileUploading}
              uploadTranscriptFile={uploadTranscriptFile}
            />
          ) : (
            <div className="participant-layout-root">
              <ParticipantMode
                currentSession={currentSession}
                selectedVideo={selectedVideo}
                setSelectedVideo={setSelectedVideo}
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
                questions={[...questions, ...mockQuestions]}
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
              />
            </div>
          )}
        </>
      )}

      {/* Artifact View - Only visible in creator mode */}
      {viewMode === 'artifact' && currentSession && sessionUserMode === 'creator' && (
        <>
          {/* Show existing artifacts */}
          {currentSession.artifacts && currentSession.artifacts.length > 0 && (
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

          {/* Show existing materials */}
          {currentSession.materials && currentSession.materials.length > 0 && (
            <div className="section" style={{ marginBottom: '20px', backgroundColor: '#f9f9f9', border: '1px solid #ddd' }}>
              <h2>Existing Materials ({currentSession.materials.length})</h2>
              {currentSession.materials.map((material, idx) => (
                <div key={material.id || idx} style={{ 
                  marginBottom: '15px', 
                  padding: '15px', 
                  border: '1px solid #ddd', 
                  borderRadius: '5px',
                  backgroundColor: '#fff'
                }}>
                  <div style={{ fontWeight: 'bold', marginBottom: '5px' }}>
                    📄 {material.filename || 'Untitled'}
                  </div>
                  <div style={{ fontSize: '12px', color: '#666', marginBottom: '10px' }}>
                    Kind: <strong>{material.kind}</strong> | 
                    Type: <strong>{material.content_type}</strong> | 
                    Status: <span style={{ 
                      color: material.text_status === 'ready' ? '#4CAF50' : 
                             material.text_status === 'pending' ? '#ff9800' : '#f44336',
                      fontWeight: 'bold'
                    }}>{material.text_status}</span>
                  </div>
                  {material.storage_url && (
                    <div style={{ fontSize: '11px', color: '#999', marginBottom: '5px' }}>
                      Storage: <code>{material.storage_url}</code>
                    </div>
                  )}
                  {material.extracted_text && (
                    <div style={{ 
                      marginTop: '10px', 
                      padding: '10px', 
                      backgroundColor: '#f5f5f5', 
                      borderRadius: '3px',
                      fontSize: '13px',
                      maxHeight: '150px',
                      overflow: 'auto',
                      border: '1px solid #e0e0e0'
                    }}>
                      <strong>Extracted Text Preview:</strong>
                      <div style={{ marginTop: '5px', fontStyle: 'italic', color: '#555' }}>
                        {material.extracted_text.length > 500 
                          ? material.extracted_text.substring(0, 500) + '...' 
                          : material.extracted_text}
                      </div>
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}

          {/* Video Player Section (Artifact View) */}
          {currentSession.video_sources && currentSession.video_sources.length > 0 && (
            <div className="section" style={{ marginBottom: '20px', backgroundColor: '#fff3e0', border: '1px solid #ff9800' }}>
              <h2>Video Player</h2>
              
              {/* Video Selection */}
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
                                          video.transcript_status === 'pending' ? 'Processing...' :
                                          video.transcript_status === 'ready' ? 'Ready' :
                                          video.transcript_status === 'failed' ? 'Failed' :
                                          video.transcript_status || 'Unknown'
                      return (
                        <option key={video.id} value={video.id}>
                          Video {idx + 1} - {video.provider} ({statusLabel})
                        </option>
                      )
                    })}
                  </select>
                </div>
              )}

              {/* Video Player */}
              {(() => {
                const video = selectedVideo || currentSession.video_sources[0]
                
                return (
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
                        {video.transcript_status}
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

                    {/* Video Player Component */}
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

                    {/* Transcript Display */}
                    {video.transcript_text && (
                      <div style={{ 
                        marginTop: '20px', 
                        padding: '15px', 
                        backgroundColor: '#f5f5f5', 
                        borderRadius: '8px',
                        fontSize: '14px',
                        maxHeight: '300px',
                        overflow: 'auto',
                        border: '1px solid #e0e0e0'
                      }}>
                        <strong style={{ display: 'block', marginBottom: '10px' }}>Transcript:</strong>
                        <div style={{ 
                          fontStyle: 'italic', 
                          color: '#555', 
                          whiteSpace: 'pre-wrap',
                          lineHeight: '1.6'
                        }}>
                          {video.transcript_text}
                        </div>
                      </div>
                    )}
                  </div>
                )
              })()}
            </div>
          )}

          <h2>2. Upload Material</h2>
      <div className="section">
        <div className="form-group">
          <label>File:</label>
          <input
            type="file"
            multiple
            onChange={(e) => {
              const files = Array.from(e.target.files || [])
              setMaterialFiles(prev => [...prev, ...files])
            }}
          />
        </div>
        <div className="form-group">
          <label>Kind:</label>
          <select value={materialKind} onChange={(e) => setMaterialKind(e.target.value)}>
            <option value="document">Document</option>
            <option value="slides">Slides</option>
            <option value="diagram">Diagram</option>
            <option value="other">Other</option>
          </select>
        </div>
        <button onClick={() => materialFiles.length > 0 && uploadMaterial(materialFiles[0])} disabled={!artifactId || materialFiles.length === 0 || loading}>
          Upload Material
        </button>
        {uploadMaterialFeedback.message && (
          <div className={uploadMaterialFeedback.type} style={{ marginTop: '10px' }}>
            {uploadMaterialFeedback.message}
          </div>
        )}
      </div>

      <h2>3. Attach Video URL</h2>
      <div className="section">
        <div className="form-group">
          <label>Provider:</label>
          <select value={videoProvider} onChange={(e) => setVideoProvider(e.target.value)}>
            <option value="loom">Loom</option>
            <option value="zoom">Zoom</option>
            <option value="other">Other</option>
          </select>
        </div>
        <div className="form-group">
          <label>Video URL:</label>
          <input
            type="url"
            value={videoUrl}
            onChange={(e) => setVideoUrl(e.target.value)}
            placeholder="https://www.loom.com/share/..."
          />
        </div>
        <button onClick={attachVideo} disabled={!artifactId || !videoUrl || loading}>
          Attach Video
        </button>
        {attachVideoFeedback.message && (
          <div className={attachVideoFeedback.type} style={{ marginTop: '10px' }}>
            {attachVideoFeedback.message}
          </div>
        )}
      </div>

      <h2>4. Submit Transcript</h2>
      <div className="section">
        <div className="form-group">
          <label>Transcript Text:</label>
          <textarea
            value={transcriptText}
            onChange={(e) => setTranscriptText(e.target.value)}
            placeholder="Paste transcript text here..."
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

      <h2>5. Ask Question</h2>
      <div className="section">
        {currentSession && (
          <>
            <div style={{ display: 'flex', gap: '10px', alignItems: 'center', marginBottom: '10px' }}>
              <button
                onClick={toggleVoiceRecording}
                disabled={loading || voiceUploading}
                style={{
                  marginTop: 0,
                  backgroundColor: voiceRecording ? '#d32f2f' : '#1976D2',
                  padding: '8px 12px'
                }}
              >
                {voiceRecording ? 'Stop Mic' : (voiceUploading ? 'Processing…' : 'Mic')}
              </button>
              <div style={{ fontSize: '13px', color: voiceRecording ? '#d32f2f' : '#666', display: 'flex', alignItems: 'center', gap: '8px' }}>
                {voiceRecording ? (
                  'Listening…'
                ) : voiceUploading ? (
                  <>
                    <span className="spinner" aria-label="Processing voice to text" />
                    <span>Processing…</span>
                  </>
                ) : (
                  ''
                )}
              </div>
            </div>
            {voiceFeedback.message && (
              <div className={voiceFeedback.type} style={{ marginBottom: '10px' }}>
                {voiceFeedback.message}
              </div>
            )}
            {showVoiceConfirm && (
              <div style={{ marginBottom: '15px', padding: '12px', border: '1px solid #ddd', borderRadius: '6px', backgroundColor: '#f9f9f9' }}>
                <div style={{ fontWeight: 600, marginBottom: '8px' }}>Review transcription</div>
                <textarea
                  value={voiceTranscribedText}
                  onChange={(e) => setVoiceTranscribedText(e.target.value)}
                  rows={3}
                  style={{ width: '100%', marginBottom: '10px' }}
                />
                <div style={{ display: 'flex', gap: '10px' }}>
                  <button
                    onClick={confirmVoiceQuestion}
                    disabled={!voiceTranscribedText.trim() || loading}
                    style={{ marginTop: 0 }}
                  >
                    Confirm & Submit (Session)
                  </button>
                  <button
                    onClick={() => { setShowVoiceConfirm(false); setVoiceTranscribedText('') }}
                    disabled={loading}
                    style={{ marginTop: 0, backgroundColor: '#757575' }}
                  >
                    Cancel
                  </button>
                </div>
              </div>
            )}
          </>
        )}
        <div className="form-group">
          <label>Question:</label>
          <textarea
            value={questionText}
            onChange={(e) => setQuestionText(e.target.value)}
            placeholder="What is the main topic discussed?"
          />
        </div>
        <button onClick={askQuestion} disabled={!artifactId || !questionText || loading}>
          Ask Question
        </button>
        {askQuestionFeedback.message && (
          <div className={askQuestionFeedback.type} style={{ marginTop: '10px' }}>
            {askQuestionFeedback.message}
          </div>
        )}
      </div>

      {currentAnswer && (
        <div className="section">
          <h3>Latest Answer</h3>
          <div className="question-item">
            <div className="question-text">Q: {currentAnswer.question.question_text}</div>
            <div>
              <span className={`answer-status ${currentAnswer.answer.answer_status}`}>
                {currentAnswer.answer.answer_status}
              </span>
              <span className="confidence">Confidence: {(currentAnswer.answer.confidence * 100).toFixed(1)}%</span>
            </div>
            <div className="answer-text">{currentAnswer.answer.answer_text}</div>
            {currentAnswer.answer.citations && currentAnswer.answer.citations.length > 0 && (
              <div className="citations">
                <strong>Citations:</strong>
                {currentAnswer.answer.citations.map((citation, idx) => (
                  <div key={idx} className="citation">
                    <div className="citation-source">
                      {citation.source_type} - {citation.source_id}
                      {citation.chunk_id && ` [chunk: ${citation.chunk_id}]`}
                      {citation.locator && ` (${citation.locator})`}
                    </div>
                    <div className="citation-snippet">{citation.snippet}</div>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      )}

      <h2>6. Question History</h2>
      <div className="section">
        <div style={{ display: 'flex', gap: '10px', alignItems: 'center', marginBottom: '15px', flexWrap: 'wrap' }}>
          <button onClick={fetchQuestions} disabled={!artifactId || loading}>
            Load Questions
          </button>
          {currentSession && (currentSession.session?.id || currentSession.id) && (
            <button 
              onClick={async () => {
                const sessionId = currentSession.session?.id || currentSession.id
                if (!sessionId) return
                
                setMockQuestionLoading(true)
                try {
                  const response = await fetch(`${apiBaseUrl}/sessions/${sessionId}/questions/mock`, {
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
                  
                  // Immediately add mock question to state for instant feedback in creator window
                  // The WebSocket message will also trigger an update, but this ensures immediate visibility
                  if (data.question) {
                    const questionWithAnswer = {
                      ...data.question,
                      answer: data.answer || null
                    }
                    console.log('Adding mock question to state immediately:', questionWithAnswer)
                    setMockQuestions(prev => {
                      // Check if question already exists (avoid duplicates from WebSocket message)
                      const exists = prev.some(q => q.id === questionWithAnswer.id)
                      if (exists) {
                        console.log('Mock question already exists in state, skipping duplicate')
                        return prev
                      }
                      console.log('Adding new mock question to state')
                      return [...prev, questionWithAnswer]
                    })
                  }
                } catch (err) {
                  console.error('Error creating mock question:', err)
                } finally {
                  setMockQuestionLoading(false)
                }
              }}
              disabled={mockQuestionLoading || loading || !currentSession || (!currentSession.session?.id && !currentSession.id)}
              style={{ 
                backgroundColor: (mockQuestionLoading || loading || !currentSession || (!currentSession.session?.id && !currentSession.id)) ? '#ccc' : '#9c27b0', 
                color: 'white',
                padding: '8px 16px',
                borderRadius: '4px',
                border: 'none',
                cursor: (mockQuestionLoading || loading || !currentSession || (!currentSession.session?.id && !currentSession.id)) ? 'not-allowed' : 'pointer',
                fontWeight: 'bold',
                fontSize: '14px',
                boxShadow: (mockQuestionLoading || loading || !currentSession || (!currentSession.session?.id && !currentSession.id)) ? 'none' : '0 2px 4px rgba(0,0,0,0.2)',
                transition: 'all 0.2s'
              }}
              title={!currentSession || (!currentSession.session?.id && !currentSession.id) ? 'Please select a session first' : 'Create a mock question to test WebSocket functionality (not persisted to database)'}
            >
              {mockQuestionLoading ? 'Creating...' : '🧪 Test WebSocket'}
            </button>
          )}
        </div>
        {questionHistoryFeedback.message && (
          <div className={questionHistoryFeedback.type} style={{ marginTop: '10px' }}>
            {questionHistoryFeedback.message}
          </div>
        )}
        {(questions.length > 0 || mockQuestions.length > 0) && (
          <div style={{ marginTop: '20px' }}>
            {[...questions, ...mockQuestions].map((q) => (
              <div key={q.id} className="question-item">
                <div className="question-text">Q: {q.question_text}</div>
                {q.answer ? (
                  <>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '15px', flexWrap: 'wrap', marginBottom: '10px' }}>
                      <span className={`answer-status ${q.answer.answer_status}`}>
                        {q.answer.answer_status}
                      </span>
                      <span className="confidence">Confidence: {(q.answer.confidence * 100).toFixed(1)}%</span>
                    </div>
                    <div className="answer-text">{q.answer.answer_text}</div>
                    {q.answer.answer_status === 'answered' && !isParticipantMode && (
                      <div style={{ marginTop: '10px', padding: '10px', backgroundColor: '#f0f8ff', borderRadius: '4px', border: '1px solid #2196F3' }}>
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
                                alert('Session ID not found. Please ensure you are viewing a session.')
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
                                
                                // Refresh questions to get updated confirmation status
                                if (fetchQuestions) {
                                  fetchQuestions()
                                }
                                // Also refresh session questions if available
                                if (fetchSessionQuestions && sessionId) {
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
                    {q.answer.citations && q.answer.citations.length > 0 && (
                      <div className="citations">
                        <strong>Citations:</strong>
                        {q.answer.citations.map((citation, cidx) => (
                          <div key={cidx} className="citation">
                            <div className="citation-source">
                              {citation.source_type} - {citation.source_id}
                              {citation.locator && ` (${citation.locator})`}
                            </div>
                            <div className="citation-snippet">{citation.snippet}</div>
                          </div>
                        ))}
                      </div>
                    )}
                  </>
                ) : (
                  <div className="loading">No answer yet</div>
                )}
              </div>
            ))}
          </div>
        )}
      </div>
        </>
      )}

      {/* Reset All Data - Only visible in creator mode */}
      {!isParticipantMode && (
        <>
          <h2>⚠ Reset All Data (Dev Only)</h2>
          <div className="section" style={{ border: '2px solid #fcc', backgroundColor: '#fff5f5' }}>
        <div style={{ marginBottom: '15px', color: '#c33', fontWeight: 600 }}>
          ⚠️ WARNING: This will delete ALL artifacts, materials, videos, questions, and answers!
        </div>
        
        {!showResetConfirm ? (
          <>
            <div className="form-group">
              <label>This action cannot be undone. Click below to confirm:</label>
            </div>
            <button 
              onClick={() => setShowResetConfirm(true)}
              style={{ backgroundColor: '#dc3545', marginTop: 0 }}
            >
              Show Reset Confirmation
            </button>
          </>
        ) : (
          <>
            <div className="form-group">
              <label>
                Type <strong>RESET</strong> to confirm deletion:
              </label>
              <input
                type="text"
                value={resetConfirmText}
                onChange={(e) => setResetConfirmText(e.target.value)}
                placeholder="Type RESET to confirm"
                style={{ border: '2px solid #dc3545' }}
              />
            </div>
            <div style={{ display: 'flex', gap: '10px' }}>
              <button
                onClick={async () => {
                  if (resetConfirmText !== 'RESET') {
                    setResetFeedback({ type: 'error', message: 'Please type RESET exactly to confirm' })
                    return
                  }

                  clearFeedback(setResetFeedback)
                  setLoading(true)

                  try {
                    const response = await fetch(`${apiBaseUrl}/admin/reset`, {
                      method: 'POST',
                      headers: { 'Content-Type': 'application/json' }
                    })

                    const data = await response.json()

                    if (!response.ok) {
                      setResetFeedback({ type: 'error', message: `Reset failed: ${JSON.stringify(data, null, 2)}` })
                      return
                    }

                    setResetFeedback({ type: 'success', message: `Reset successful! ${JSON.stringify(data, null, 2)}` })
                    setArtifactId('')
                    setVideoId('')
                    setQuestions([])
                    setMockQuestions([])
                    setCurrentAnswer(null)
                    setResetConfirmText('')
                    setShowResetConfirm(false)
                    // Clear all other feedback
                    clearFeedback(setCreateArtifactFeedback)
                    clearFeedback(setUploadMaterialFeedback)
                    clearFeedback(setAttachVideoFeedback)
                    clearFeedback(setSubmitTranscriptFeedback)
                    clearFeedback(setAskQuestionFeedback)
                    clearFeedback(setQuestionHistoryFeedback)
                  } catch (err) {
                    setResetFeedback({ type: 'error', message: `Failed to reset: ${err.message}` })
                  } finally {
                    setLoading(false)
                  }
                }}
                disabled={resetConfirmText !== 'RESET' || loading}
                style={{ 
                  backgroundColor: '#dc3545',
                  marginTop: 0
                }}
              >
                {loading ? 'Resetting...' : '⚠ Reset All Data'}
              </button>
              <button
                onClick={() => {
                  setShowResetConfirm(false)
                  setResetConfirmText('')
                  clearFeedback(setResetFeedback)
                }}
                disabled={loading}
                style={{ 
                  backgroundColor: '#6c757d',
                  marginTop: 0
                }}
              >
                Cancel
              </button>
            </div>
            {resetFeedback.message && (
              <div className={resetFeedback.type} style={{ marginTop: '10px' }}>
                {resetFeedback.message}
              </div>
            )}
          </>
        )}
      </div>
        </>
      )}
    </div>
  )
}

export default App
