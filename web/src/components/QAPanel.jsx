import { QAHistory } from './QAHistory'

export function QAPanel({
  questions,
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
  const replyingToQuestion = replyingToQuestionId && Array.isArray(questions)
    ? questions.find((q) => q.id === replyingToQuestionId)
    : null

  return (
    <>
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
          readOnly={false}
          currentAskerName={currentAskerName}
          onCitationClick={onCitationClick}
          onReply={setReplyingToQuestionId ? (q) => setReplyingToQuestionId(q.id) : undefined}
        />
      </div>
      <footer className="participant-qa-footer">
        <div style={{ display: 'flex', gap: '8px', alignItems: 'center', marginBottom: '8px' }}>
          <button
            type="button"
            onClick={toggleVoiceRecording}
            disabled={loading || voiceUploading}
            style={{
              padding: '6px 10px',
              fontSize: '13px',
              backgroundColor: voiceRecording ? '#d32f2f' : '#1976D2'
            }}
          >
            {voiceRecording ? 'Stop Mic' : (voiceUploading ? '…' : 'Mic')}
          </button>
          {voiceRecording && (
            <span style={{ fontSize: '12px', color: '#d32f2f' }}>Listening…</span>
          )}
          {voiceUploading && (
            <span style={{ fontSize: '12px', color: '#666', display: 'flex', alignItems: 'center', gap: '4px' }}>
              <span className="spinner" aria-hidden /> Processing…
            </span>
          )}
        </div>
        {voiceFeedback.message && (
          <div className={voiceFeedback.type} style={{ marginBottom: '8px', fontSize: '12px' }}>
            {voiceFeedback.message}
          </div>
        )}
        {replyingToQuestion && (
          <div style={{
            marginBottom: '8px',
            padding: '8px 10px',
            backgroundColor: '#e3f2fd',
            borderRadius: '4px',
            fontSize: '12px',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            gap: '8px'
          }}>
            <span style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
              Replying to: {(replyingToQuestion.question_text || '').slice(0, 60)}
              {(replyingToQuestion.question_text || '').length > 60 ? '…' : ''}
            </span>
            <button
              type="button"
              onClick={() => setReplyingToQuestionId(null)}
              style={{ flexShrink: 0, padding: '2px 8px', fontSize: '12px' }}
            >
              Cancel
            </button>
          </div>
        )}
        {!showVoiceConfirm && (
          <>
            <textarea
              value={questionText}
              onChange={(e) => setQuestionText(e.target.value)}
              placeholder={replyingToQuestionId ? 'Ask a follow-up...' : 'Ask a question...'}
              rows={2}
              style={{
                width: '100%',
                marginBottom: '8px',
                resize: 'vertical',
                minHeight: '44px',
                padding: '8px'
              }}
            />
            <button
              type="button"
              onClick={askSessionQuestion}
              disabled={!questionText?.trim() || loading}
              style={{ width: '100%' }}
            >
              Ask
            </button>
          </>
        )}
        {showVoiceConfirm && (
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
                    <img
                      src="https://static.thenounproject.com/png/1294-200.png"
                      alt=""
                      width={14}
                      height={14}
                      style={{ display: 'block' }}
                      aria-hidden
                    />
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
    </>
  )
}
