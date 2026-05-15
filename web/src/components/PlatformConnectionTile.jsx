// SCRUM-420: shared platform-connection tile used by the Zoom (SCRUM-420),
// Google Meet (SCRUM-XX10) and Teams (SCRUM-XX11) entries in Add Content.
//
// State matrix:
//   enabled=false  -> caller hides the tile entirely (return null).
//   connected=false -> "Connect <Platform>" CTA opens OAuth popup.
//   connected=true  -> "Browse <Platform> recordings" CTA with the
//                      accountEmail as secondary copy.
//
// onConnect launches the OAuth flow; the parent handles popup lifecycle.
// onBrowse opens the recordings picker (SCRUM-XX8b ships the picker itself).
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
        <div data-testid={`platform-tile-${platform}-label`}>{lbl.name}</div>
        <button
          data-testid={`platform-tile-${platform}-connect`}
          onClick={handleConnect}
          disabled={busy}
        >
          {busy ? 'Connecting…' : lbl.connect}
        </button>
        {error && (
          <>
            <p data-testid={`platform-tile-${platform}-error`}>{String(error)}</p>
            <button
              data-testid={`platform-tile-${platform}-retry`}
              onClick={handleConnect}
              disabled={busy}
            >
              Try again
            </button>
          </>
        )}
      </div>
    )
  }

  return (
    <div data-testid={`platform-tile-${platform}`} data-state="connected">
      <div data-testid={`platform-tile-${platform}-label`}>{lbl.name}</div>
      <button data-testid={`platform-tile-${platform}-browse`} onClick={onBrowse}>
        {lbl.browse}
      </button>
      {accountEmail && (
        <p data-testid={`platform-tile-${platform}-account`}>{accountEmail}</p>
      )}
    </div>
  )
}
