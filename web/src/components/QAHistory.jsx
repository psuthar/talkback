import { useState, useEffect, useRef } from 'react'
import styles from './QAHistory.module.css'
import { MicIcon } from './icons/MicIcon'
import { TrashIcon } from './icons/TrashIcon'
import { PolishButton } from './PolishButton'

const ANSWER_STATUS_LABELS = {
  answered: 'Answered',
  not_covered: 'Not covered by session',
  error: 'Unable to answer',
}

const SOURCE_TYPE_LABELS = {
  transcript: 'Transcript',
  material: 'Document',
  link: 'Link',
}

function CitationBadge({ citation, onClick }) {
  const label = citation.label || SOURCE_TYPE_LABELS[citation.source_type] || citation.source_type || 'Source'
  const canNavigate = citation.anchor?.start_ms != null ||
    citation?.source_type === 'material' ||
    citation?.source_type === 'link' ||
    (citation.navigation && (citation.navigation.type === 'video' || citation.navigation.type === 'pdf' || citation.navigation.type === 'doc' || citation.navigation.type === 'url'))
  return (
    <button
      type="button"
      data-testid="citation-badge"
      onClick={onClick}
      title={citation.excerpt || citation.snippet || label}
      className={styles.citationBtn}
      style={{
        backgroundColor: canNavigate ? 'var(--color-primary-bg)' : '#f5f5f5',
        color: canNavigate ? 'var(--color-primary)' : '#555',
        border: `1px solid ${canNavigate ? '#90caf9' : '#e0e0e0'}`,
      }}
    >
      {citation.citation_id ? <span>[{citation.citation_id}]</span> : null}
      {' '}
      <span className={styles.citationLabel}>{label}</span>
    </button>
  )
}

// Find the root question ID for any question (itself if root, or walk up via parent_question_id).
function findRootId(questions, questionId) {
  const byId = Object.fromEntries((questions || []).map((q) => [q.id, q]))
  let q = byId[questionId]
  while (q?.parent_question_id) q = byId[q.parent_question_id]
  return q?.id ?? questionId
}

// Build thread tree: roots have no parent_question_id; replies grouped under parent.
// Top-level questions: newest first (descending by created_at). Nested replies: chronological (ascending).
function buildThreadTree(questions) {
  if (!questions || questions.length === 0) return { roots: [], byParent: {} }
  const byParent = {}
  for (const q of questions) {
    const pid = q.parent_question_id ?? 'root'
    if (!byParent[pid]) byParent[pid] = []
    byParent[pid].push(q)
  }
  const roots = (byParent.root || []).sort((a, b) => new Date(b.created_at) - new Date(a.created_at))
  for (const k of Object.keys(byParent)) {
    if (k !== 'root') byParent[k].sort((a, b) => new Date(a.created_at) - new Date(b.created_at))
  }
  return { roots, byParent }
}

const SYSTEM_GENERATED_LABEL = 'Answered by TalkBack AI'

