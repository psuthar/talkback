import { describe, it, expect, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { PlatformConnectionTile } from '../components/PlatformConnectionTile'

describe('PlatformConnectionTile', () => {
  it('renders null when enabled=false (tile fully removed)', () => {
    const { container } = render(
      <PlatformConnectionTile platform="zoom" enabled={false} connected={false} />
    )
    expect(container.firstChild).toBeNull()
  })

  it('unconnected state shows the Connect CTA', () => {
    render(
      <PlatformConnectionTile platform="zoom" enabled connected={false} onConnect={() => {}} />
    )
    expect(screen.getByTestId('platform-tile-zoom').getAttribute('data-state')).toBe('unconnected')
    expect(screen.getByTestId('platform-tile-zoom-connect').textContent).toContain('Connect Zoom')
  })

  it('connected state shows Browse + account email', () => {
    render(
      <PlatformConnectionTile
        platform="zoom"
        enabled
        connected
        accountEmail="z@example.com"
        onBrowse={() => {}}
      />
    )
    expect(screen.getByTestId('platform-tile-zoom').getAttribute('data-state')).toBe('connected')
    expect(screen.getByTestId('platform-tile-zoom-browse').textContent).toContain('Browse Zoom recordings')
    expect(screen.getByTestId('platform-tile-zoom-account').textContent).toBe('z@example.com')
  })

  it('Connect click calls onConnect and shows busy state', async () => {
    let resolve
    const onConnect = vi.fn().mockReturnValue(new Promise((r) => { resolve = r }))
    const user = userEvent.setup()
    render(
      <PlatformConnectionTile platform="zoom" enabled connected={false} onConnect={onConnect} />
    )
    await user.click(screen.getByTestId('platform-tile-zoom-connect'))
    expect(onConnect).toHaveBeenCalledTimes(1)
    expect(screen.getByTestId('platform-tile-zoom-connect').textContent).toContain('Connecting')
    resolve()
    await waitFor(() => expect(screen.getByTestId('platform-tile-zoom-connect').textContent).toContain('Connect Zoom'))
  })

  it('onConnect rejecting surfaces an inline error + Try again affordance', async () => {
    const onConnect = vi.fn().mockRejectedValue(new Error('oauth_blocked'))
    const user = userEvent.setup()
    render(
      <PlatformConnectionTile platform="zoom" enabled connected={false} onConnect={onConnect} />
    )
    await user.click(screen.getByTestId('platform-tile-zoom-connect'))
    await waitFor(() => expect(screen.getByTestId('platform-tile-zoom-error').textContent).toContain('oauth_blocked'))
    expect(screen.getByTestId('platform-tile-zoom-retry')).toBeTruthy()

    // Try again retries onConnect.
    await user.click(screen.getByTestId('platform-tile-zoom-retry'))
    expect(onConnect).toHaveBeenCalledTimes(2)
  })

  it('uses google_meet labels when platform=google_meet', () => {
    render(
      <PlatformConnectionTile platform="google_meet" enabled connected={false} onConnect={() => {}} />
    )
    expect(screen.getByTestId('platform-tile-google_meet-connect').textContent).toContain('Connect Google Meet')
  })

  it('uses teams labels when platform=teams', () => {
    render(
      <PlatformConnectionTile platform="teams" enabled connected={false} onConnect={() => {}} />
    )
    expect(screen.getByTestId('platform-tile-teams-connect').textContent).toContain('Connect Microsoft Teams')
  })

  // SCRUM-461: compact layout. The section header above the tile already
  // names the platform (subsectionLabel "IMPORT FROM ZOOM"), so the tile
  // itself no longer renders a redundant platform-name line. The
  // connected state renders Browse button + account email on a single
  // horizontal row.
  it('connected state omits the redundant platform-name label (SCRUM-461)', () => {
    render(
      <PlatformConnectionTile
        platform="zoom"
        enabled
        connected
        accountEmail="z@example.com"
        onBrowse={() => {}}
      />
    )
    expect(screen.queryByTestId('platform-tile-zoom-label')).toBeNull()
  })

  it('connected state lays out Browse + account email inline on one row (SCRUM-461)', () => {
    render(
      <PlatformConnectionTile
        platform="zoom"
        enabled
        connected
        accountEmail="z@example.com"
        onBrowse={() => {}}
      />
    )
    const tile = screen.getByTestId('platform-tile-zoom')
    expect(tile.style.display).toBe('flex')
    expect(tile.style.alignItems).toBe('center')
  })

  it('unconnected state omits the redundant platform-name label (SCRUM-461)', () => {
    render(
      <PlatformConnectionTile platform="zoom" enabled connected={false} onConnect={() => {}} />
    )
    expect(screen.queryByTestId('platform-tile-zoom-label')).toBeNull()
  })
})
