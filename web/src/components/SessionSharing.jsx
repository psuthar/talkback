import { useState } from 'react'
import { buildCanonicalSessionUrl } from '../sessionNavigation'

export function SessionSharing({ sessionId, sessionTitle, apiBaseUrl }) {
  const [copied, setCopied] = useState(false)
  const q = { ...(apiBaseUrl ? { api: apiBaseUrl } : {}) }
  const defaultUrl = buildCanonicalSessionUrl(sessionId, q) // creator default (no mode)
  const creatorUrl = buildCanonicalSessionUrl(sessionId, { ...q, mode: 'edit' })
  const participantUrl = buildCanonicalSessionUrl(sessionId, { ...q, mode: 'view' })
  
  const copyToClipboard = async (text) => {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch (err) {
      console.error('Failed to copy:', err)
    }
  }

  return (
    <div className="section" style={{ 
      marginBottom: '20px', 
      padding: '15px', 
      backgroundColor: 'var(--color-primary-bg)', 
      borderRadius: '5px',
      border: '1px solid #2196F3'
    }}>
      <h2>Share Session</h2>
      <div style={{ marginBottom: '15px' }}>
        <div style={{ fontWeight: 'bold', marginBottom: '8px' }}>Session ID:</div>
        <div style={{ display: 'flex', gap: '10px', alignItems: 'center', marginBottom: '10px' }}>
          <code style={{ 
            flex: 1, 
            padding: '8px', 
            backgroundColor: '#fff', 
            borderRadius: '4px',
            border: '1px solid #ddd',
            fontSize: '12px',
            wordBreak: 'break-all'
          }}>
            {sessionId}
          </code>
          <button
            onClick={() => copyToClipboard(sessionId)}
            style={{
              padding: '8px 16px',
              backgroundColor: copied ? 'var(--color-success-mid)' : 'var(--color-primary-mid)',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              cursor: 'pointer',
              whiteSpace: 'nowrap'
            }}
          >
            {copied ? '✓ Copied!' : 'Copy ID'}
          </button>
        </div>
      </div>
      
      <div style={{ marginBottom: '15px' }}>
        <div style={{ fontWeight: 'bold', marginBottom: '8px' }}>
          Default Link (Creator Mode) <span style={{ fontSize: '12px', fontWeight: 'normal', color: '#666' }}>— Recommended</span>:
        </div>
        <div style={{ display: 'flex', gap: '10px', alignItems: 'center', marginBottom: '10px' }}>
          <input
            type="text"
            value={defaultUrl}
            readOnly
            style={{
              flex: 1,
              padding: '8px',
              border: '2px solid #2196F3',
              borderRadius: '4px',
              fontSize: '12px',
              backgroundColor: '#f0f8ff'
            }}
          />
          <button
            onClick={() => copyToClipboard(defaultUrl)}
            style={{
              padding: '8px 16px',
              backgroundColor: copied ? 'var(--color-success-mid)' : 'var(--color-primary-mid)',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              cursor: 'pointer',
              whiteSpace: 'nowrap'
            }}
          >
            {copied ? '✓ Copied!' : 'Copy'}
          </button>
        </div>
      </div>
      
      <div style={{ marginBottom: '15px' }}>
        <div style={{ fontWeight: 'bold', marginBottom: '8px' }}>Participant Link (View Mode):</div>
        <div style={{ display: 'flex', gap: '10px', alignItems: 'center', marginBottom: '10px' }}>
          <input
            type="text"
            value={participantUrl}
            readOnly
            style={{
              flex: 1,
              padding: '8px',
              border: '1px solid #ddd',
              borderRadius: '4px',
              fontSize: '12px'
            }}
          />
          <button
            onClick={() => copyToClipboard(participantUrl)}
            style={{
              padding: '8px 16px',
              backgroundColor: copied ? 'var(--color-success-mid)' : 'var(--color-primary-mid)',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              cursor: 'pointer',
              whiteSpace: 'nowrap'
            }}
          >
            {copied ? '✓ Copied!' : 'Copy'}
          </button>
        </div>
      </div>
      
      <div style={{ marginTop: '15px', padding: '10px', backgroundColor: '#fff', borderRadius: '4px', fontSize: '13px', color: '#666' }}>
        <strong>Tip:</strong> The default link opens in creator mode. Share the participant link with viewers so they can ask questions.
      </div>
    </div>
  )
}
