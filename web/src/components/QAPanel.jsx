import { useState, useEffect } from 'react'
import styles from './QAPanel.module.css'
import { QAHistory } from './QAHistory'
import { MicIcon } from './icons/MicIcon'
import { PolishButton } from './PolishButton'

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
  polishQuestionText,
  voicePolishing,
  voicePolishMode
}) {
  // Single defined source for polish: voice confirm uses the transcribed buffer,
  // otherwise the typed question text. Same invariant as App.polishQuestionText.
  const polishHandler = polishQuestionText || polishVoiceQuestion
  const polishSourceText = showVoiceConfirm ? voiceTranscribedText : questionText
  const canPolish = !!polishHandler && !!polishSourceText?.trim() && !loading && !voicePolishing
  const [isProcessing, setIsProcessing] = useState(false)

  useEffect(() => {
    if (isProcessing && !loading) {
      setIsProcessing(false)
    }
  }, [loading])

  const handleAsk = () => { setIsProcessing(true); askSessionQuestion() }
  const handleConfirmVoice = () => { setIsProcessing(true); confirmVoiceQuestion() }

  const isThinking = isProcessing

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
              {polishHandler && (
                <PolishButton
                  onPolish={polishHandler}
                  disabled={!canPolish}
                  busy={voicePolishing && voicePolishMode === 'llm'}
                  className={styles.polishBtn}
                  data-testid="polish-question-btn"
                />
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
            <div className={styles.askActions}>
              <button
                data-testid="ask-button"
                type="button"
                onClick={handleAsk}
                disabled={!questionText?.trim() || loading}
                className={styles.askBtn}
              >
                Ask
              </button>
            </div>
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
          polishQuestionText={polishQuestionText}
          voicePolishing={voicePolishing}
          voicePolishMode={voicePolishMode}
          askQuestionFeedback={askQuestionFeedback}
        />
      </div>
    </>
  )
}
