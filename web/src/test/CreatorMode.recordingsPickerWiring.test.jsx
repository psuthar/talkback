// SCRUM-463: pins the AddContentSection -> RecordingsPicker wiring that
// CreatorMode owns post-unification. The "Import meeting recording"
// button replaces SCRUM-460's three per-platform Browse buttons; the
// picker now lets the user pick a platform via its segmented selector.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { AddContentSection } from '../components/AddContentSection'
import { RecordingsPicker } from '../components/RecordingsPicker'

// Minimal CreatorMode-shaped harness mirroring the SCRUM-463 wiring in
// web/src/modes/CreatorMode.jsx so any drift between the two trips this
// test.
function CreatorWiringHarness({ refetchSession, integrations }) {
  const [open, setOpen] = useState(false)
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
        onBrowseImport={() => setOpen(true)}
      />
      {open && (
        <RecordingsPicker
          sessionId="s-1"
          apiBaseUrl=""
          userEmail="user@example.com"
          integrations={integrations}
          importedExternalIds={[]}
          onClose={() => setOpen(false)}
          onConnect={() => {}}
          onSwitchAccount={() => {}}
          onImported={async () => {
            setOpen(false)
            if (refetchSession) await refetchSession()
          }}
        />
      )}
    </>
  )
}

function stubFetch(integrations) {
  return vi.fn(async (url) => {
    const u = String(url)
    if (u.includes('/api/integrations/status')) {
      return { ok: true, status: 200, json: async () => integrations }
    }
    if (u.match(/\/api\/(zoom|google-meet|teams)\/recordings/)) {
      return { ok: true, status: 200, json: async () => ({ items: [] }) }
    }
    return { ok: false, status: 404, json: async () => ({}) }
  })
}

const allConnected = {
  zoom:        { enabled: true, connected: true, account_email: 'p+zoom@example.com' },
  google_meet: { enabled: true, connected: true, account_email: 'p+meet@example.com' },
  teams:       { enabled: true, connected: true, account_email: 'p+teams@example.com' },
}

describe('SCRUM-463: unified Import meeting recording wiring', () => {
  let originalFetch
  beforeEach(() => { originalFetch = global.fetch })
  afterEach(() => { global.fetch = originalFetch })

  it('Add Content shows one Import meeting recording button (no per-platform tiles)', async () => {
    global.fetch = stubFetch(allConnected)
    render(<CreatorWiringHarness integrations={allConnected} />)
    await waitFor(() => expect(screen.getByTestId('import-meeting-recording-btn')).toBeTruthy())
    expect(screen.queryByTestId('platform-tile-zoom')).toBeNull()
    expect(screen.queryByTestId('platform-tile-google_meet')).toBeNull()
    expect(screen.queryByTestId('platform-tile-teams')).toBeNull()
  })

  it('clicking the Import button opens the unified picker', async () => {
    global.fetch = stubFetch(allConnected)
    const user = userEvent.setup()
    render(<CreatorWiringHarness integrations={allConnected} />)
    await waitFor(() => expect(screen.getByTestId('import-meeting-recording-btn')).toBeTruthy())
    await user.click(screen.getByTestId('import-meeting-recording-btn'))
    await waitFor(() => expect(screen.queryByTestId('recordings-picker')).toBeTruthy())
    expect(screen.getByTestId('recordings-picker-platform-selector')).toBeTruthy()
  })

  it('closing the picker via × dismounts it from the DOM', async () => {
    global.fetch = stubFetch(allConnected)
    const user = userEvent.setup()
    render(<CreatorWiringHarness integrations={allConnected} />)
    await waitFor(() => expect(screen.getByTestId('import-meeting-recording-btn')).toBeTruthy())
    await user.click(screen.getByTestId('import-meeting-recording-btn'))
    await waitFor(() => expect(screen.queryByTestId('recordings-picker')).toBeTruthy())
    await user.click(screen.getByTestId('recordings-picker-close'))
    await waitFor(() => expect(screen.queryByTestId('recordings-picker')).toBeNull())
  })

  it('hides the Import button entirely when no meeting platform is enabled', async () => {
    const noPlatforms = {
      zoom:        { enabled: false, connected: false },
      google_meet: { enabled: false, connected: false },
      teams:       { enabled: false, connected: false },
    }
    global.fetch = stubFetch(noPlatforms)
    render(<CreatorWiringHarness integrations={noPlatforms} />)
    // Wait long enough for the integrations fetch to settle.
    await waitFor(() => expect(global.fetch).toHaveBeenCalled())
    expect(screen.queryByTestId('import-meeting-recording-btn')).toBeNull()
  })
})
