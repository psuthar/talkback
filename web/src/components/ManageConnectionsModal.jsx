// SCRUM-419: Manage Connections modal — lists Zoom / Google Meet / Teams
// connection state and lets the user disconnect each (with a confirmation
// dialog that calls out: imported recordings stay in their sessions).
// Reads status from the SCRUM-418 useIntegrationsStatus hook.
import { useState, useCallback } from 'react'
import { useIntegrationsStatus } from '../hooks/useIntegrationsStatus'

const PLATFORMS = [
  { key: 'zoom', label: 'Zoom', disconnectPath: '/api/zoom/disconnect' },
  { key: 'google_meet', label: 'Google Meet', disconnectPath: '/api/google-meet/disconnect' },
  { key: 'teams', label: 'Microsoft Teams', disconnectPath: '/api/teams/disconnect' },
]

function ConfirmDialog({ platformLabel, onConfirm, onCancel }) {
  return (
    <div data-testid="disconnect-confirm" role="dialog" aria-label={`Disconnect ${platformLabel}`}>
      <p>
        Disconnecting {platformLabel} won&apos;t remove already-imported recordings from your sessions.
        New imports from this account will require reconnecting. Are you sure?
      </p>
      <button data-testid="disconnect-confirm-button" onClick={onConfirm}>Disconnect</button>
      <button data-testid="disconnect-cancel-button" onClick={onCancel}>Cancel</button>
    </div>
  )
}

export function ManageConnectionsModal({ apiBaseUrl, userEmail, onClose }) {
  const { status, loading, error, refresh } = useIntegrationsStatus(apiBaseUrl)
  const [pendingDisconnect, setPendingDisconnect] = useState(null) // platform key
  const [disconnecting, setDisconnecting] = useState(false)
  const [disconnectError, setDisconnectError] = useState(null)

  const requestDisconnect = (platformKey) => {
    setDisconnectError(null)
    setPendingDisconnect(platformKey)
  }

  const cancelDisconnect = () => setPendingDisconnect(null)

  const confirmDisconnect = useCallback(async () => {
    const platform = PLATFORMS.find((p) => p.key === pendingDisconnect)
    if (!platform || !apiBaseUrl) {
      setPendingDisconnect(null)
      return
    }
    setDisconnecting(true)
    setDisconnectError(null)
    try {
      const base = apiBaseUrl.replace(/\/$/, '')
      const headers = { 'Accept': 'application/json' }
      if (userEmail) headers['X-Creator-Identity'] = userEmail
      const res = await fetch(`${base}${platform.disconnectPath}`, {
        method: 'POST',
        credentials: 'include',
        headers,
      })
      if (!res.ok && res.status !== 204) {
        throw new Error(`disconnect ${platform.key}: ${res.status}`)
      }
      setPendingDisconnect(null)
      await refresh()
    } catch (err) {
      setDisconnectError(err)
    } finally {
      setDisconnecting(false)
    }
  }, [apiBaseUrl, pendingDisconnect, refresh, userEmail])

  return (
    <div data-testid="manage-connections-modal" role="dialog" aria-label="Manage Connections">
      <header>
        <h2>Manage Connections</h2>
        <button data-testid="manage-connections-close" onClick={onClose}>Close</button>
      </header>

      {loading && <p data-testid="manage-connections-loading">Loading…</p>}
      {error && <p data-testid="manage-connections-error">Failed to load: {String(error)}</p>}

      {status && (
        <ul data-testid="manage-connections-list">
          {PLATFORMS.map((p) => {
            const platformStatus = status[p.key] || { enabled: false, connected: false, account_email: null }
            const disabled = !platformStatus.enabled
            return (
              <li key={p.key} data-testid={`connection-row-${p.key}`} aria-disabled={disabled}>
                <span data-testid={`connection-label-${p.key}`}>{p.label}</span>
                {disabled ? (
                  <span data-testid={`connection-state-${p.key}`}>Not available</span>
                ) : platformStatus.connected ? (
                  <>
                    <span data-testid={`connection-state-${p.key}`}>
                      Connected{platformStatus.account_email ? ` (${platformStatus.account_email})` : ''}
                    </span>
                    <button
                      data-testid={`disconnect-${p.key}`}
                      onClick={() => requestDisconnect(p.key)}
                      disabled={disconnecting}
                    >
                      Disconnect
                    </button>
                  </>
                ) : (
                  <span data-testid={`connection-state-${p.key}`}>Not connected</span>
                )}
              </li>
            )
          })}
        </ul>
      )}

      {pendingDisconnect && (
        <ConfirmDialog
          platformLabel={PLATFORMS.find((p) => p.key === pendingDisconnect)?.label || pendingDisconnect}
          onConfirm={confirmDisconnect}
          onCancel={cancelDisconnect}
        />
      )}

      {disconnectError && (
        <p data-testid="disconnect-error">Disconnect failed: {String(disconnectError)}</p>
      )}
    </div>
  )
}