// Inline reply form shown under a Q&A card when user clicks Reply (textarea + mic + Ask + Cancel).
function InlineReplyForm({
  questionText,
  setQuestionText,
  askSessionQuestion,
  loading,
  onCancel,
  toggleVoiceRecording,
  voiceRecording,
  voiceUploading,
  voiceFeedback,
  showVoiceConfirm,
  voiceTranscribedText,
  setVoiceTranscribedText,
  confirmVoiceQuestion,
  cancelVoiceReview,
  polishVoiceQuestion,
  polishQuestionText,
  voicePolishing,
  voicePolishMode,
  askQuestionFeedback
}) {
  const textareaRef = useRef(null)
  useEffect(() => {
    textareaRef.current?.focus()
  }, [])

  // Same single-source invariant as the main ask flow: voice confirm uses the
  // transcribed buffer, the typed reply uses questionText.
  const polishHandler = polishQuestionText || polishVoiceQuestion
  const polishSourceText = showVoiceConfirm ? voiceTranscribedText : questionText
  const canPolish = !!polishHandler && !!polishSourceText?.trim() && !loading && !voicePolishing

  return (
    <div className={styles.replyForm}>
      <div className={styles.replyFormHeader}>
        Ask a follow-up…
      </div>
      <div className={styles.replyFormRow}>
        <button
          type="button"
          onClick={toggleVoiceRecording}
          disabled={loading || voiceUploading}
          title={voiceRecording ? 'Stop recording' : 'Record with microphone'}
          aria-label={voiceRecording ? 'Stop recording' : 'Record with microphone'}
          className={styles.replyFormMicBtn}
          style={{
            backgroundColor: voiceRecording ? 'var(--color-danger-mid)' : 'var(--color-primary)',
            cursor: (loading || voiceUploading) ? 'not-allowed' : 'pointer',
          }}
        >
          {voiceRecording ? (
            <span className={styles.replyFormMicStop}>Stop</span>
          ) : voiceUploading ? (
            <span className="spinner" style={{ width: 18, height: 18 }} aria-hidden />
          ) : (
            <MicIcon />
          )}
        </button>
        <div className={styles.replyFormInputCol}>
          {voiceRecording && (
            <span className={styles.replyFormListening}>Listening…</span>
          )}
          {!showVoiceConfirm && (
            <>
              <textarea
                ref={textareaRef}
                value={questionText}
                onChange={(e) => setQuestionText(e.target.value)}
                placeholder="Ask a follow-up..."
                rows={2}
                className={styles.replyFormTextarea}
              />
              <div className={styles.replyFormActions}>
                <button
                  type="button"
                  onClick={askSessionQuestion}
                  disabled={!questionText?.trim() || loading}
                  className={styles.replyFormAskBtn}
                >
                  Ask
                </button>
                <button
                  type="button"
                  onClick={onCancel}
                  disabled={loading}
                  className={styles.replyFormCancelBtn}
                >
                  Cancel
                </button>
              </div>
            </>
          )}
          {showVoiceConfirm && (
            <div className={styles.replyFormVoiceConfirm}>
              <textarea
                value={voiceTranscribedText}
                onChange={(e) => setVoiceTranscribedText(e.target.value)}
                rows={2}
                className={styles.replyFormVoiceTextarea}
              />
              <div className={styles.replyFormVoiceActions}>
                {polishHandler && (
                  <PolishButton
                    onPolish={polishHandler}
                    disabled={!canPolish}
                    busy={voicePolishing && voicePolishMode === 'llm'}
                    className={styles.replyFormPolishBtn}
                    shortLabel="Polish"
                    data-testid="polish-reply-btn"
                  />
                )}
                <button type="button" onClick={confirmVoiceQuestion} disabled={!voiceTranscribedText?.trim() || loading || voicePolishing} className={styles.replyFormConfirmBtn}>
                  Confirm & Submit
                </button>
                <button type="button" onClick={() => cancelVoiceReview?.()} disabled={loading} className={styles.replyFormVoiceCancelBtn}>
                  Cancel
                </button>
              </div>
            </div>
          )}
        </div>
      </div>
      {voiceFeedback?.message && (
        <div className={`${voiceFeedback.type} ${styles.replyFormFeedback}`}>{voiceFeedback.message}</div>
      )}
      {askQuestionFeedback?.message && (
        <div className={`${askQuestionFeedback.type} ${styles.replyFormFeedback}`}>{askQuestionFeedback.message}</div>
      )}
    </div>
  )
}

