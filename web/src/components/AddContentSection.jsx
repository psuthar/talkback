import { useCallback, useState } from 'react'
import { useIntegrationsStatus } from '../hooks/useIntegrationsStatus'
import { PlatformConnectionTile } from './PlatformConnectionTile'

export function AddContentSection({
  sessionId,
  apiBaseUrl,
  refetchSession,
  onUploadClick,
  uploading = false,
  uploadFeedback,
  defaultExpanded = true,
  onBrowseZoom,
  onBrowseGoogleMeet,
  onBrowseTeams,
}) {
  const { status: integrations, refresh: refreshIntegrations } = useIntegrationsStatus(apiBaseUrl)

  // SCRUM-420 / SCRUM-422 / SCRUM-423: open the platform's OAuth flow in a
  // popup. The popup-closed poll refreshes integrations status so the tile
  // re-renders into the connected state without a full page reload.
  const buildConnect = useCallback((connectPath, label) => () => {
    const base = (apiBaseUrl || '').replace(/\/$/, '')
    return new Promise((resolve, reject) => {
      let popup
      try {
        popup = window.open(`${base}${connectPath}`, `${label}_oauth`, 'width=600,height=720')
      } catch (err) {
        reject(err)
        return
      }
      if (!popup) {
        reject(new Error(`Popup blocked — allow pop-ups for this site to connect ${label}.`))
        return
      }
      const interval = setInterval(() => {
        if (popup.closed) {
          clearInterval(interval)
          refreshIntegrations()
          resolve()
        }
      }, 500)
    })
  }, [apiBaseUrl, refreshIntegrations])

  const connectZoom = useCallback(buildConnect('/api/zoom/connect', 'Zoom'), [buildConnect])
  const connectGoogleMeet = useCallback(buildConnect('/api/google-meet/connect', 'Google Meet'), [buildConnect])
  const connectTeams = useCallback(buildConnect('/api/teams/connect', 'Microsoft Teams'), [buildConnect])
  const [expanded, setExpanded] = useState(defaultExpanded)
  const [urlInput, setUrlInput] = useState('')
  const [adding, setAdding] = useState(false)
  const [addError, setAddError] = useState(null)

  const base = (apiBaseUrl || '').replace(/\/$/, '')

  const addLink = async () => {
    const url = (urlInput || '').trim()
    if (!url || !sessionId) return
    setAddError(null)
    setAdding(true)
    try {
      const res = await fetch(`${base}/api/sessions/${sessionId}/links`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ url }),
      })
      const data = await res.json().catch(() => ({}))
      if (res.ok) {
        setUrlInput('')
        if (typeof refetchSession === 'function') await refetchSession()
      } else {
        if (res.status === 429 && data.max_links != null) {
          setAddError(`Session links limit reached (max ${data.max_links}).`)
        } else {
          setAddError(data.error || `HTTP ${res.status}`)
        }
      }
    } catch (err) {
      setAddError(err?.message || 'Failed to add link')
    } finally {
      setAdding(false)
    }
  }

  const subsectionLabel = {
    fontSize: '12px',
    fontWeight: 700,
    textTransform: 'uppercase',
    color: '#444',
    marginBottom: '6px',
  }

  return (
    <div style={{ borderBottom: '1px solid #e0e0e0' }}>
      <div style={{ padding: '8px 12px' }}>
        <button
          type="button"
          data-testid="add-content-toggle"
          className="creator-collapsible-btn"
          onClick={() => setExpanded(e => !e)}
          aria-expanded={expanded}
          aria-controls="add-content-body"
        >
          <span style={{ fontSize: '12px', color: '#555' }} aria-hidden>{expanded ? '▼' : '▷'}</span>
          {' '}Add content
        </button>
      </div>

      {expanded && (
        <div id="add-content-body" style={{ padding: '0 12px 12px', display: 'flex', flexDirection: 'column', gap: '12px' }}>

          {/* Upload file */}
          <div>
            <div style={subsectionLabel}>Upload file</div>
            <button
              type="button"
              data-testid="upload-file-btn"
              onClick={onUploadClick}
              disabled={uploading}
              aria-label="Choose a file to upload to this session"
              style={{
                width: '100%',
                padding: '10px',
                border: '1px dashed #81c784',
                borderRadius: '4px',
                background: uploading ? '#f5f5f5' : '#f1f8e9',
                color: uploading ? '#999' : 'var(--color-success)',
                fontSize: '13px',
                fontWeight: 500,
                cursor: uploading ? 'not-allowed' : 'pointer',
                textAlign: 'center',
                margin: 0,
              }}
            >
              {uploading ? 'Uploading…' : '+ Choose file'}
            </button>
            <div style={{ fontSize: '12px', color: '#888', marginTop: '4px' }}>
              PDF, DOCX, XLSX, PPTX, images, MP4
            </div>
            {uploadFeedback?.message && (
              <div
                className={uploadFeedback.type}
                role={uploadFeedback.type === 'error' ? 'alert' : 'status'}
                style={{ fontSize: '12px', padding: '4px 6px', marginTop: '4px' }}
              >
                {uploadFeedback.message}
              </div>
            )}
          </div>

          <hr style={{ border: 'none', borderTop: '1px solid #eee', margin: 0 }} />

          {/* SCRUM-420: Zoom tile. */}
          {integrations && integrations.zoom?.enabled && (
            <>
              <div>
                <div style={subsectionLabel}>Import from Zoom</div>
                <PlatformConnectionTile
                  platform="zoom"
                  enabled={integrations.zoom.enabled}
                  connected={integrations.zoom.connected}
                  accountEmail={integrations.zoom.account_email}
                  onConnect={connectZoom}
                  onBrowse={onBrowseZoom}
                />
              </div>
              <hr style={{ border: 'none', borderTop: '1px solid #eee', margin: 0 }} />
            </>
          )}

          {/* SCRUM-422: Google Meet tile (reuses PlatformConnectionTile). */}
          {integrations && integrations.google_meet?.enabled && (
            <>
              <div>
                <div style={subsectionLabel}>Import from Google Meet</div>
                <PlatformConnectionTile
                  platform="google_meet"
                  enabled={integrations.google_meet.enabled}
                  connected={integrations.google_meet.connected}
                  accountEmail={integrations.google_meet.account_email}
                  onConnect={connectGoogleMeet}
                  onBrowse={onBrowseGoogleMeet}
                />
              </div>
              <hr style={{ border: 'none', borderTop: '1px solid #eee', margin: 0 }} />
            </>
          )}

          {/* SCRUM-423: Teams tile lands in a follow-up PR (mirrors this pattern). */}

          {/* Add link */}
          <div>
            <div style={subsectionLabel}>Add link</div>
            <div style={{ display: 'flex', gap: '6px' }}>
              <input
                type="url"
                data-testid="add-link-url-input"
                placeholder="https://..."
                value={urlInput}
                onChange={(e) => setUrlInput(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && addLink()}
                disabled={adding}
                aria-label="URL to add to this session"
                style={{
                  flex: 1,
                  minWidth: 0,
                  padding: '6px 8px',
                  fontSize: '12px',
                  border: '1px solid #ddd',
                  borderRadius: '3px',
                }}
              />
              <button
                type="button"
                data-testid="add-link-submit-btn"
                onClick={addLink}
                disabled={adding || !urlInput.trim()}
                style={{
                  padding: '6px 10px',
                  fontSize: '12px',
                  margin: 0,
                  backgroundColor: 'var(--color-primary)',
                  color: '#fff',
                  border: 'none',
                  borderRadius: '3px',
                  cursor: adding || !urlInput.trim() ? 'not-allowed' : 'pointer',
                  flexShrink: 0,
                }}
              >
                {adding ? '…' : 'Add'}
              </button>
            </div>
            {addError && (
              <div
                role="alert"
                style={{ fontSize: '12px', color: 'var(--color-danger-dark)', marginTop: '4px' }}
              >
                {addError}{' '}
                <button
                  type="button"
                  onClick={() => setAddError(null)}
                  style={{ fontSize: '12px', color: 'var(--color-danger-dark)', background: 'none', border: 'none', cursor: 'pointer', padding: 0, textDecoration: 'underline' }}
                >
                  Dismiss
                </button>
              </div>
            )}
          </div>

        </div>
      )}
    </div>
  )
}
