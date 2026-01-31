import { useState, useRef, useEffect } from 'react'
import { VideoPlayer, PlayerEvent } from '../VideoPlayer'
import { TranscriptViewer } from '../components/TranscriptViewer'
import { MaterialsList } from '../components/MaterialsList'
import { SessionSharing } from '../components/SessionSharing'

export function CreatorMode({
  currentSession,
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
  viewMode,
  setViewMode,
  // Upload props
  materialFiles,
  setMaterialFiles,
  uploadedMaterials,
  setUploadedMaterials,
  removeMaterialFile,
  materialKind,
  setMaterialKind,
  uploadMaterial,
  uploadMaterialFeedback,
  videoProvider,
  setVideoProvider,
  videoUrl,
  setVideoUrl,
  playbackMode,
  setPlaybackMode,
  embedUrl,
  setEmbedUrl,
  mediaUrl,
  setMediaUrl,
  posterUrl,
  setPosterUrl,
  durationSeconds,
  setDurationSeconds,
  attachVideo,
  attachVideoFeedback,
  videoFile,
  setVideoFile,
  videoFileUploading,
  uploadVideoFile,
  loomVideoSource,
  setLoomVideoSource,
  transcriptText,
  setTranscriptText,
  submitTranscript,
  submitTranscriptFeedback,
  transcriptFile,
  setTranscriptFile,
  transcriptFileUploading,
  uploadTranscriptFile
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

  const video = selectedVideo || (currentSession.video_sources && currentSession.video_sources[0])

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
  const participantUrl = sessionId 
    ? `${window.location.origin}${window.location.pathname}?session=${sessionId}&mode=view`
    : null

  return (
    <>
      {/* Session Header for Creator Mode */}
      {currentSession?.session && (
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
      {currentSession.session && (
        <SessionSharing 
          sessionId={currentSession.session.id} 
          sessionTitle={currentSession.session.title} 
        />
      )}

      {/* Existing Artifacts */}
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

      {/* Existing Materials */}
      {currentSession.materials && currentSession.materials.length > 0 && (
        <MaterialsList materials={currentSession.materials} />
      )}

      {/* Video Player */}
      {currentSession.video_sources && currentSession.video_sources.length > 0 && (
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
                  {statusLabel}
                  {video.source_type && (
                    <span style={{ marginLeft: '8px', fontSize: '11px', color: '#666' }}>
                      ({sourceTypeLabel})
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
              />

              {video.transcript_text && (
                <TranscriptViewer transcriptText={video.transcript_text} />
              )}
            </div>
          )}
        </div>
      )}

      {/* Upload Sections - Only visible in Creator Mode */}
      <h2>Upload Material</h2>
      <div className="section">
        <div className="form-group">
          <label>Select Files (multiple):</label>
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
        
        {/* List of selected files waiting to upload */}
        {materialFiles.length > 0 && (
          <div style={{ marginBottom: '15px', padding: '10px', backgroundColor: '#f5f5f5', borderRadius: '4px' }}>
            <div style={{ fontWeight: 'bold', marginBottom: '10px' }}>Files to Upload ({materialFiles.length}):</div>
            {materialFiles.map((file, index) => (
              <div key={index} style={{ 
                display: 'flex', 
                justifyContent: 'space-between', 
                alignItems: 'center', 
                padding: '8px', 
                marginBottom: '5px', 
                backgroundColor: 'white', 
                borderRadius: '4px',
                border: '1px solid #ddd'
              }}>
                <span style={{ flex: 1 }}>
                  {file.name} ({(file.size / 1024).toFixed(2)} KB)
                </span>
                <div style={{ display: 'flex', gap: '5px' }}>
                  <button
                    onClick={() => uploadMaterial(file)}
                    disabled={!artifactId || loading}
                    style={{ padding: '4px 8px', fontSize: '12px' }}
                  >
                    Upload
                  </button>
                  <button
                    onClick={() => removeMaterialFile(index)}
                    disabled={loading}
                    style={{ padding: '4px 8px', fontSize: '12px', backgroundColor: '#f44336', color: 'white', border: 'none', borderRadius: '4px' }}
                  >
                    Remove
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}

        {/* List of uploaded materials */}
        {uploadedMaterials.length > 0 && (
          <div style={{ marginBottom: '15px', padding: '10px', backgroundColor: '#e8f5e9', borderRadius: '4px' }}>
            <div style={{ fontWeight: 'bold', marginBottom: '10px', color: '#2e7d32' }}>Uploaded Materials ({uploadedMaterials.length}):</div>
            {uploadedMaterials.map((material, index) => (
              <div key={material.id || index} style={{ 
                padding: '8px', 
                marginBottom: '5px', 
                backgroundColor: 'white', 
                borderRadius: '4px',
                border: '1px solid #4CAF50'
              }}>
                <div style={{ fontWeight: 'bold' }}>{material.filename}</div>
                <div style={{ fontSize: '12px', color: '#666' }}>
                  Kind: {material.kind} | Status: {material.text_status}
                </div>
              </div>
            ))}
          </div>
        )}

        {uploadMaterialFeedback.message && (
          <div className={uploadMaterialFeedback.type} style={{ marginTop: '10px' }}>
            {uploadMaterialFeedback.message}
          </div>
        )}
      </div>

      <h2>Attach Video</h2>
      <div className="section">
        {/* Transcript file upload option */}
        <div className="form-group" style={{ marginBottom: '20px', padding: '15px', backgroundColor: '#e3f2fd', borderRadius: '5px', border: '1px solid #2196F3' }}>
          <label style={{ fontWeight: 'bold', marginBottom: '10px', display: 'block' }}>Upload Transcript (MP4):</label>
          <input
            type="file"
            accept="video/mp4"
            onChange={(e) => {
              const file = e.target.files?.[0]
              if (file) {
                setTranscriptFile(file)
              }
            }}
            style={{ marginBottom: '10px', width: '100%' }}
            id="transcript-file-input-video-section"
          />
          {transcriptFile && (
            <div style={{ marginBottom: '10px', padding: '10px', backgroundColor: 'white', borderRadius: '4px', border: '1px solid #2196F3' }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: '10px' }}>
                <span>
                  <strong>Selected:</strong> {transcriptFile.name} ({(transcriptFile.size / 1024 / 1024).toFixed(2)} MB)
                </span>
                <button
                  onClick={uploadTranscriptFile}
                  disabled={!currentSession?.session?.id || transcriptFileUploading || loading}
                  style={{ 
                    padding: '6px 12px',
                    backgroundColor: (transcriptFileUploading || loading) ? '#ccc' : '#2196F3',
                    color: 'white',
                    border: 'none',
                    borderRadius: '4px',
                    cursor: (transcriptFileUploading || loading) ? 'not-allowed' : 'pointer'
                  }}
                >
                  {transcriptFileUploading ? 'Transcribing...' : 'Upload and Transcribe'}
                </button>
              </div>
            </div>
          )}
          <div style={{ fontSize: '12px', color: '#666', fontStyle: 'italic' }}>
            Upload an MP4 file to automatically transcribe it. The transcript will be populated in the "Submit Transcript" section below.
          </div>
        </div>

        {/* Loom guidance callout */}
        {loomVideoSource && loomVideoSource.source_type === 'embed_url' && (
          <div style={{ 
            marginBottom: '20px', 
            padding: '15px', 
            backgroundColor: '#fff3cd', 
            borderRadius: '5px', 
            border: '2px solid #ffc107' 
          }}>
            <div style={{ marginBottom: '10px', fontWeight: 'bold', color: '#856404' }}>
              Loom Share URL Detected
            </div>
            <div style={{ marginBottom: '15px', color: '#856404' }}>
              We can't transcribe a Loom share page directly. Please download the MP4 from Loom and upload it here — once uploaded, we'll transcribe it automatically.
            </div>
            <div>
              <input
                type="file"
                accept="video/mp4"
                onChange={(e) => {
                  const file = e.target.files?.[0]
                  if (file) {
                    setVideoFile(file)
                  }
                }}
                style={{ marginBottom: '10px', width: '100%' }}
                id="loom-video-file-input"
              />
              {videoFile && (
                <div style={{ marginTop: '10px', padding: '10px', backgroundColor: '#e8f5e9', borderRadius: '4px', border: '1px solid #4CAF50' }}>
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: '10px' }}>
                    <span>
                      <strong>Selected:</strong> {videoFile.name} ({(videoFile.size / 1024 / 1024).toFixed(2)} MB)
                    </span>
                    <button
                      onClick={uploadVideoFile}
                      disabled={!currentSession?.session?.id || videoFileUploading || loading}
                      style={{ 
                        padding: '6px 12px',
                        backgroundColor: (videoFileUploading || loading) ? '#ccc' : '#4CAF50',
                        color: 'white',
                        border: 'none',
                        borderRadius: '4px',
                        cursor: (videoFileUploading || loading) ? 'not-allowed' : 'pointer'
                      }}
                    >
                      {videoFileUploading ? 'Uploading...' : 'Upload MP4'}
                    </button>
                  </div>
                </div>
              )}
            </div>
          </div>
        )}
        <div className="form-group">
          <label>Upload MP4 File:</label>
          <input
            type="file"
            accept="video/mp4"
            onChange={(e) => {
              const file = e.target.files?.[0]
              if (file) {
                setVideoFile(file)
              }
            }}
            style={{ marginBottom: '10px', width: '100%' }}
            id="video-file-input"
          />
          {videoFile && !loomVideoSource && (
            <div style={{ marginBottom: '15px', padding: '10px', backgroundColor: '#e8f5e9', borderRadius: '4px', border: '1px solid #4CAF50' }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: '10px' }}>
                <span>
                  <strong>Selected:</strong> {videoFile.name} ({(videoFile.size / 1024 / 1024).toFixed(2)} MB)
                </span>
                <button
                  onClick={uploadVideoFile}
                  disabled={!currentSession?.session?.id || videoFileUploading || loading}
                  style={{ 
                    padding: '6px 12px',
                    backgroundColor: (videoFileUploading || loading) ? '#ccc' : '#4CAF50',
                    color: 'white',
                    border: 'none',
                    borderRadius: '4px',
                    cursor: (videoFileUploading || loading) ? 'not-allowed' : 'pointer'
                  }}
                >
                  {videoFileUploading ? 'Uploading...' : 'Upload MP4'}
                </button>
              </div>
            </div>
          )}
        </div>
        <div className="form-group">
          <label>Or Paste Video URL:</label>
          <input
            type="url"
            value={videoUrl}
            onChange={(e) => setVideoUrl(e.target.value)}
            placeholder="https://www.loom.com/share/... or https://example.com/video.mp4"
            style={{ width: '100%' }}
          />
        </div>
        <div className="form-group">
          <label>Provider:</label>
          <select value={videoProvider} onChange={(e) => setVideoProvider(e.target.value)}>
            <option value="loom">Loom</option>
            <option value="zoom">Zoom</option>
            <option value="other">Other</option>
          </select>
        </div>
        <button onClick={attachVideo} disabled={!artifactId || !videoUrl || loading}>
          Attach Video URL
        </button>
        {attachVideoFeedback.message && (
          <div className={attachVideoFeedback.type} style={{ marginTop: '10px' }}>
            {attachVideoFeedback.message}
          </div>
        )}
      </div>

      <h2>Submit Transcript</h2>
      <div className="section">
        <div className="form-group">
          <label>Upload Transcript File (MP4):</label>
          <input
            type="file"
            accept="video/mp4"
            onChange={(e) => {
              const file = e.target.files?.[0]
              if (file) {
                setTranscriptFile(file)
              }
            }}
            style={{ marginBottom: '10px', width: '100%' }}
            id="transcript-file-input"
          />
          {transcriptFile && (
            <div style={{ marginBottom: '15px', padding: '10px', backgroundColor: '#e3f2fd', borderRadius: '4px', border: '1px solid #2196F3' }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: '10px' }}>
                <span>
                  <strong>Selected:</strong> {transcriptFile.name} ({(transcriptFile.size / 1024 / 1024).toFixed(2)} MB)
                </span>
                <button
                  onClick={uploadTranscriptFile}
                  disabled={!currentSession?.session?.id || transcriptFileUploading || loading}
                  style={{ 
                    padding: '6px 12px',
                    backgroundColor: (transcriptFileUploading || loading) ? '#ccc' : '#2196F3',
                    color: 'white',
                    border: 'none',
                    borderRadius: '4px',
                    cursor: (transcriptFileUploading || loading) ? 'not-allowed' : 'pointer'
                  }}
                >
                  {transcriptFileUploading ? 'Transcribing...' : 'Upload and Transcribe'}
                </button>
              </div>
            </div>
          )}
        </div>
        <div className="form-group">
          <label>Or Paste Transcript Text:</label>
          <textarea
            value={transcriptText}
            onChange={(e) => setTranscriptText(e.target.value)}
            placeholder="Paste transcript text here or upload an MP4 file above to transcribe..."
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
