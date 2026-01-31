import { useEffect, useState, useRef } from 'react'
import { VideoPlayer } from '../VideoPlayer'
import { QAHistory } from '../components/QAHistory'
import { MaterialsTreePanel } from '../components/MaterialsTreePanel'
import { QAPanel } from '../components/QAPanel'
import { DocumentViewerOverlay } from '../components/DocumentViewerOverlay'

const STORAGE_KEY_MATERIALS_COLLAPSED = 'talkback.participant.materialsCollapsed'

export function ParticipantMode({
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
  confirmVoiceQuestion
}) {
  const hasSession = currentSession && currentSession.session

  const video = selectedVideo || (currentSession?.video_sources && currentSession.video_sources[0])

  const [materialsCollapsed, setMaterialsCollapsedState] = useState(false)

  const [overlayDocument, setOverlayDocument] = useState(null)
  const [overlayOpen, setOverlayOpen] = useState(false)
  const [selectedDocumentId, setSelectedDocumentId] = useState(null)
  const overlayTriggerRef = useRef(null)

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

  const handleSelectDocument = (doc, event) => {
    setOverlayDocument(doc)
    setOverlayOpen(true)
    setSelectedDocumentId(doc?.id ?? doc?.transcriptId ?? null)
    overlayTriggerRef.current = event?.currentTarget ?? null
  }

  const handleCloseOverlay = () => {
    setOverlayOpen(false)
    setOverlayDocument(null)
    const trigger = overlayTriggerRef.current
    overlayTriggerRef.current = null
    if (trigger && typeof trigger.focus === 'function') {
      try {
        trigger.focus()
      } catch {
        // ignore
      }
    }
  }

  if (!hasSession) {
    return (
      <div className="section">
        <div className="error" style={{ marginBottom: '20px' }}>
          Unable to load session. Please check the API connection and try again.
        </div>
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
          backgroundColor: '#4CAF50',
          color: 'white',
          padding: '4px 12px',
          borderRadius: '4px',
          fontWeight: 'bold',
          fontSize: '14px'
        }}>
          Participant
        </span>
      </div>

      <div className={gridClassName}>
        <aside className="participant-materials-panel">
          <MaterialsTreePanel
            session={currentSession}
            selectedVideo={selectedVideo}
            setSelectedVideo={setSelectedVideo}
            setVideoId={setVideoId}
            setVideoPlayerKey={setVideoPlayerKey}
            onSelectDocument={handleSelectDocument}
            selectedDocumentId={selectedDocumentId}
            collapsed={materialsCollapsed}
            onCollapsedChange={setMaterialsCollapsed}
          />
        </aside>

        <main className="participant-video-stage">
          <div style={{ padding: '12px', flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
            {currentSession.video_sources && currentSession.video_sources.length > 0 ? (
              <>
                {currentSession.video_sources.length > 1 && (
                  <div style={{ marginBottom: '10px' }}>
                    <label style={{ fontWeight: 'bold', marginRight: '8px' }}>Video:</label>
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
                          Video {idx + 1} – {v.provider}
                        </option>
                      ))}
                    </select>
                  </div>
                )}
                {video && (
                  <div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
                    <VideoPlayer
                      video={video}
                      onEvent={handleVideoPlayerEvent}
                      onTimeUpdate={handleVideoTimeUpdate}
                      currentTime={currentVideoTime}
                      playing={isVideoPlaying}
                    />
                  </div>
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
        </aside>
      </div>

      <DocumentViewerOverlay
        document={overlayDocument}
        open={overlayOpen}
        onClose={handleCloseOverlay}
      />
    </>
  )
}
