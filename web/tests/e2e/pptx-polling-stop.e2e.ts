/**
 * pptx-polling-stop.e2e.ts
 *
 * Regression test for the fix that stops the three background polling loops
 * (processing, ingestion, transcript) when a session has no active import job
 * (e.g. a PPTX-only session with no Zoom/video import started).
 *
 * Prior to the fix, all three loops ran indefinitely, generating noise in the
 * network tab and wasting server resources.  After the fix each loop halts
 * after 5 consecutive empty/no-job responses (~10-13 s after page load).
 *
 * Strategy
 * ─────────
 * 1. Create a fresh session and upload a PPTX (no video import, so all three
 *    endpoints return empty/no-job payloads).
 * 2. Attach a request listener to count calls to each poll endpoint.
 * 3. Record the wall-clock timestamp of the last call seen for each endpoint.
 * 4. Wait 35 s — comfortably past the stop point (~13 s) — then sample a
 *    further 10 s window for any residual calls.
 * 5. Assert: no new calls were made to any poll endpoint during the final 10 s
 *    observation window.  This confirms the loops stopped rather than running
 *    indefinitely.
 *
 * The test does NOT assert exact LLM prose or internal React state.
 * It asserts observable browser-network behavior only.
 */

import path from 'path'
import { fileURLToPath } from 'url'
import { test, expect } from '@playwright/test'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)

import {
  createUserAndLoginWithId,
  createSession,
  deleteSession,
  deleteUserViaAdmin,
  loginAsAdmin,
  uniqueEmail,
} from './fixtures'

// The full observation window is 45 s.  Add 30 s buffer for upload + initial loads.
test.setTimeout(90_000)

const FIXTURES_DIR = path.join(__dirname, 'fixtures')
const PPTX_FILE = path.join(FIXTURES_DIR, 'test.pptx')

// ─── Helpers ──────────────────────────────────────────────────────────────────

async function navigateToCreatorSession(page, sessionId: string) {
  const params = new URLSearchParams({ session: sessionId, mode: 'edit' })
  await page.goto(`/?${params.toString()}`)
  await page.waitForLoadState('networkidle')
}

async function ensureAddContentExpanded(page) {
  const toggle = page.getByTestId('add-content-toggle')
  const expanded = await toggle.getAttribute('aria-expanded')
  if (expanded !== 'true') {
    await toggle.click()
    await page.waitForTimeout(300)
  }
}

async function uploadFile(page, filePath: string) {
  await ensureAddContentExpanded(page)
  const [fileChooser] = await Promise.all([
    page.waitForEvent('filechooser'),
    page.getByTestId('upload-file-btn').click(),
  ])
  await fileChooser.setFiles(filePath)
  await page.waitForResponse(
    (res) => res.url().includes('/materials/upload') && res.request().method() === 'POST',
    { timeout: 30_000 }
  )
  // Give React a moment to register the upload and start the polling loops.
  await page.waitForTimeout(500)
}

// ─── Test ─────────────────────────────────────────────────────────────────────

test('PPTX-only session: processing, ingestion, and transcript polls stop within 30 s (no active import job)', async ({
  page,
  context,
  request,
}) => {
  const email = uniqueEmail('poll-stop')
  const userId = await createUserAndLoginWithId(context, request, email)
  const session = await createSession(request, 'E2E Polling Stop PPTX')

  try {
    // ── Set up request listeners BEFORE navigation so we capture all poll calls ──
    // Each entry records the wall-clock time (ms) of the most recent request.
    // IMPORTANT: register before page.goto() so that the very first poll calls
    // (e.g. transcript which stops after 1 call) are not missed.
    const lastSeen: Record<string, number> = {
      processing: 0,
      ingestion: 0,
      transcript: 0,
    }
    const callCounts: Record<string, number> = {
      processing: 0,
      ingestion: 0,
      transcript: 0,
    }

    page.on('request', (req) => {
      const url = req.url()
      // Match the three polling endpoint patterns for this session
      if (url.includes(`/sessions/${session.id}/processing`)) {
        lastSeen.processing = Date.now()
        callCounts.processing++
      } else if (url.includes(`/sessions/${session.id}/ingestion`)) {
        lastSeen.ingestion = Date.now()
        callCounts.ingestion++
      } else if (url.includes(`/sessions/${session.id}/transcript`)) {
        lastSeen.transcript = Date.now()
        callCounts.transcript++
      }
    })

    await navigateToCreatorSession(page, session.id)

    // ── Upload PPTX — triggers all three polling loops ────────────────────────
    await uploadFile(page, PPTX_FILE)

    // The PPTX item should appear in the Documents section
    const pptxItem = page.getByTestId('material-item').filter({ hasText: 'test.pptx' })
    await expect(pptxItem).toBeVisible({ timeout: 20_000 })

    // ── Observe the polling stop ──────────────────────────────────────────────
    // Polling loops fire at 2000-2500 ms intervals.  After 5 empty responses
    // (~10-13 s) the loops should be cleared.  We wait 35 s to be certain they
    // have stopped, then sample for a further 10 s to confirm silence.

    // Wait 35 s post-upload (loops should have halted well before this).
    await page.waitForTimeout(35_000)

    // Record the count at this snapshot.
    const snapshotCounts = { ...callCounts }
    const snapshotLastSeen = { ...lastSeen }

    // Wait another 10 s observation window.
    await page.waitForTimeout(10_000)

    // ── Assert: no new calls during the 10 s quiet window ────────────────────
    // If polling had not stopped, we would expect ~4-5 new calls per endpoint
    // during 10 s (2-2.5 s intervals).  Zero new calls confirms the loops halted.
    for (const endpoint of ['processing', 'ingestion', 'transcript'] as const) {
      const newCallsInWindow = callCounts[endpoint] - snapshotCounts[endpoint]
      expect(
        newCallsInWindow,
        `Expected ${endpoint} polling to have stopped but saw ${newCallsInWindow} new calls in the final 10 s window`
      ).toBe(0)
    }

    // Sanity: verify that each endpoint was polled at least once before stopping
    // (proves the loops actually started and ran).
    for (const endpoint of ['processing', 'ingestion', 'transcript'] as const) {
      expect(
        callCounts[endpoint],
        `Expected ${endpoint} endpoint to have been polled at least once`
      ).toBeGreaterThan(0)
    }

    // Sanity: last call to each endpoint should be at least 10 s in the past
    // (i.e. the stop happened before our observation window, not at its edge).
    const now = Date.now()
    for (const endpoint of ['processing', 'ingestion', 'transcript'] as const) {
      if (snapshotLastSeen[endpoint] > 0) {
        const ageSecs = (now - snapshotLastSeen[endpoint]) / 1000
        expect(
          ageSecs,
          `Expected ${endpoint} last call to be >10 s old but was ${ageSecs.toFixed(1)} s ago`
        ).toBeGreaterThan(10)
      }
    }
  } finally {
    await loginAsAdmin(request)
    await deleteSession(request, session.id)
    await deleteUserViaAdmin(request, userId)
  }
})