function QACard({ q, isUnread = false, onCitationClick, onReply, depth = 0, collapsed = false, onToggle, creatorDisplayName, replyingToQuestionId, inlineReplyProps, replyCount = 0, canDeleteQuestions = false, onRequestDelete }) {
  const answerFromLabel = (() => {
    if (!q.answer) return null
    if (q.answer.model && q.answer.model !== 'manual') {
      return SYSTEM_GENERATED_LABEL
    }
    return q.answer.answered_by_display_name || q.answer.answered_by || creatorDisplayName || null
  })()
  const confirmedByLabel = creatorDisplayName || null
  const isReply = depth > 0
  const replyIndentPx = 28
  // SCRUM-367: peer tombstone — render placeholder instead of full card.
  if (q._tombstone) {
    return (
      <div
        data-testid="question-tombstone"
        style={{
          marginBottom: '15px',
          marginLeft: isReply ? replyIndentPx : 0,
          padding: '12px 15px',
          border: '1px dashed #bbb',
          borderRadius: '5px',
          color: '#777',
          fontStyle: 'italic',
          fontSize: '13px',
          backgroundColor: '#fafafa',
          opacity: 0.7,
          transition: 'opacity 0.6s ease',
        }}
      >
        This question was deleted by the creator
      </div>
    )
  }
  return (
    <div
      data-testid="question-item"
      className="question-item"
      style={{
        marginBottom: '15px',
        marginLeft: isReply ? replyIndentPx : 0,
        padding: isReply ? '10px 12px' : '15px',
        border: '1px solid #ddd',
        borderRadius: '5px',
        backgroundColor: q.answer ? '#f9f9f9' : '#fff',
        borderLeft: isReply ? '3px solid #90caf9' : undefined
      }}
    >
      <div className={styles.qaCardHeader}>
        {onToggle && (
          <button
            type="button"
            onClick={onToggle}
            aria-label={collapsed ? 'Expand' : 'Collapse'}
            className={styles.qaCardToggleBtn}
          >
            {collapsed ? '▶' : '▼'}
          </button>
        )}
        <div className={styles.qaCardBody}>
          {collapsed ? (
            <div className={styles.qaCardCollapsedRow}>
              <span className={styles.qaCardQuestionText}>Q: {q.question_text}</span>
              {replyCount > 0 && (
                <span className={styles.qaCardReplyCount}>({replyCount} {replyCount === 1 ? 'reply' : 'replies'})</span>
              )}
              {isUnread && (
                <span className={styles.qaCardNewBadge}>New</span>
              )}
              {/* SCRUM-370: trash icon also visible while collapsed for root cards. */}
              {!isReply && canDeleteQuestions && onRequestDelete && (
                <button
                  type="button"
                  data-testid={`delete-question-trigger-${q.id}`}
                  aria-label="Delete question"
                  onClick={(e) => {
                    e.stopPropagation()
                    onRequestDelete(q.id)
                  }}
                  style={{
                    flexShrink: 0,
                    marginLeft: 'auto',
                    padding: '4px 8px',
                    background: 'none',
                    border: 'none',
                    cursor: 'pointer',
                    color: '#666',
                    lineHeight: 0,
                    display: 'inline-flex',
                    alignItems: 'center',
                  }}
                >
                  <TrashIcon />
                </button>
              )}
            </div>
          ) : (
            <>
              <div className={styles.qaCardExpandedRow}>
                <div className={styles.qaCardQuestionInner}>
                  <div className={styles.qaCardQuestionTitle}>
                    Q: {q.question_text}
                    {isUnread && (
                      <span className={styles.qaCardNewBadge}>New</span>
                    )}
                  </div>
                  <div className={styles.qaCardMeta}>
                    Asked: {new Date(q.created_at).toLocaleString()}
                    {' · asked by '}
                    {q.asked_by ? <strong>{q.asked_by}</strong> : <span className={styles.qaCardAnonLabel}>—</span>}
                    {q.video_time_seconds !== null && q.video_time_seconds !== undefined && (
                      <span className={styles.qaCardVideoTime}>
                        | At {Math.floor(q.video_time_seconds / 60)}:{(q.video_time_seconds % 60).toString().padStart(2, '0')}
                      </span>
                    )}
                  </div>
                </div>
                {onReply && (
                  <button
                    type="button"
                    onClick={() => onReply(q)}
                    className={styles.qaCardReplyBtn}
                  >
                    Reply
                  </button>
                )}
                {/* SCRUM-370: trash icon — root question only, creator/admin only. */}
                {!isReply && canDeleteQuestions && onRequestDelete && (
                  <button
                    type="button"
                    data-testid={`delete-question-trigger-${q.id}`}
                    aria-label="Delete question"
                    onClick={(e) => {
                      e.stopPropagation()
                      onRequestDelete(q.id)
                    }}
                    style={{
                      flexShrink: 0,
                      marginLeft: '6px',
                      padding: '4px 8px',
                      background: 'none',
                      border: 'none',
                      cursor: 'pointer',
                      color: '#666',
                      lineHeight: 0,
                      display: 'inline-flex',
                      alignItems: 'center',
                    }}
                  >
                    <TrashIcon />
                  </button>
                )}
              </div>
      {q.answer ? (
        <div className={styles.qaCardAnswerBlock}>
          <div data-testid="answer-text" className={`answer-text ${styles.qaCardAnswerText}`}><strong>A:</strong> {q.answer.answer_text}</div>
          <div className={styles.qaCardAnswerMeta}>
            From: {answerFromLabel ? <strong>{answerFromLabel}</strong> : <strong>—</strong>}
            {' | '}
            Status: <span
              data-testid="answer-status"
              className={`answer-status ${q.answer.answer_status}`}
              style={{
                color: q.answer.answer_status === 'answered' ? 'var(--color-success-mid)' :
                  q.answer.answer_status === 'not_covered' ? '#ff9800' : 'var(--color-danger)',
                fontWeight: 'bold'
              }}
            >{ANSWER_STATUS_LABELS[q.answer.answer_status] ?? 'Unknown'}</span>
            {q.answer.model !== 'manual' && (
              <>
                {' | '}
                Confidence: {q.answer.confidence != null ? (q.answer.confidence * 100).toFixed(0) + '%' : 'N/A'}
                {q.answer.confirmed && confirmedByLabel && (
                  <span className={styles.qaCardConfirmed}>
                    ✓ Confirmed by {confirmedByLabel}
                  </span>
                )}
              </>
            )}
          </div>
          {q.answer.citations && q.answer.citations.length > 0 && (
            <div data-testid="citations" className={`citations ${styles.qaCardCitations}`}>
              <strong>Citations:</strong>
              <div className={styles.citationList}>
                {q.answer.citations.map((citation, cidx) => (
                  <CitationBadge
                    key={cidx}
                    citation={citation}
                    onClick={() => onCitationClick?.(citation)}
                  />
                ))}
              </div>
            </div>
          )}
        </div>
      ) : (
        <div style={{ marginTop: '10px', padding: '10px', backgroundColor: q._pending ? 'var(--color-primary-bg)' : '#f5f5f5', borderRadius: '3px', fontSize: '13px', display: 'flex', alignItems: 'center', gap: '8px' }}>
          <span className={`spinner ${styles.qaCardPendingSpinner}`} aria-hidden />
          <span>{q._pending ? 'Getting an answer…' : 'Processing…'}</span>
        </div>
      )}
      {replyingToQuestionId === q.id && inlineReplyProps && (
        <InlineReplyForm {...inlineReplyProps} />
      )}
            </>
          )}
        </div>
      </div>
    </div>
  )
}

