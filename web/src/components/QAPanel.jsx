import { QAHistory } from './QAHistory'

export function QAPanel({
  questions,
  fetchSessionQuestions,
  sessionId,
  loading,
  questionText,
  setQuestionText,
  askSessionQuestion,
  askQuestionFeedback,
  currentAnswer,
  onCitationClick,
  voiceRecording,
  voiceUploading,
  toggleVoiceRecording,
  voiceFeedback,
  showVoiceConfirm,
  voiceTranscribedText,
  setVoiceTranscribedText,
  confirmVoiceQuestion
}) {
  const isThinking = loading && questionText && (!currentAnswer || !currentAnswer.answer)

  return (
    <>
      <div className="participant-qa-scroll">
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '8px' }}>
          <h3 style={{ margin: 0, fontSize: '14px', color: '#555' }}>Q&A</h3>
          {sessionId && (
            <button
              type="button"
              onClick={() => fetchSessionQuestions(sessionId)}
              disabled={loading}
              style={{ padding: '2px 8px', fontSize: '12px' }}
            >
              Refresh
            </button>
          )}
        </div>
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
        <QAHistory questions={questions} readOnly={false} onCitationClick={onCitationClick} />
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
        <textarea
          value={questionText}
          onChange={(e) => setQuestionText(e.target.value)}
          placeholder="Ask a question..."
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
        {showVoiceConfirm && (
          <div style={{
            marginTop: '10px',
            padding: '10px',
            border: '1px solid #ddd',
            borderRadius: '4px',
            backgroundColor: '#f9f9f9'
          }}>
            <div style={{ fontWeight: 600, marginBottom: '6px', fontSize: '13px' }}>Review transcription</div>
            <textarea
              value={voiceTranscribedText}
              onChange={(e) => setVoiceTranscribedText(e.target.value)}
              rows={2}
              style={{ width: '100%', marginBottom: '8px', padding: '6px' }}
            />
            <div style={{ display: 'flex', gap: '8px' }}>
              <button type="button" onClick={confirmVoiceQuestion} disabled={!voiceTranscribedText?.trim() || loading} style={{ marginTop: 0 }}>
                Confirm & Submit
              </button>
              <button
                type="button"
                onClick={() => { setShowVoiceConfirm?.(false); setVoiceTranscribedText(''); }}
                disabled={loading}
                style={{ marginTop: 0, backgroundColor: '#757575' }}
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
