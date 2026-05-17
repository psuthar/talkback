// SCRUM-420: shared platform-connection tile used by the Zoom (SCRUM-420),
// Google Meet (SCRUM-XX10) and Teams (SCRUM-XX11) entries in Add Content.
//
// SCRUM-461 (compact layout): the section header above the tile already
// names the platform ("IMPORT FROM ZOOM"), so the tile drops its own
// redundant platform-name line. Connected state renders the Browse
// button + accountEmail on a single horizontal row to roughly halve the
// vertical space each tile takes in the sidebar.
//
// State matrix:
//   enabled=false  -> caller hides the tile entirely (return null).
//   connected=false -> "Connect <Platform>" CTA opens OAuth popup.
//   connected=true  -> "Browse <Platform> recordings" CTA inline with
//                      the accountEmail as muted secondary copy.
//
// onConnect launches the OAuth flow; the parent handles popup lifecycle.
// onBrowse opens the recordings picker (SCRUM-421 ships the picker itself).
//
// Errors raised by the OAuth flow surface inline + a "Try again" affordance.
import { useState, useCallback } from 'react'

const labels = {
  zoom: { name: 'Zoom', connect: 'Connect Zoom', browse: 'Browse Zoom recordings' },
  google_meet: { name: 'Google Meet', connect: 'Connect Google Meet', browse: 'Browse Google Meet recordings' },
  teams: { name: 'Microsoft Teams', connect: 'Connect Microsoft Teams', browse: 'Browse Teams recordings' },
}

export function PlatformConnectionTile({
  platform,
  enabled,
  connected,
  accountEmail,
  onConnect,
  onBrowse,
}) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState(null)

  const lbl = labels[platform] || { name: platform, connect: `Connect ${platform}`, browse: `Browse ${platform} recordings` }

  const handleConnect = useCallback(async () => {
    setError(null)
    setBusy(true)
    try {
      await Promise.resolve(onConnect?.())
    } catch (err) {
      setError(err)
    } finally {
      setBusy(false)
    }
  }, [onConnect])

  if (!enabled) {
    // Tile is hidden — fully removed from layout (per SCRUM-420 spec).
    return null
  }

  if (!connected) {
    return (
      <div data-testid={`platform-tile-${platform}`} data-state="unconnected">
        <button
          data-testid={`platform-tile-${platform}-connect`}
          onClick={handleConnect}
          disabled={busy}
          style={tileButtonStyle}
        >
          {busy ? 'Connecting…' : lbl.connect}
        </button>
        {error && (
          <>
            <p data-testid={`platform-tile-${platform}-error`} style={tileErrorStyle}>{String(error)}</p>
            <button
              data-testid={`platform-tile-${platform}-retry`}
              onClick={handleConnect}
              disabled={busy}
              style={tileButtonStyle}
            >
              Try again
            </button>
          </>
        )}
      </div>
    )
  }

  return (
    <div
      data-testid={`platform-tile-${platform}`}
      data-state="connected"
      style={tileRowStyle}
    >
      <button
        data-testid={`platform-tile-${platform}-browse`}
        onClick={onBrowse}
        style={tileButtonStyle}
      >
        {lbl.browse}
      </button>
      {accountEmail && (
        <span data-testid={`platform-tile-${platform}-account`} style={tileEmailStyle}>
          {accountEmail}
        </span>
      )}
    </div>
  )
}

// SCRUM-461: shared inline styles for the compact tile. Kept inline so
// existing tests don't need a CSS-module import and so the tile renders
// the same way in storybook / vitest.
const tileRowStyle = {
  display: 'flex',
  alignItems: 'center',
  flexWrap: 'wrap',
  gap: '6px 8px',
}
const tileButtonStyle = {
  padding: '4px 10px',
  fontSize: '12px',
  fontWeight: 500,
  borderRadius: '4px',
  border: 'none',
  backgroundColor: 'var(--color-primary, #1976d2)',
  color: '#fff',
  cursor: 'pointer',
}
const tileEmailStyle = {
  fontSize: '12px',
  color: '#666',
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
  maxWidth: '180px',
}
const tileErrorStyle = {
  fontSize: '12px',
  color: 'var(--color-danger, #c62828)',
  margin: '4px 0',
}