function countReplies(byParent, rootId) {
  let n = 0
  const direct = byParent[rootId] || []
  n += direct.length
  for (const q of direct) n += countReplies(byParent, q.id)
  return n
}

function ThreadList({ roots, byParent, unreadQuestionIds = [], onCitationClick, onReply, depth = 0, expandedCards = {}, onToggleCard, creatorDisplayName, replyingToQuestionId, inlineReplyProps, canDeleteQuestions = false, onRequestDelete }) {
  const isRootLevel = depth === 0
  return (
    <>
      {roots.map((q) => {
        const hasReplies = byParent[q.id] && byParent[q.id].length > 0
        const isExpanded = expandedCards[q.id] === true
        const showReplies = isExpanded && hasReplies
        const replyCount = hasReplies ? countReplies(byParent, q.id) : 0
        const isUnread = unreadQuestionIds && unreadQuestionIds.includes(String(q.id))

        return (
          <div key={q.id} className={isRootLevel ? styles.threadRootItem : undefined}>
            <QACard
              q={q}
              isUnread={isUnread}
              onCitationClick={onCitationClick}
              onReply={onReply}
              depth={depth}
              collapsed={!isExpanded}
              onToggle={onToggleCard ? () => onToggleCard(q.id) : undefined}
              creatorDisplayName={creatorDisplayName}
              replyingToQuestionId={replyingToQuestionId}
              inlineReplyProps={inlineReplyProps}
              replyCount={isRootLevel ? replyCount : 0}
              canDeleteQuestions={canDeleteQuestions}
              onRequestDelete={onRequestDelete}
            />
            {showReplies && hasReplies && (
              <div className={styles.threadRepliesIndent}>
                <ThreadList
                  roots={byParent[q.id]}
                  byParent={byParent}
                  unreadQuestionIds={unreadQuestionIds}
                  onCitationClick={onCitationClick}
                  onReply={onReply}
                  depth={depth + 1}
                  expandedCards={expandedCards}
                  onToggleCard={onToggleCard}
                  creatorDisplayName={creatorDisplayName}
                  replyingToQuestionId={replyingToQuestionId}
                  inlineReplyProps={inlineReplyProps}
                  canDeleteQuestions={canDeleteQuestions}
                  onRequestDelete={onRequestDelete}
                />
              </div>
            )}
          </div>
        )
      })}
    </>
  )
}

