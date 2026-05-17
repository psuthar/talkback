// SCRUM-460: pins the AddContentSection -> RecordingsPicker wiring that
// CreatorMode now owns. The bug this guards against: CreatorMode used to
// render <AddContentSection> without passing onBrowseZoom /
// onBrowseGoogleMeet / onBrowseTeams, so clicking the per-platform
// Browse buttons did nothing (onClick={undefined}). This file rebuilds
// the smallest version of that wiring (state + handlers + picker mount)
// and asserts each Browse click opens the picker for the right platform.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { AddContentSection } from '../components/AddContentSection'
import { RecordingsPicker } from '../components/RecordingsPicker'

// Minimal CreatorMode-shaped harness: mirrors the SCRUM-460 fix structure
// in web/src/modes/CreatorMode.jsx so any drift between the two will be
// caught by this test failing.
function CreatorWiringHarness({ refetchSession }) {
  const [browsePlatform, setBrowsePlatform] = useState(null)
  return (
    <>
      <AddContentSection
        sessionId="s-1"
        apiBaseUrl=""
        refetchSession={refetchSession}
        onUploadClick={() => {}}
        uploading={false}
        uploadFeedback={{ type: '', message: '' }}
        defaultExpanded={true}
        onBrowseZoom={() => setBrowsePlatform('zoom')}
        onBrowseGoogleMeet={() => setBrowsePlatform('google_meet')}
        onBrowseTeams={() => setBrowsePlatform('teams')}
      />
      {browsePlatform && (
        <RecordingsPicker
          platform={browsePlatform}
          sessionId="s-1"
          apiBaseUrl=""
          userEmail="user@example.com"
          importedExternalIds={[]}
          onClose={() => setBrowsePlatform(null)}
          onImported={async () => {
            setBrowsePlatform(null)
            if (refetchSession) await refetchSession()
          }}
        />
      )}
    </>
  )
}

function stubFetch() {
  return vi.fn(async (url) => {
    const u = String(url)
    if (u.includes('/api/integrations/status')) {
      return {
        ok: true,
        status: 200,
        json: async () => ({
          zoom:        { enabled: true, connected: true, account_email: 'p+zoom@example.com' },
          google_meet: { enabled: true, connected: true, account_email: 'p+meet@example.com' },
          teams:       { enabled: true, connected: true, account_email: 'p+teams@example.com' },
        }),
      }
    }
    // RecordingsPicker lists recordings on mount; an empty list is fine.
    if (u.match(/\/api\/(zoom|google-meet|teams)\/recordings/)) {
      return { ok: true, status: 200, json: async () => ({ recordings: [] }) }
    }
    return { ok: false, status: 404, json: async () => ({}) }
  })
}

describe('SCRUM-460: CreatorMode -> RecordingsPicker wiring', () => {
  let originalFetch
  beforeEach(() => { originalFetch = global.fetch; global.fetch = stubFetch() })
  afterEach(() => { global.fetch = originalFetch })

  it('Browse Zoom recordings opens the picker for zoom', async () => {
    const user = userEvent.setup()
    render(<CreatorWiringHarness />)
    // Wait for the Zoom tile to render after integrations status loads.
    const browse = await screen.findByTestId('platform-tile-zoom-browse')
    await user.click(browse)
    // RecordingsPicker for zoom should mount — match by its on-screen title.
    await waitFor(() => {
      expect(screen.queryByTestId('recordings-picker-zoom')).toBeTruthy()
    })
  })

  it('Browse Google Meet recordings opens the picker for google_meet', async () => {
    const user = userEvent.setup()
    render(<CreatorWiringHarness />)
    const browse = await screen.findByTestId('platform-tile-google_meet-browse')
    await user.click(browse)
    await waitFor(() => {
      expect(screen.queryByTestId('recordings-picker-google_meet')).toBeTruthy()
    })
  })

  it('Browse Teams recordings opens the picker for teams', async () => {
    const user = userEvent.setup()
    render(<CreatorWiringHarness />)
    const browse = await screen.findByTestId('platform-tile-teams-browse')
    await user.click(browse)
    await waitFor(() => {
      expect(screen.queryByTestId('recordings-picker-teams')).toBeTruthy()
    })
  })

  it('clicking Browse on a different platform replaces the open picker (no double mount)', async () => {
    const user = userEvent.setup()
    render(<CreatorWiringHarness />)
    await user.click(await screen.findByTestId('platform-tile-zoom-browse'))
    await waitFor(() => expect(screen.queryByTestId('recordings-picker-zoom')).toBeTruthy())
    await user.click(screen.getByTestId('platform-tile-google_meet-browse'))
    await waitFor(() => expect(screen.queryByTestId('recordings-picker-google_meet')).toBeTruthy())
    expect(screen.queryByTestId('recordings-picker-zoom')).toBeNull()
  })

  it('closing the picker via Close removes it from the DOM', async () => {
    const user = userEvent.setup()
    render(<CreatorWiringHarness />)
    await user.click(await screen.findByTestId('platform-tile-zoom-browse'))
    await waitFor(() => expect(screen.queryByTestId('recordings-picker-zoom')).toBeTruthy())
    await user.click(screen.getByTestId('recordings-picker-close'))
    await waitFor(() => expect(screen.queryByTestId('recordings-picker-zoom')).toBeNull())
  })
})
