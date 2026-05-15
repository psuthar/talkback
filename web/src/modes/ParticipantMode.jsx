import { useEffect, useState, useRef, useCallback } from 'react'
import { VideoPlayer } from '../VideoPlayer'
import { QAHistory } from '../components/QAHistory'
import { MaterialsTreePanel, MaterialsPanelHeader } from '../components/MaterialsTreePanel'
import { QAPanel } from '../components/QAPanel'
import { TranscriptViewer } from '../components/TranscriptViewer'
import { DocumentViewer } from '../components/DocumentViewer'
import { resolvePrimaryAutoSelection } from '../components/sessionPrimaryAutoSelect'
import { getDefaultApiBaseUrl } from '../config'
import { VideoStartOverlay } from '../components/VideoStartOverlay'
import { SessionSkeleton } from '../components/SessionSkeleton'
import { DecisionBriefHeader } from '../components/DecisionBriefHeader'
import { DecisionBar } from '../components/DecisionBar'
import {
  STORAGE_KEY_MATERIALS_COLLAPSED,
  getStoredMaterialsCollapsed,
  setStoredMaterialsCollapsed,
  isParticipantOnboardingDismissed,
  setParticipantOnboardingDismissed,
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
  getParticipantContextFields,
} from '../utils/sessionSidebar'
import { ParticipantOnboardingDialog } from '../components/ParticipantOnboardingDialog'
import { ParticipantSessionMenu } from '../components/ParticipantSessionMenu'
import styles from './ParticipantMode.module.css'

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
  canDeleteQuestions = false,
  requestDeleteQuestion,
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
  polishQuestionText,
  voicePolishing,
  voicePolishMode,
  refetchSession,
  markMaterialsSeen,
  replyingToQuestionId,
  setReplyingToQuestionId,
  currentAskerName,
  onCitationClick,
  onClearSession,
  onLogout,
  debugMode = false,
  setDebugMode,
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

  const [materialsCollapsed, setMaterialsCollapsedState] = useState(false) // expanded by default on first visit; localStorage may override
  // Track link count "last seen" per session so we can show "New" when creator adds links (for other users)
  const [lastSeenLinkCountBySession, setLastSeenLinkCountBySession] = useState({})
  const [membersPanelExpanded, setMembersPanelExpandedState] = useState(false)
  const [contextPanelExpanded, setContextPanelExpandedState] = useState(false)
  const [materialsTreeExpanded, setMaterialsTreeExpandedState] = useState(true)
  const [showParticipantOnboarding, setShowParticipantOnboarding] = useState(false)
  const setMembersPanelExpanded = useCallback((value) => {
    setMembersPanelExpandedState((prev) => {
      const next = typeof value === 'function' ? value(prev) : value
      const sid = currentSession?.session?.id
      if (sid) setStoredMembersExpanded(sid, next)
      return next
    })
  }, [currentSession?.session?.id])
  const setContextPanelExpanded = useCallback((value) => {
    setContextPanelExpandedState((prev) => {
      const next = typeof value === 'function' ? value(prev) : value
      const sid = currentSession?.session?.id
      if (sid) setStoredContextExpanded(sid, next)
      return next
    })
  }, [currentSession?.session?.id])
  const setMaterialsTreeExpanded = useCallback((value) => {
    setMaterialsTreeExpandedState((prev) => {
      const next = typeof value === 'function' ? value(prev) : value
      const sid = currentSession?.session?.id
      if (sid) setStoredMaterialsTreeExpanded(sid, next)
      return next
    })
  }, [currentSession?.session?.id])

  // Decision stance state
  const [myStance, setMyStance] = useState(null)
  const [stanceAggregate, setStanceAggregate] = useState(null)
  const [stanceResponses, setStanceResponses] = useState([]) // per-person list with user_email
  const [stanceRationale, setStanceRationale] = useState('')
  const [stanceSubmitting, setStanceSubmitting] = useState(false)
  const [stanceFeedback, setStanceFeedback] = useState({ type: '', message: '' })
  const [stanceReadiness, setStanceReadiness] = useState(null)

  const fetchMyStance = useCallback(async () => {
    if (!currentSession?.session?.id || apiBaseUrl == null) return
    const base = (apiBaseUrl || '').replace(/\/$/, '')
    try {
      const res = await fetch(`${base}/api/sessions/${currentSession.session.id}/stances`, { credentials: 'include' })
      if (!res.ok) return
      const data = await res.json()
      setMyStance(data.my_stance ?? null)
      setStanceAggregate(data.aggregate ?? null)
      setStanceReadiness(data.readiness ?? null)
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
      if (data.readiness !== undefined) setStanceReadiness(data.readiness ?? null)
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
      if (data.readiness !== undefined) setStanceReadiness(data.readiness ?? null)
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
  // SCRUM-327: Once the user explicitly switches the center pane to a video
  // (e.g. by clicking a video row in the sidebar), the primary-material
  // auto-select effect must NOT later restore the primary document/link.
  // Tracked as a ref so it survives session refetches and re-renders without
  // forcing the auto-select effect to re-run.
  const userSelectedVideoRef = useRef(false)

  const [transcriptHighlightRange, setTranscriptHighlightRange] = useState(null)
  const transcriptHighlightTimerRef = useRef(null)
  /** Session transcript segments (from GET /sessions/:id/transcript) when video has no transcript_segments */
  const [sessionTranscriptSegments, setSessionTranscriptSegments] = useState(null)

  const setMaterialsCollapsed = (value) => {
    setMaterialsCollapsedState(value)
    if (currentSession?.session?.id) {
      setStoredMaterialsCollapsed(currentSession.session.id, value)
    }
  }

  useEffect(() => {
    if (!hasSession) return
    fetchSessionQuestions(currentSession.session.id)
  }, [hasSession])

  // SCRUM-274: when GET session reports an explicit primary (kind=document or
  // kind=link), default the center pane to that material so the participant
  // lands on the focal artifact without an extra left-panel click. kind=video
  // is the existing default. The decision lives in resolvePrimaryAutoSelection
  // so it can be unit-tested without rendering this component.
  // SCRUM-327: reset the user-selected-video guard when switching sessions so
  // the next session's primary-material auto-select still fires.
  useEffect(() => {
    userSelectedVideoRef.current = false
  }, [currentSession?.session?.id])

  useEffect(() => {
    if (!hasSession) return
    if (userSelectedVideoRef.current) return // SCRUM-327: respect user's explicit video selection
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
  }, [hasSession, currentSession?.session?.id, currentSession?.primary?.kind, currentSession?.primary?.id])

  useEffect(() => {
    const sid = currentSession?.session?.id
    if (!sid) return
    const stored = getStoredMaterialsCollapsed(sid)
    // First visit (no stored preference): default to expanded so participants discover materials.
    setMaterialsCollapsedState(stored === null ? false : stored)
  }, [currentSession?.session?.id])

  // Load per-section collapse preferences for the lifted sidebar siblings.
  // Context and Members force-collapse below the narrow viewport breakpoint
  // regardless of stored preference; the Materials sub-collapse always honors
  // its stored value so the tree stays discoverable.
  useEffect(() => {
    const sid = currentSession?.session?.id
    if (!sid) return
    const win = typeof window !== 'undefined' ? window : null
    // When a session has a recorded decision_outcome and the participant has no
    // stored preference yet, default Context to expanded so the outcome stays
    // surfaced even though SCRUM-456 removed the duplicate card over the video.
    const hasOutcome = typeof currentSession?.session?.decision_outcome === 'string'
      && currentSession.session.decision_outcome.trim().length > 0
    setContextPanelExpandedState(resolveInitialExpanded({
      stored: getStoredContextExpanded(sid),
      defaultExpanded: hasOutcome,
      honorNarrowOverride: true,
      win,
    }))
    setMembersPanelExpandedState(resolveInitialExpanded({
      stored: getStoredMembersExpanded(sid),
      defaultExpanded: false,
      honorNarrowOverride: true,
      win,
    }))
    setMaterialsTreeExpandedState(resolveInitialExpanded({
      stored: getStoredMaterialsTreeExpanded(sid),
      defaultExpanded: true,
      honorNarrowOverride: false,
      win,
    }))
  }, [currentSession?.session?.id])

  useEffect(() => {
    const sid = currentSession?.session?.id
    if (!sid) {
      setShowParticipantOnboarding(false)
      return
    }
    setShowParticipantOnboarding(!isParticipantOnboardingDismissed(sid))
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
    // SCRUM-327: mark the user's explicit choice so the auto-select effect
    // doesn't restore the primary document/link on a later session refetch.
    userSelectedVideoRef.current = true
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
    return <SessionSkeleton />
  }

  const gridClassName = materialsCollapsed
    ? 'participant-layout-grid materials-collapsed'
    : 'participant-layout-grid'

  const dismissParticipantOnboarding = () => {
    const sid = currentSession?.session?.id
    setParticipantOnboardingDismissed(sid)
    setShowParticipantOnboarding(false)
  }

  return (
    <>
      <ParticipantOnboardingDialog open={showParticipantOnboarding} onDismiss={dismissParticipantOnboarding} />

      <div className="participant-layout-topbar">
        <div className={styles.topbarInner}>
          <h2 className={styles.sessionTitle}>
            {currentSession.session.title}
          </h2>
          <span
            className={styles.roleBadge}
            style={{
              backgroundColor: currentSession.session.created_by === authUser?.email ? 'var(--color-success)' : '#757575',
            }}
          >
            {currentSession.session.created_by === authUser?.email ? 'Creator' : 'Participant'}
          </span>
        </div>
        <ParticipantSessionMenu
          authUser={authUser}
          onShowAllSessions={onClearSession}
          onLogout={onLogout}
          debugMode={debugMode}
          setDebugMode={setDebugMode}
        />
      </div>

      <DecisionBriefHeader
        premise={currentSession.session.premise}
        decision={currentSession.session.primary_decision}
        decisionOutcome={currentSession.session.decision_outcome}
        readiness={stanceReadiness}
      />

      <DecisionBar
        placement="top"
        primaryDecision={currentSession.session.primary_decision}
        decisionOutcome={currentSession.session.decision_outcome}
        myStance={myStance}
        stanceAggregate={stanceAggregate}
        stanceResponses={stanceResponses}
        sessionInvitations={sessionInvitations}
        stanceRationale={stanceRationale}
        setStanceRationale={setStanceRationale}
        stanceSubmitting={stanceSubmitting}
        stanceFeedback={stanceFeedback}
        submitStance={submitStance}
        clearStance={clearStance}
      />

      <div className={gridClassName}>
        <aside
          className={`participant-materials-panel ${materialsCollapsed ? 'materials-panel-collapsed' : 'materials-panel-expanded'}`}
          aria-expanded={!materialsCollapsed}
        >
          <MaterialsPanelHeader
            collapsed={materialsCollapsed}
            onCollapsedChange={setMaterialsCollapsed}
            unreadCount={Array.isArray(currentSession?.unread_material_ids) ? currentSession.unread_material_ids.length : 0}
            itemCount={
              (Array.isArray(currentSession?.video_sources) ? currentSession.video_sources.length : 0)
              + (Array.isArray(currentSession?.materials) ? currentSession.materials.length : 0)
              + (Array.isArray(currentSession?.links) ? currentSession.links.length : 0)
            }
          />
          {!materialsCollapsed && (
            <>
              {/* Context: read-only premise / primary decision / decision outcome.
                  Skipped entirely when all three fields are empty so the block
                  does not show up as an empty placeholder. */}
              {(() => {
                const ctx = getParticipantContextFields(currentSession)
                if (!ctx) return null
                return (
                  <div className={styles.panelSection} data-testid="participant-sidebar-context">
                    <button
                      type="button"
                      onClick={() => setContextPanelExpanded((e) => !e)}
                      aria-expanded={contextPanelExpanded}
                      aria-controls="participant-sidebar-context-region"
                      className={`${styles.collapsibleBtn} ${styles.collapsibleBtnMembers}`}
                      data-testid="participant-context-toggle"
                    >
                      <span className={styles.panelChevron} aria-hidden>{contextPanelExpanded ? '▼' : '▷'}</span>
                      Context
                    </button>
                    {contextPanelExpanded && (
                      <div id="participant-sidebar-context-region" className={styles.contextContent}>
                        {ctx.premise && (
                          <div className={styles.contextField}>
                            <span className={styles.contextFieldLabel}>Premise</span>
                            <p className={styles.contextFieldValue}>{ctx.premise}</p>
                          </div>
                        )}
                        {ctx.decision && (
                          <div className={styles.contextField}>
                            <span className={styles.contextFieldLabel}>Primary Decision</span>
                            <p className={styles.contextFieldValue}>{ctx.decision}</p>
                          </div>
                        )}
                        {ctx.outcome && (
                          <div className={styles.contextField}>
                            <span className={styles.contextFieldLabel}>Decision Outcome</span>
                            <p className={styles.contextFieldValue}>{ctx.outcome}</p>
                          </div>
                        )}
                      </div>
                    )}
                  </div>
                )
              })()}

              {/* Members: read-only list of invited members */}
              <div className={styles.panelSection} data-testid="participant-sidebar-members">
                <button
                  type="button"
                  onClick={() => setMembersPanelExpanded((e) => !e)}
                  aria-expanded={membersPanelExpanded}
                  aria-controls="participant-sidebar-members-region"
                  className={`${styles.collapsibleBtn} ${styles.collapsibleBtnMembers}`}
                  data-testid="participant-members-toggle"
                >
                  <span className={styles.panelChevron} aria-hidden>{membersPanelExpanded ? '▼' : '▷'}</span>
                  Members{Array.isArray(sessionInvitations) && sessionInvitations.length > 0 ? ` (${sessionInvitations.length})` : ''}
                </button>
                {membersPanelExpanded && (
                  <div id="participant-sidebar-members-region" className={styles.membersContent}>
                    {Array.isArray(sessionInvitations) && sessionInvitations.length > 0 ? (
                      <ul className={styles.membersList}>
                        {sessionInvitations.map((inv) => (
                          <li key={inv.id} className={styles.memberItem}>
                            <span className={styles.memberEmail}>{inv.invited_email}</span>
                            <span className={styles.memberRole}>({inv.invited_role || 'participant'}, {inv.status})</span>
                          </li>
                        ))}
                      </ul>
                    ) : (
                      <div className={styles.noMembersMsg}>No invited members listed.</div>
                    )}
                  </div>
                )}
              </div>

              {/* Materials sub-header. Sibling of Context/Members; toggles only
                  the tree, leaving the lifted blocks visible. The column-level
                  collapse remains owned by MaterialsPanelHeader above. */}
              <div className={styles.panelSection} data-testid="participant-sidebar-materials">
                <button
                  type="button"
                  onClick={() => setMaterialsTreeExpanded((e) => !e)}
                  aria-expanded={materialsTreeExpanded}
                  aria-controls="participant-sidebar-materials-region"
                  className={`${styles.collapsibleBtn} ${styles.collapsibleBtnMembers}`}
                  data-testid="participant-materials-tree-toggle"
                >
                  <span className={styles.panelChevron} aria-hidden>{materialsTreeExpanded ? '▼' : '▷'}</span>
                  Materials{(() => {
                    const n = sessionMaterialsCount(currentSession)
                    return n > 0 ? ` (${n})` : ''
                  })()}
                </button>
              </div>

              {materialsTreeExpanded && (
                <div id="participant-sidebar-materials-region">
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
                </div>
              )}
            </>
          )}
        </aside>

        <main className="participant-video-stage">
          <div className={styles.videoStageContent}>
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
                  <div
                    className={styles.videoPendingCard}
                    style={{ backgroundColor: currentSession.playback_reason_code === 'VIDEO_INGEST_PENDING' ? '#fff8e1' : '#f5f5f5' }}
                  >
                    <p className={styles.videoPendingText}>
                      {currentSession.playback_message || (currentSession.playback_reason_code === 'VIDEO_INGEST_PENDING' ? 'Video is still being prepared. Refresh in a moment.' : 'Video not available for this session.')}
                    </p>
                  </div>
                )}
                {video && !(currentSession?.session?.primary_video_artifact_id && !hasPrimaryR2Video && currentSession?.playback_reason_code) && (
                  <>
                    <div className={styles.videoPlayerWrap}>
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
                    <div className={styles.transcriptWrap}>
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
                        <div className={styles.transcriptUnavailable}>
                          Transcript: {video.transcript_status === 'pending' || video.transcript_status === 'processing' ? 'Processing…' : 'No transcript yet.'}
                        </div>
                      )}
                    </div>
                  </>
                )}
              </>
            ) : (
              <div className={styles.noVideosMsg}>
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
            canDeleteQuestions={canDeleteQuestions}
            requestDeleteQuestion={requestDeleteQuestion}
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
            polishQuestionText={polishQuestionText}
            voicePolishing={voicePolishing}
            voicePolishMode={voicePolishMode}
          />
        </aside>
      </div>

    </>
  )
}