export function QAHistory({
  questions,
  unreadQuestionIds = [],
  markQuestionViewed,
  sessionId,
  readOnly = false,
  currentAskerName,
  onCitationClick,
  onReply,
  creatorDisplayName,
  replyingToQuestionId,
  setReplyingToQuestionId,
  questionText,
  setQuestionText,
  askSessionQuestion,
  loading,
  toggleVoiceRecording,
  voiceRecording,
  voiceUploading,
  voiceFeedback,
  showVoiceConfirm,
  voiceTranscribedText,
  setVoiceTranscribedText,
  confirmVoiceQuestion,
  cancelVoiceReview,
  polishVoiceQuestion,
  polishQuestionText,
  voicePolishing,
  voicePolishMode,
  askQuestionFeedback,
  canDeleteQuestions = false,
  requestDeleteQuestion
}) {
  // Per-question collapse/expand. Answered cards default to expanded on first load.
  const [expandedCards, setExpandedCards] = useState({})
  // Track which question IDs we've already auto-expanded so refetches don't re-expand (participant mode).
  const autoExpandedIdsRef = useRef(new Set())
  const initialExpandDoneRef = useRef(false)

  // On initial load, expand all cards that already have answers.
  useEffect(() => {
    if (initialExpandDoneRef.current || !questions?.length) return
    initialExpandDoneRef.current = true
    const initial = {}
    for (const q of questions) {
      if (q.answer) initial[q.id] = true
    }
    if (Object.keys(initial).length > 0) setExpandedCards(initial)
  }, [questions])

  // When user clicks Reply, expand that card and its root so the inline form is visible.
  useEffect(() => {
    if (!replyingToQuestionId || !questions?.length) return
    const rootId = findRootId(questions, replyingToQuestionId)
    setExpandedCards((prev) => ({ ...prev, [rootId]: true, [replyingToQuestionId]: true }))
  }, [replyingToQuestionId, questions])

  // Auto-expand only when a *new* answer appears for a question the current user asked (once per question).
  // Do not re-expand on every refetch so participant view doesn't auto-expand after a few seconds.
  useEffect(() => {
    if (!questions?.length || !currentAskerName) return
    const current = String(currentAskerName).trim().toLowerCase()
    const toExpand = {}
    for (const q of questions) {
      if (!q.answer) continue
      const askedBy = q.asked_by != null ? String(q.asked_by).trim().toLowerCase() : ''
      if (askedBy !== current) continue
      const rootId = findRootId(questions, q.id)
      if (rootId == null) continue
      if (!autoExpandedIdsRef.current.has(q.id)) toExpand[q.id] = true
      if (!autoExpandedIdsRef.current.has(rootId)) toExpand[rootId] = true
    }
    if (Object.keys(toExpand).length === 0) return
    Object.keys(toExpand).forEach((id) => autoExpandedIdsRef.current.add(id))
    setExpandedCards((prev) => ({ ...prev, ...toExpand }))
  }, [questions, currentAskerName])

  if (!questions || questions.length === 0) {
    return (
      <div className="info">
        {readOnly ? 'No questions yet in this session.' : 'No questions yet in this session. Ask a question above to get started!'}
      </div>
    )
  }

  const { roots, byParent } = buildThreadTree(questions)
  const handleToggleCard = (questionId) => {
    setExpandedCards((prev) => {
      const next = { ...prev, [questionId]: !prev[questionId] }
      if (!prev[questionId] && next[questionId] && markQuestionViewed && sessionId) {
        markQuestionViewed(sessionId, questionId)
      }
      return next
    })
  }

  const inlineReplyProps =
    !readOnly && setReplyingToQuestionId && questionText != null && setQuestionText && askSessionQuestion
      ? {
          questionText: questionText || '',
          setQuestionText,
          askSessionQuestion,
          loading: !!loading,
          onCancel: () => {
            setReplyingToQuestionId(null)
            setQuestionText('')
            cancelVoiceReview?.()
          },
          toggleVoiceRecording: toggleVoiceRecording || (() => {}),
          voiceRecording: !!voiceRecording,
          voiceUploading: !!voiceUploading,
          voiceFeedback: voiceFeedback || {},
          showVoiceConfirm: !!showVoiceConfirm,
          voiceTranscribedText: voiceTranscribedText || '',
          setVoiceTranscribedText: setVoiceTranscribedText || (() => {}),
          confirmVoiceQuestion: confirmVoiceQuestion || (() => {}),
          cancelVoiceReview: cancelVoiceReview || (() => {}),
          polishVoiceQuestion: polishVoiceQuestion || (() => {}),
          polishQuestionText: polishQuestionText || polishVoiceQuestion || null,
          voicePolishing: !!voicePolishing,
          voicePolishMode: voicePolishMode || '',
          askQuestionFeedback: askQuestionFeedback || {}
        }
      : null

  return (
    <div>
      <ThreadList
        roots={roots}
        byParent={byParent}
        unreadQuestionIds={unreadQuestionIds}
        onCitationClick={onCitationClick}
        onReply={readOnly ? undefined : onReply}
        expandedCards={expandedCards}
        onToggleCard={readOnly ? undefined : handleToggleCard}
        creatorDisplayName={creatorDisplayName}
        replyingToQuestionId={replyingToQuestionId}
        inlineReplyProps={inlineReplyProps}
        canDeleteQuestions={canDeleteQuestions}
        onRequestDelete={requestDeleteQuestion}
      />
    </div>
  )
}
