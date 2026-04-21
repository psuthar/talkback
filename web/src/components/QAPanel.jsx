import { useState, useEffect } from 'react'
import styles from './QAPanel.module.css'
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
  const [isProcessing, setIsProcessing] = useState(false)

  useEffect(() => {
    if (isProcessing && !loading) {
      setIsProcessing(false)
    }
  }, [loading])

  const handleAsk = () => { setIsProcessing(true); askSessionQuestion() }
  const handleConfirmVoice = () => { setIsProcessing(true); confirmVoiceQuestion() }

  const isThinking = isProcessing

  const MicIcon = () => (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="currentColor" aria-hidden focusable="false">
      <path d="M12 14c1.66 0 3-1.34 3-3V5c0-1.66-1.34-3-3-3S9 3.34 9 5v6c0 1.66 1.34 3 3 3zm5-3c0 2.76-2.24 5-5 5s-5-2.24-5-5H5c0 3.53 2.61 6.43 6 6.92V21h2v-3.08c3.39-.49 6-3.39 6-6.92h-2z" />
    </svg>
  )

  const askBlock = (
    <footer className="participant-qa-footer">
      <div className={styles.askRow}>
        <button
          type="button"
          onClick={toggleVoiceRecording}
          disabled={loading || voiceUploading}
          title={voiceRecording ? 'Stop recording' : 'Record with microphone'}
          aria-label={voiceRecording ? 'Stop recording' : 'Record with microphone'}
          className={styles.micBtn}
          style={{
            backgroundColor: voiceRecording ? 'var(--color-danger-mid)' : 'var(--color-primary)',
            cursor: (loading || voiceUploading) ? 'not-allowed' : 'pointer'
          }}
        >
          {voiceRecording ? (
            <span className={styles.micBtnStop}>Stop</span>
          ) : voiceUploading ? (
            <span className="spinner" style={{ width: 18, height: 18 }} aria-hidden />
          ) : (
            <MicIcon />
          )}
        </button>
        <div className={styles.inputCol}>
          {voiceRecording && (
            <span className={styles.listeningLabel}>Listening…</span>
          )}
          <textarea
            data-testid="question-input"
            value={showVoiceConfirm ? voiceTranscribedText : questionText}
            onChange={(e) => showVoiceConfirm ? setVoiceTranscribedText(e.target.value) : setQuestionText(e.target.value)}
            placeholder={showVoiceConfirm ? undefined : 'Click on the microphone or type here to ask a question'}
            rows={2}
            className={styles.questionTextarea}
          />
          {showVoiceConfirm && !replyingToQuestionId ? (
            <div className={styles.voiceConfirmActions}>
              {polishVoiceQuestion && (
                <button
                  type="button"
                  onClick={() => polishVoiceQuestion(true)}
                  disabled={!voiceTranscribedText?.trim() || loading || voicePolishing}
                  title="AI polish"
                  className={styles.polishBtn}
                  style={{
                    background: voicePolishing && voicePolishMode === 'llm' ? 'var(--color-primary-bg)' : 'rgba(255,255,255,0.9)',
                    cursor: (!voiceTranscribedText?.trim() || loading || voicePolishing) ? 'not-allowed' : 'pointer',
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
              <button type="button" onClick={handleConfirmVoice} disabled={!voiceTranscribedText?.trim() || loading || voicePolishing} className={styles.voiceConfirmSubmit}>
                Confirm & Submit
              </button>
              <button
                type="button"
                onClick={() => cancelVoiceReview?.()}
                disabled={loading}
                className={styles.voiceConfirmCancel}
              >
                Cancel
              </button>
            </div>
          ) : (
            <button
              data-testid="ask-button"
              type="button"
              onClick={handleAsk}
              disabled={!questionText?.trim() || loading}
              className={styles.askBtn}
            >
              Ask
            </button>
          )}
        </div>
      </div>
      {voiceFeedback.message && (
        <div className={`${voiceFeedback.type} ${styles.feedbackMsg}`}>
          {voiceFeedback.message}
        </div>
      )}
      {askQuestionFeedback.message && (
        <div className={`${askQuestionFeedback.type} ${styles.feedbackMsgBottom}`}>
          {askQuestionFeedback.message}
        </div>
      )}
    </footer>
  )

  return (
    <>
      {askBlock}
      <div className="participant-qa-scroll">
        <h3 className={styles.qaHeader}>Q&A</h3>
        {isThinking && (
          <div className={styles.thinkingIndicator}>
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
