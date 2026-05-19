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
//
// SCRUM-337: replaced ambiguous text-based locators with deterministic
// data-testid + dialog-role scoping after the original spec flaked
// intermittently in CI on PRs unrelated to Meet (the recording subject
// "Customer call (no transcript)" raced the modal's "no transcript" note in
// strict-mode getByText, and the row-scoping ../../ chain ascended past the
// row into the recordings list and matched all three Import buttons).

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

// SCRUM-481 / SCRUM-482: the Create New Session screen no longer renders
// per-platform "From Google Meet" tiles or the inline Google Meet UI this
// test exercises (transcript-state badges, no-transcript modal). The new
// flow opens RecordingsPicker in mode="create" which has its own unit
// tests in web/src/test/RecordingsPicker.test.jsx. Skipped here pending
// SCRUM-482's cleanup PR, which will rewrite this e2e to drive the picker
// (different mock-response shape — unified meeting_uuid/instance_uuid —
// and chained POST /sessions + POST /api/sessions/{id}/import/google-meet).
test.skip('From Google Meet tile renders, recordings list shows transcript-state badges, import modal opens', async ({
  page,
  context,
  request,
}) => {
  const email = uniqueEmail('meet-e2e')
  seededUserId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', 'Meet E2E User')
  await page.goto('/')

  // Open create-session flow. The Create button can be off-screen on first
  // paint; wait deterministically for it before clicking. Some user states
  // open directly into the create flow — fall back to clicking the Meet tile
  // if the create button never becomes visible.
  const createBtn = page.getByRole('button', { name: /Create new session/i }).first()
  await createBtn
    .waitFor({ state: 'visible', timeout: 10_000 })
    .then(() => createBtn.click())
    .catch(() => {})

  // The "From Google Meet" tile should render because /api/google-meet/status
  // is mocked to enabled:true.
  const meetTile = page.getByRole('button', { name: 'From Google Meet', exact: true })
  await expect(meetTile).toBeVisible({ timeout: 10_000 })
  await meetTile.click()

  // Connected pill — workspace_eligible:true → success color, no warning text.
  await expect(page.getByText(/Connected as meet-e2e@workspace\.example/)).toBeVisible()
  await expect(page.getByText(/Workspace Business Standard/)).toHaveCount(0)

  // Disconnect microcopy.
  await expect(page.getByText(/Disconnecting won.t affect sessions/)).toBeVisible()

  // Load recordings.
  await page.getByRole('button', { name: 'Load recordings', exact: true }).click()

  // Three rows; each renders a transcript-state badge.
  const recordingRows = page.getByTestId('google-meet-recording')
  await expect(recordingRows).toHaveCount(3)
  await expect(recordingRows.filter({ hasText: 'Sprint demo (ready transcript)' })).toBeVisible()
  await expect(recordingRows.filter({ hasText: 'Standup (transcript pending)' })).toBeVisible()
  await expect(recordingRows.filter({ hasText: 'Customer call (no transcript)' })).toBeVisible()

  // Each row exposes its badge label exactly once. Scope by row to avoid
  // racing with the import-modal note's "no transcript" copy later in the
  // test — getByTestId('google-meet-recording').filter() bounds the search to
  // the row that owns the badge.
  await expect(
    recordingRows
      .filter({ hasText: 'Sprint demo (ready transcript)' })
      .getByText('Transcript', { exact: true })
  ).toBeVisible()
  await expect(
    recordingRows
      .filter({ hasText: 'Standup (transcript pending)' })
      .getByText('Transcript pending', { exact: true })
  ).toBeVisible()
  await expect(
    recordingRows
      .filter({ hasText: 'Customer call (no transcript)' })
      .getByText('No transcript', { exact: true })
  ).toBeVisible()

  // Open import modal on the no-transcript row by scoping the Import-button
  // lookup to that row's data-testid container — not by ../../ ancestor walk.
  const noTranscriptRow = recordingRows.filter({ hasText: 'Customer call (no transcript)' })
  await noTranscriptRow.getByRole('button', { name: 'Import', exact: true }).click()

  // Modal scopes assertions: role=dialog + aria-labelledby. All
  // modal-internal text lookups must run through `dialog` so the recording
  // subject text in the row outside the modal can't satisfy them.
  const dialog = page.getByRole('dialog', { name: 'Import Google Meet recording' })
  await expect(dialog).toBeVisible()

  // Recording subject and the no-transcript note both render, but the note
  // is unique to the modal — assert by its full sentence so it cannot match
  // the "Customer call (no transcript)" subject text in the row behind.
  await expect(
    dialog.getByText(
      "This recording has no transcript. You'll get video and AI-generated answers about anything visible on screen, but Q&A quality is best when a transcript is available."
    )
  ).toBeVisible()

  // Default session name is the recording subject. Scope the textbox lookup
  // to the dialog so other inputs on the page can't be selected first.
  const nameInput = dialog.getByRole('textbox')
  await expect(nameInput).toHaveValue('Customer call (no transcript)')

  // Empty name disables the Import button (modal-scoped).
  await nameInput.fill('')
  await expect(dialog.getByRole('button', { name: 'Import', exact: true })).toBeDisabled()

  // Cancel closes the modal.
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click()
  await expect(dialog).toBeHidden()
})
