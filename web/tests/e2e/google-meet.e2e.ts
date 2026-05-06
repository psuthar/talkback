import { test, expect } from '@playwright/test'
import {
  createUserAndLoginWithId,
  deleteUserViaAdmin,
  loginAsAdmin,
  uniqueEmail,
} from './fixtures'

// SCRUM-325: Google Meet integration E2E. Unlike the other e2e specs in this
// directory, this test does NOT depend on ENABLE_GOOGLE_MEET being set on the
// backend — it intercepts /api/google-meet/* responses with page.route() so
// the SPA renders Meet UI regardless of backend feature-flag state. This keeps
// the spec hermetic: no Google Cloud OAuth app, no real recordings, no
// dependency on a Workspace tenant. The integration's gated routes are
// covered by the SCRUM-324 backend smoke tests.

test.setTimeout(60_000)

let seededUserId = ''

test.afterAll(async ({ request }) => {
  await loginAsAdmin(request)
  if (seededUserId) await deleteUserViaAdmin(request, seededUserId)
})

test.beforeEach(async ({ page }) => {
  // Status: enabled + connected with workspace-eligible Workspace user.
  await page.route('**/api/google-meet/status*', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        enabled: true,
        connected: true,
        google_email: 'meet-e2e@workspace.example',
        google_user_id: 'meet-sub-e2e',
        workspace_eligible: true,
        expires_at: new Date(Date.now() + 3600_000).toISOString(),
      }),
    })
  )

  // Recordings: 3 rows covering the three transcript_state badges.
  await page.route('**/api/google-meet/recordings*', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        items: [
          {
            conference_record_name: 'conferenceRecords/A',
            recording_name: 'conferenceRecords/A/recordings/r1',
            subject: 'Sprint demo (ready transcript)',
            start_time: '2026-05-01T10:00:00Z',
            drive_file_id: 'drive-1',
            export_uri: 'https://drive.google.com/file/d/drive-1/view',
            state: 'FILE_GENERATED',
            transcript_state: 'ready',
          },
          {
            conference_record_name: 'conferenceRecords/B',
            recording_name: 'conferenceRecords/B/recordings/r1',
            subject: 'Standup (transcript pending)',
            start_time: '2026-05-02T09:00:00Z',
            drive_file_id: 'drive-2',
            state: 'FILE_GENERATED',
            transcript_state: 'pending',
          },
          {
            conference_record_name: 'conferenceRecords/C',
            recording_name: 'conferenceRecords/C/recordings/r1',
            subject: 'Customer call (no transcript)',
            start_time: '2026-05-03T14:00:00Z',
            drive_file_id: 'drive-3',
            state: 'FILE_GENERATED',
            transcript_state: 'none',
          },
        ],
      }),
    })
  )
})

test('From Google Meet tile renders, recordings list shows transcript-state badges, import modal opens', async ({
  page,
  context,
  request,
}) => {
  const email = uniqueEmail('meet-e2e')
  seededUserId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', 'Meet E2E User')
  await page.goto('/')
  await page.waitForLoadState('networkidle')

  // Open create-session flow.
  const createBtn = page.getByRole('button', { name: /Create new session/i }).first()
  if (await createBtn.isVisible().catch(() => false)) {
    await createBtn.click()
  }

  // The "From Google Meet" tile should render because /api/google-meet/status
  // is mocked to enabled:true.
  const meetTile = page.getByRole('button', { name: 'From Google Meet' })
  await expect(meetTile).toBeVisible({ timeout: 10_000 })
  await meetTile.click()

  // Connected pill — workspace_eligible:true → success color, no warning text.
  await expect(page.getByText(/Connected as meet-e2e@workspace\.example/)).toBeVisible()
  await expect(page.getByText(/Workspace Business Standard/)).not.toBeVisible()

  // Disconnect microcopy.
  await expect(page.getByText(/Disconnecting won.t affect sessions/)).toBeVisible()

  // Load recordings.
  await page.getByRole('button', { name: 'Load recordings' }).click()

  // Three rows; each badge appears at least once.
  await expect(page.getByText('Sprint demo (ready transcript)')).toBeVisible()
  await expect(page.getByText('Standup (transcript pending)')).toBeVisible()
  await expect(page.getByText('Customer call (no transcript)')).toBeVisible()
  await expect(page.getByText(/^Transcript$/).first()).toBeVisible()
  await expect(page.getByText('Transcript pending').first()).toBeVisible()
  await expect(page.getByText('No transcript').first()).toBeVisible()

  // Open import modal on the no-transcript row — assert the Meet-specific note appears.
  const noTranscriptRow = page.locator('text=Customer call (no transcript)').locator('..').locator('..')
  await noTranscriptRow.getByRole('button', { name: 'Import' }).click()
  await expect(page.getByRole('heading', { name: 'Import Google Meet recording' })).toBeVisible()
  await expect(page.getByText(/no transcript/i)).toBeVisible()
  await expect(page.getByText(/Q&A quality is best when a transcript is available/)).toBeVisible()

  // Default session name is the recording subject.
  const nameInput = page.getByRole('textbox')
  await expect(nameInput).toHaveValue('Customer call (no transcript)')

  // Empty name disables the Import button.
  await nameInput.fill('')
  await expect(page.getByRole('button', { name: 'Import', exact: true })).toBeDisabled()

  // Cancel closes the modal.
  await page.getByRole('button', { name: 'Cancel' }).click()
  await expect(page.getByRole('heading', { name: 'Import Google Meet recording' })).not.toBeVisible()
})
