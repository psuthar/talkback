import { useEffect } from 'react'
import { VideoPlayer, PlayerEvent } from '../VideoPlayer'
import { TranscriptViewer } from '../components/TranscriptViewer'
import { MaterialsList } from '../components/MaterialsList'
import { QAHistory } from '../components/QAHistory'
import { ArtifactsList } from '../components/ArtifactsList'

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
  // Question input props
  questionText,
  setQuestionText,
  askSessionQuestion,
  askQuestionFeedback,
  currentAnswer,
  // Voice recording props
  voiceRecording,
  voiceUploading,
  toggleVoiceRecording,
  voiceFeedback,
  showVoiceConfirm,
  voiceTranscribedText,
  setVoiceTranscribedText,
  confirmVoiceQuestion
}) {
  // Handle case where currentSession might be null (e.g., API error)
  // Still render the UI so users can see the structure, but show error message
  const hasSession = currentSession && currentSession.session

  const video = selectedVideo || (currentSession?.video_sources && currentSession.video_sources[0])
  const embedUrl = video ? getVideoEmbedUrl(video) : null

  // Fetch questions on mount/change (WebSocket handles real-time updates)
  useEffect(() => {
    if (!hasSession) return

    // Fetch immediately on mount/change
    fetchSessionQuestions(currentSession.session.id)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hasSession])

  return (
    <>
      {/* Error message if session failed to load */}
      {!hasSession && (
        <div className="section">
          <div className="error" style={{ marginBottom: '20px' }}>
            Unable to load session. Please check the API connection and try again.
          </div>
        </div>
      )}

      {/* Session Header for Participant Mode */}
      {hasSession && (
        <div style={{ marginBottom: '20px', padding: '15px', backgroundColor: '#e8f5e9', borderRadius: '5px', border: '2px solid #4CAF50' }}>
          <div style={{ flex: 1 }}>
            <h2 style={{ margin: '0 0 10px 0', color: '#2e7d32' }}>Session: {currentSession.session.title}</h2>
            <div style={{ fontSize: '14px', color: '#666' }}>
              {currentSession.artifacts && currentSession.artifacts.length > 0 ? (
                <>
                  <strong>Artifacts:</strong> {currentSession.artifacts.map(a => a.title).join(', ')}
                  <br />
                </>
              ) : (
                <div style={{ color: '#999', fontStyle: 'italic' }}>No artifacts in this session yet</div>
              )}
              {currentSession.session.created_by && (
                <>
                  <strong>Created by:</strong> {currentSession.session.created_by}
                  <br />
                </>
              )}
              <strong>Created:</strong> {new Date(currentSession.session.created_at).toLocaleString()}
            </div>
          </div>
        </div>
      )}

      {/* Mode Indicator */}
      <div style={{ 
        marginBottom: '20px', 
        padding: '12px 20px', 
        backgroundColor: '#e8f5e9', 
        borderRadius: '5px', 
        border: '2px solid #4CAF50',
        display: 'flex',
        alignItems: 'center',
        gap: '10px'
      }}>
        <span style={{ 
          backgroundColor: '#4CAF50', 
          color: 'white', 
          padding: '4px 12px', 
          borderRadius: '4px',
          fontWeight: 'bold',
          fontSize: '14px'
        }}>
          PARTICIPANT MODE
        </span>
        <span style={{ color: '#2e7d32', fontSize: '14px' }}>
          You can view content and ask questions
        </span>
      </div>

      {/* Artifacts - Read-only view */}
      {hasSession && currentSession.artifacts && currentSession.artifacts.length > 0 && (
        <ArtifactsList artifacts={currentSession.artifacts} readOnly={true} />
      )}

      {/* Materials */}
      {hasSession && currentSession.materials && currentSession.materials.length > 0 && (
        <MaterialsList materials={currentSession.materials} />
      )}

      {/* Video Player - Always show section, even if no videos */}
      <div className="section" style={{ backgroundColor: '#fff3e0', border: '1px solid #ff9800' }}>
        <h2>Video Player</h2>
        
        {hasSession && currentSession.video_sources && currentSession.video_sources.length > 0 ? (
          <>
            {currentSession.video_sources.length > 1 && (
              <div style={{ marginBottom: '15px' }}>
                <label style={{ fontWeight: 'bold', marginRight: '10px' }}>Select Video:</label>
                <select 
                  value={selectedVideo?.id || currentSession.video_sources[0].id}
                  onChange={(e) => {
                    const video = currentSession.video_sources.find(v => v.id === e.target.value)
                    setSelectedVideo(video)
                    setVideoId(video.id)
                    setVideoPlayerKey(prev => prev + 1)
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

            {video && (
              <div>
                <div style={{ marginBottom: '10px' }}>
                  <strong>Provider:</strong> <span style={{ textTransform: 'capitalize' }}>{video.provider}</span>
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
          </>
        ) : (
          <div style={{ padding: '15px', color: '#666', fontStyle: 'italic' }}>
            No videos available in this session yet.
          </div>
        )}
      </div>

      {/* Ask Question Section */}
      <div className="section">
        <h2>Ask Question</h2>
        <div className="form-group">
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
          <textarea
            value={questionText}
            onChange={(e) => setQuestionText(e.target.value)}
            placeholder="Ask a question about this session..."
            rows={4}
            style={{ width: '100%', marginBottom: '10px' }}
          />
          <button onClick={askSessionQuestion} disabled={!questionText || loading}>
            Ask Question
          </button>
          {showVoiceConfirm && (
            <div style={{ marginTop: '15px', padding: '12px', border: '1px solid #ddd', borderRadius: '6px', backgroundColor: '#f9f9f9' }}>
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
                  Confirm & Submit
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
          {askQuestionFeedback.message && (
            <div className={askQuestionFeedback.type} style={{ marginTop: '10px' }}>
              {askQuestionFeedback.message}
            </div>
          )}
        </div>

        {currentAnswer && currentAnswer.answer && (
          <div style={{ marginTop: '20px', padding: '15px', backgroundColor: '#f0f0f0', borderRadius: '5px' }}>
            <h3>Answer:</h3>
            <div><strong>Status:</strong> {currentAnswer.answer.answer_status}</div>
            <div><strong>Confidence:</strong> {currentAnswer.answer.confidence}</div>
            {currentAnswer.answer.confirmed && (
              <div style={{ marginTop: '5px' }}>
                <strong style={{ color: '#4CAF50' }}>✓ Confirmed by Creator</strong>
              </div>
            )}
            <div style={{ marginTop: '10px' }}>{currentAnswer.answer.answer_text}</div>
            {currentAnswer.answer.citations && currentAnswer.answer.citations.length > 0 && (
              <div className="citations" style={{ marginTop: '15px' }}>
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
        )}
      </div>

      {/* Question History */}
      <div className="section">
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '10px' }}>
          <h2 style={{ margin: 0 }}>Question History</h2>
          {currentSession?.session?.id && (
            <div style={{ display: 'flex', gap: '10px', alignItems: 'center' }}>
              <span style={{ fontSize: '12px', color: '#666', fontStyle: 'italic' }}>
                Real-time updates via WebSocket
              </span>
              <button onClick={() => fetchSessionQuestions(currentSession.session.id)} disabled={loading}>
                Refresh Now
              </button>
            </div>
          )}
        </div>
        {questions.length > 0 && (
          <div style={{ marginBottom: '10px', fontSize: '14px', color: '#666' }}>
            {questions.length} question{questions.length !== 1 ? 's' : ''} in this session
          </div>
        )}
        <QAHistory questions={questions || []} readOnly={false} />
      </div>
    </>
  )
}
