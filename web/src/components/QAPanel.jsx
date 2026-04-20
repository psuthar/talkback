import { QAHistory } from './QAHistory'

export function QAPanel({
  questions,
  unreadQuestionIds = [],
  markQuestionViewed,
  sessionId,
  loading,
  questionText,
  setQuestionText,
  askSessionQuestion,
  askQuestionFeedback,
  currentAnswer,
  onCitationClick,
  replyingToQuestionId,
  setReplyingToQuestionId,
  currentAskerName,
  creatorDisplayName,
  voiceRecording,
  voiceUploading,
  toggleVoiceRecording,
  voiceFeedback,
  showVoiceConfirm,
  voiceTranscribedText,
  setVoiceTranscribedText,
  confirmVoiceQuestion,
  cancelVoiceReview,
  polishVoiceQuestion,
  voicePolishing,
  voicePolishMode
}) {
  const isThinking = loading && questionText && (!currentAnswer || !currentAnswer.answer)

  const MicIcon = () => (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="currentColor" aria-hidden focusable="false">
      <path d="M12 14c1.66 0 3-1.34 3-3V5c0-1.66-1.34-3-3-3S9 3.34 9 5v6c0 1.66 1.34 3 3 3zm5-3c0 2.76-2.24 5-5 5s-5-2.24-5-5H5c0 3.53 2.61 6.43 6 6.92V21h2v-3.08c3.39-.49 6-3.39 6-6.92h-2z" />
    </svg>
  )

  const askBlock = (
    <footer className="participant-qa-footer">
      <div style={{ display: 'flex', gap: '8px', alignItems: 'flex-start', marginBottom: '8px' }}>
        <button
          type="button"
          onClick={toggleVoiceRecording}
          disabled={loading || voiceUploading}
          title={voiceRecording ? 'Stop recording' : 'Record with microphone'}
          aria-label={voiceRecording ? 'Stop recording' : 'Record with microphone'}
          style={{
            flexShrink: 0,
            padding: '8px',
            display: 'inline-flex',
            alignItems: 'center',
            justifyContent: 'center',
            backgroundColor: voiceRecording ? '#d32f2f' : '#1976D2',
            color: '#fff',
            border: 'none',
            borderRadius: '4px',
            cursor: (loading || voiceUploading) ? 'not-allowed' : 'pointer'
          }}
        >
          {voiceRecording ? (
            <span style={{ fontSize: '12px', fontWeight: 600 }}>Stop</span>
          ) : voiceUploading ? (
            <span className="spinner" style={{ width: 18, height: 18 }} aria-hidden />
          ) : (
            <MicIcon />
          )}
        </button>
        <div style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column', gap: '6px' }}>
          {voiceRecording && (
            <span style={{ fontSize: '12px', color: '#d32f2f' }}>Listening…</span>
          )}
          {!showVoiceConfirm && (
            <>
              <textarea
                data-testid="question-input"
                value={questionText}
                onChange={(e) => setQuestionText(e.target.value)}
                placeholder="Click on the microphone or type here to ask a question"
                rows={2}
                style={{
                  width: '100%',
                  resize: 'vertical',
                  minHeight: '44px',
                  padding: '8px',
                  boxSizing: 'border-box'
                }}
              />
              <button
                data-testid="ask-button"
                type="button"
                onClick={askSessionQuestion}
                disabled={!questionText?.trim() || loading}
                style={{ width: '100%' }}
              >
                Ask
              </button>
            </>
          )}
        </div>
      </div>
      {voiceFeedback.message && (
        <div className={voiceFeedback.type} style={{ marginBottom: '8px', fontSize: '12px' }}>
          {voiceFeedback.message}
        </div>
      )}
      {showVoiceConfirm && !replyingToQuestionId && (
          <div style={{
            marginTop: '10px',
            padding: '10px',
            border: '1px solid #ddd',
            borderRadius: '4px',
            backgroundColor: '#f9f9f9'
          }}>
            <div style={{ position: 'relative', marginBottom: '8px' }}>
              <textarea
                value={voiceTranscribedText}
                onChange={(e) => setVoiceTranscribedText(e.target.value)}
                rows={2}
                style={{ width: '100%', paddingRight: '26px', boxSizing: 'border-box', padding: '6px' }}
              />
              {polishVoiceQuestion && (
                <button
                  type="button"
                  onClick={() => polishVoiceQuestion(true)}
                  disabled={!voiceTranscribedText?.trim() || loading || voicePolishing}
                  title="AI polish"
                  style={{
                    position: 'absolute',
                    top: '6px',
                    right: '6px',
                    margin: 0,
                    padding: '3px',
                    border: '1px solid #e0e0e0',
                    borderRadius: '4px',
                    background: voicePolishing && voicePolishMode === 'llm' ? '#e3f2fd' : 'rgba(255,255,255,0.9)',
                    cursor: (!voiceTranscribedText?.trim() || loading || voicePolishing) ? 'not-allowed' : 'pointer',
                    display: 'inline-flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    boxShadow: '0 1px 2px rgba(0,0,0,0.06)'
                  }}
                >
                  {voicePolishing && voicePolishMode === 'llm' ? (
                    <span className="spinner" style={{ width: 12, height: 12 }} aria-hidden />
                  ) : (
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" focusable="false" style={{ display: 'block' }}>
                      <path d="M12 1l2.753 8.247L23 12l-8.247 2.753L12 23l-2.753-8.247L1 12l8.247-2.753z" />
                    </svg>
                  )}
                </button>
              )}
            </div>
            <div style={{ display: 'flex', gap: '8px', flexWrap: 'wrap', alignItems: 'center' }}>
              <button type="button" onClick={confirmVoiceQuestion} disabled={!voiceTranscribedText?.trim() || loading || voicePolishing} style={{ marginTop: 0 }}>
                Confirm & Submit
              </button>
              <button
                type="button"
                onClick={() => cancelVoiceReview?.()}
                disabled={loading}
                style={{ marginTop: 0, backgroundColor: '#fff', color: '#333', border: '1px solid #666' }}
              >
                Cancel
              </button>
            </div>
          </div>
        )}
      {askQuestionFeedback.message && (
        <div className={askQuestionFeedback.type} style={{ marginTop: '8px', fontSize: '12px' }}>
          {askQuestionFeedback.message}
        </div>
      )}
    </footer>
  )

  return (
    <>
      {askBlock}
      <div className="participant-qa-scroll">
        <h3 style={{ margin: 0, marginBottom: '8px', fontSize: '14px', color: '#555' }}>Q&A</h3>
        {isThinking && (
          <div style={{
            padding: '12px',
            marginBottom: '12px',
            backgroundColor: '#fff3e0',
            borderRadius: '4px',
            fontSize: '13px',
            display: 'flex',
            alignItems: 'center',
            gap: '8px'
          }}>
            <span className="spinner" aria-hidden />
            <span>Thinking…</span>
          </div>
        )}
        <QAHistory
          questions={questions}
          unreadQuestionIds={unreadQuestionIds}
          markQuestionViewed={markQuestionViewed}
          sessionId={sessionId}
          readOnly={false}
          currentAskerName={currentAskerName}
          onCitationClick={onCitationClick}
          onReply={setReplyingToQuestionId ? (q) => setReplyingToQuestionId(q.id) : undefined}
          creatorDisplayName={creatorDisplayName}
          replyingToQuestionId={replyingToQuestionId}
          setReplyingToQuestionId={setReplyingToQuestionId}
          questionText={questionText}
          setQuestionText={setQuestionText}
          askSessionQuestion={askSessionQuestion}
          loading={loading}
          toggleVoiceRecording={toggleVoiceRecording}
          voiceRecording={voiceRecording}
          voiceUploading={voiceUploading}
          voiceFeedback={voiceFeedback}
          showVoiceConfirm={showVoiceConfirm}
          voiceTranscribedText={voiceTranscribedText}
          setVoiceTranscribedText={setVoiceTranscribedText}
          confirmVoiceQuestion={confirmVoiceQuestion}
          cancelVoiceReview={cancelVoiceReview}
          polishVoiceQuestion={polishVoiceQuestion}
          voicePolishing={voicePolishing}
          voicePolishMode={voicePolishMode}
          askQuestionFeedback={askQuestionFeedback}
        />
      </div>
    </>
  )
}
