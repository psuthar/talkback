import { useEffect, useMemo, useRef, useState } from 'react'

/**
 * SCRUM-367: Cascade-aware delete confirmation modal for a top-level question.
 *
 * Props:
 *   - question: the root question object being deleted
 *   - replyCount: number of descendant replies that will be deleted
 *   - hasConfirmedAnswer: true when any answer in the cascade has confirmed=true.
 *     When true, we render a heightened type-to-confirm variant: Delete is
 *     disabled until the user types the question's first word (case-insensitive).
 *   - onCancel(): user clicked Cancel or backdrop
 *   - onConfirm(): user clicked Delete with all gates satisfied
 */
export function DeleteQuestionModal({ question, replyCount = 0, hasConfirmedAnswer = false, onCancel, onConfirm }) {
  const [typed, setTyped] = useState('')
  const inputRef = useRef(null)
  const cancelBtnRef = useRef(null)

  const firstWord = useMemo(() => {
    const txt = (question?.question_text || '').trim()
    if (!txt) return ''
    const m = txt.split(/\s+/)[0] || ''
    return m.replace(/[^\p{L}\p{N}]+$/u, '')
  }, [question?.question_text])

  // Focus the type-to-confirm input on open, otherwise the Cancel button.
  useEffect(() => {
    if (hasConfirmedAnswer) {
      inputRef.current?.focus()
    } else {
      cancelBtnRef.current?.focus()
    }
  }, [hasConfirmedAnswer])

  // Esc cancels.
  useEffect(() => {
    const onKey = (e) => {
      if (e.key === 'Escape') onCancel?.()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onCancel])

  const typeMatches =
    !hasConfirmedAnswer ||
    (firstWord && typed.trim().toLowerCase() === firstWord.toLowerCase())

  const title = hasConfirmedAnswer ? 'Delete this confirmed Q&A?' : 'Delete this question?'

  const body =
    replyCount > 0
      ? `This will also delete ${replyCount} ${replyCount === 1 ? 'reply' : 'replies'} and their answers. This can't be undone after 5 seconds.`
      : "This can't be undone after 5 seconds."

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="delete-question-modal-title"
      data-testid="delete-question-modal"
      style={{
        position: 'fixed',
        inset: 0,
        zIndex: 1100,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        background: 'rgba(0,0,0,0.5)'
      }}
      onClick={() => onCancel?.()}
    >
      <div
        style={{
          background: '#fff',
          padding: '24px',
          borderRadius: '8px',
          boxShadow: '0 4px 20px rgba(0,0,0,0.2)',
          maxWidth: '440px',
          width: '90%'
        }}
        onClick={(e) => e.stopPropagation()}
      >
        <h3 id="delete-question-modal-title" style={{ marginTop: 0, marginBottom: '12px', fontSize: '18px' }}>
          {title}
        </h3>
        <p style={{ marginTop: 0, marginBottom: '16px', color: '#555' }}>{body}</p>
        {hasConfirmedAnswer && (
          <div style={{ marginBottom: '16px' }}>
            <label
              htmlFor="delete-question-type-confirm"
              style={{ display: 'block', fontSize: '13px', color: '#444', marginBottom: '6px' }}
            >
              Type the question's first word to confirm:{' '}
              <strong>{firstWord}</strong>
            </label>
            <input
              id="delete-question-type-confirm"
              ref={inputRef}
              data-testid="delete-question-type-confirm"
              type="text"
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
              autoComplete="off"
              style={{
                width: '100%',
                padding: '8px 10px',
                border: '1px solid #bbb',
                borderRadius: '4px',
                fontSize: '14px'
              }}
            />
          </div>
        )}
        <div style={{ display: 'flex', gap: '12px', justifyContent: 'flex-end' }}>
          <button
            ref={cancelBtnRef}
            type="button"
            data-testid="delete-question-cancel"
            onClick={(e) => { e.stopPropagation(); onCancel?.() }}
            style={{
              padding: '8px 16px',
              border: '1px solid #666',
              borderRadius: '4px',
              cursor: 'pointer',
              background: '#f0f0f0',
              color: '#333',
              fontWeight: 500
            }}
          >
            Cancel
          </button>
          <button
            type="button"
            data-testid="delete-question-confirm"
            disabled={!typeMatches}
            onClick={(e) => {
              e.stopPropagation()
              if (!typeMatches) return
              onConfirm?.()
            }}
            style={{
              padding: '8px 16px',
              backgroundColor: typeMatches ? 'var(--color-danger-mid)' : '#e0a3a3',
              color: '#fff',
              border: 'none',
              borderRadius: '4px',
              cursor: typeMatches ? 'pointer' : 'not-allowed'
            }}
          >
            Delete
          </button>
        </div>
      </div>
    </div>
  )
}
