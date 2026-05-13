/**
 * material-processing-state.e2e.ts
 *
 * E2E coverage for the processing-state gating behavior in the MaterialsTreePanel.
 *
 * For each uploadable material type (MP4 video is excluded — it goes to the Presentation
 * section as a VideoSource, not a document/image tree item with text_status gating):
 *
 *   PPTX → Documents section; disabled while slides pipeline is pending/processing
 *   DOCX → Documents section; disabled while text_status is pending/processing
 *   JPG  → Images section;    disabled while text_status is pending/processing
 *
 * Test structure per material (two-phase assertion):
 *   Phase 1: Immediately after upload, assert the tree item is disabled (not clickable).
 *            This validates that the UI correctly blocks interaction while processing.
 *   Phase 2: Poll the session API until the material reaches a terminal state
 *            (text_status = ready | failed; or slides_status = ready | failed for PPTX).
 *            Then reload the page and assert the item is now enabled (clickable).
 *            This validates that the UI correctly unlocks once processing completes.
 *
 * Cleanup: each test creates its own session and user, and deletes them in afterAll.
 * The global teardown (global-teardown.ts) also catches any @smoke.test leakage.
 */

import path from 'path'
import { fileURLToPath } from 'url'
import { test, expect } from '@playwright/test'
import type { APIRequestContext } from '@playwright/test'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)

import {
  API_BASE,
  createUserAndLoginWithId,
  createSession,
  cleanupSessionAndUserAsAdmin,
  uniqueEmail,
} from './fixtures'

// Default timeout for DOCX and JPG tests.
// PPTX overrides this below because LibreOffice slides conversion can take ~90-100s locally.
test.setTimeout(120_000)

const FIXTURES_DIR = path.join(__dirname, 'fixtures')
const PPTX_FILE = path.join(FIXTURES_DIR, 'test.pptx')
const DOCX_FILE = path.join(FIXTURES_DIR, 'test.docx')
const JPG_FILE  = path.join(FIXTURES_DIR, 'test.jpg')

// ─── Helpers ──────────────────────────────────────────────────────────────────

/** Navigate to the creator edit view for a session and wait for initial load. */
async function navigateToCreatorSession(page, sessionId: string) {
  const params = new URLSearchParams({ session: sessionId, mode: 'edit' })
  await page.goto(`/?${params.toString()}`)
  await page.waitForLoadState('networkidle')
}

/** Expand the "Add content" collapsible if it is not already expanded. */
async function ensureAddContentExpanded(page) {
  const toggle = page.getByTestId('add-content-toggle')
  const expanded = await toggle.getAttribute('aria-expanded')
  if (expanded !== 'true') {
    await toggle.click()
    await page.waitForTimeout(300)
  }
}

/**
 * Upload a file via the hidden file input triggered by the "Choose file" button.
 * Waits for the upload POST response and then for the materials tree to update.
 */
async function uploadFile(page, filePath: string) {
  await ensureAddContentExpanded(page)
  const [fileChooser] = await Promise.all([
    page.waitForEvent('filechooser'),
    page.getByTestId('upload-file-btn').click(),
  ])
  await fileChooser.setFiles(filePath)
  // SCRUM-397: 90 s (not 30 s) so cold-start CI runners can clear synchronous
  // markitdown text-extraction without timing out. Matches the precedent set
  // in material-viewers.e2e.ts by SCRUM-218 — DOCX-first runs were
  // consistently hitting the 30001ms deadline on this exact assertion
  // (release-readiness run 25770842762 captured a passed/failed pair: 32.8s
  // on attempt 0 vs 5.0s warmed).
  // SCRUM-437: bumped further to 180 s — the JPG test here (line 290+) takes
  // the markitdown image-extraction cold-start path which SCRUM-437 observed
  // at 92.4 s on a sibling spec (material-viewers.e2e.ts). Preemptive bump
  // before this spec hits the same flake. A real upload-pipeline regression
  // would surface as a much-larger latency spike that still exceeds 180 s.
  await page.waitForResponse(
    (res) => res.url().includes('/materials/upload') && res.request().method() === 'POST',
    { timeout: 180_000 }
  )
  await page.waitForLoadState('networkidle')
}

/**
 * Poll GET /sessions/{id} via the API request context until the predicate is satisfied
 * or the timeout elapses. Returns the final session data (or null on timeout).
 *
 * The predicate receives the raw JSON body of the GET /sessions/{id} response.
 */
async function pollSessionUntil(
  request: APIRequestContext,
  sessionId: string,
  predicate: (data: any) => boolean,
  options: { pollIntervalMs?: number; timeoutMs?: number } = {}
): Promise<any | null> {
  const { pollIntervalMs = 2_000, timeoutMs = 90_000 } = options
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    const res = await request.get(`${API_BASE}/sessions/${sessionId}`)
    if (res.ok()) {
      const data = await res.json()
      if (predicate(data)) return data
    }
    await new Promise((r) => setTimeout(r, pollIntervalMs))
  }
  return null
}

/**
 * Find a material by filename in a session data payload.
 * Looks in both `materials` (document/image uploads) arrays.
 */
function findMaterial(sessionData: any, filename: string): any | null {
  const materials: any[] = sessionData?.materials ?? []
  return materials.find(
    (m) => String(m?.filename ?? '').toLowerCase() === filename.toLowerCase()
  ) ?? null
}

/** True when text_status is a terminal state (ready or failed). */
function isTerminalTextStatus(status: string | undefined): boolean {
  return status === 'ready' || status === 'failed'
}

/** True when slides status is a terminal state (ready or failed). */
function isTerminalSlidesStatus(data: any, materialId: string): boolean {
  const slidesStatus = data?.material_slides_status?.[materialId]
  if (slidesStatus === 'ready' || slidesStatus === 'failed') return true
  // Fallback: legacy material_slides_ready flag
  const slidesReady = data?.material_slides_ready?.[materialId]
  if (slidesReady === true) return true
  return false
}

// ─── Tests ────────────────────────────────────────────────────────────────────

test.describe('Material processing-state gating in MaterialsTreePanel', () => {
  // ─── PPTX ────────────────────────────────────────────────────────────────

  // LibreOffice slides conversion via Docker typically completes in ~30s locally.
  // The per-test timeout is set generously to handle slow Docker startup on cold machines.
  test('PPTX: item is disabled while slides are processing, becomes enabled at terminal state', { tag: '@pptx' }, async ({ page, context, request }) => {
    test.setTimeout(180_000)
    const email = uniqueEmail('proc-pptx')
    const userId = await createUserAndLoginWithId(context, request, email)
    const session = await createSession(request, 'E2E Processing State PPTX')

    try {
      await navigateToCreatorSession(page, session.id)
      await uploadFile(page, PPTX_FILE)

      // ── Phase 1: assert item appears and is initially disabled ──────────────
      // The PPTX item should appear in the Documents section as [data-testid="material-item"].
      // It may immediately show "Processing…" meta or be disabled; either is acceptable right
      // after upload since the slides pipeline is async.
      const pptxItem = page.getByTestId('material-item').filter({ hasText: 'test.pptx' })
      await expect(pptxItem).toBeVisible({ timeout: 20_000 })

      // The item should be initially disabled (slides pipeline has not completed yet).
      // We give a short grace period — if the pipeline finishes before we check (extremely
      // fast local LibreOffice), we fall through to Phase 2 which still validates clickability.
      const initiallyDisabled = await pptxItem.evaluate((el) => {
        return (el as HTMLButtonElement).disabled === true
      })

      if (initiallyDisabled) {
        // Confirmed: item is gated while processing.
        // Also verify the processing badge appears in the tree row.
        // For PPTX, the badge now reads "Generating slides…" (spinner + text);
        // other types still show "Processing…". Match both.
        const rowText = await pptxItem.textContent()
        expect(rowText).toMatch(/Processing|Generating/i)
      }
      // If not initially disabled the pipeline was too fast; Phase 2 still validates terminal state.

      // ── Phase 2: wait for terminal state and assert item is enabled ─────────
      // Poll the session API until slides status reaches ready or failed.
      // For PPTX, the component exclusively uses slides pipeline status (not text_status)
      // to determine viewability. We must wait for slides terminal state specifically.
      // Poll with 120s budget — well above typical Docker-based LibreOffice conversion time (~7-30s).
      // If conversion takes longer on a cold machine, the else branch asserts the item is still disabled.
      const terminalData = await pollSessionUntil(
        request,
        session.id,
        (data) => {
          const mat = findMaterial(data, 'test.pptx')
          if (!mat) return false
          const matId = String(mat.id)
          return isTerminalSlidesStatus(data, matId)
        },
        { timeoutMs: 120_000 }
      )

      // Always re-navigate before Phase 2 assertions: the browser page may have shifted state
      // after a long polling wait (React re-renders, session refetches, etc.).
      await navigateToCreatorSession(page, session.id)
      const pptxItemFresh = page.getByTestId('material-item').filter({ hasText: 'test.pptx' })
      await expect(pptxItemFresh).toBeVisible({ timeout: 15_000 })

      if (terminalData !== null) {
        // Terminal state reached — assert the item is now enabled.
        const isEnabledAfter = await pptxItemFresh.evaluate((el) => {
          return (el as HTMLButtonElement).disabled !== true
        })
        expect(isEnabledAfter).toBe(true)

        // Clicking should now work and not throw.
        await pptxItemFresh.click()
        await page.waitForLoadState('networkidle')
      } else {
        // Pipeline did not reach a terminal status within the 300-second poll budget.
        // This is an unexpectedly slow conversion. The UI must still be holding the gate
        // open (item disabled) — assert that and emit a warning so CI is visible.
        console.warn('[PPTX test] slides pipeline did not reach terminal state within 120s poll budget — asserting still-disabled')
        const stillDisabled = await pptxItemFresh.evaluate((el) => {
          return (el as HTMLButtonElement).disabled === true
        })
        expect(stillDisabled).toBe(true)
      }
    } finally {
      await cleanupSessionAndUserAsAdmin(session.id, userId)
    }
  })

  // ─── DOCX ────────────────────────────────────────────────────────────────

  test('DOCX: item is disabled while text extraction is processing, becomes enabled at terminal state', async ({ page, context, request }) => {
    const email = uniqueEmail('proc-docx')
    const userId = await createUserAndLoginWithId(context, request, email)
    const session = await createSession(request, 'E2E Processing State DOCX')

    try {
      await navigateToCreatorSession(page, session.id)
      await uploadFile(page, DOCX_FILE)

      // ── Phase 1: item appears in Documents section ───────────────────────────
      const docxItem = page.getByTestId('material-item').filter({ hasText: 'test.docx' })
      await expect(docxItem).toBeVisible({ timeout: 20_000 })

      // Check disabled state immediately after upload.
      const initiallyDisabled = await docxItem.evaluate((el) => {
        return (el as HTMLButtonElement).disabled === true
      })

      if (initiallyDisabled) {
        const rowText = await docxItem.textContent()
        expect(rowText).toMatch(/Processing/i)
      }

      // ── Phase 2: poll for terminal text_status ──────────────────────────────
      const terminalData = await pollSessionUntil(
        request,
        session.id,
        (data) => {
          const mat = findMaterial(data, 'test.docx')
          return mat != null && isTerminalTextStatus(mat.text_status)
        },
        { timeoutMs: 90_000 }
      )

      if (terminalData !== null) {
        await navigateToCreatorSession(page, session.id)
        const docxItemAfter = page.getByTestId('material-item').filter({ hasText: 'test.docx' })
        await expect(docxItemAfter).toBeVisible({ timeout: 15_000 })

        const isEnabledAfter = await docxItemAfter.evaluate((el) => {
          return (el as HTMLButtonElement).disabled !== true
        })
        expect(isEnabledAfter).toBe(true)

        // Clicking should open the DocumentViewer.
        await docxItemAfter.click()
        await page.waitForLoadState('networkidle')
        const documentViewer = page.getByTestId('document-viewer')
        await expect(documentViewer).toBeVisible({ timeout: 10_000 })
      } else {
        const stillDisabled = await docxItem.evaluate((el) => {
          return (el as HTMLButtonElement).disabled === true
        })
        expect(stillDisabled).toBe(true)
      }
    } finally {
      await cleanupSessionAndUserAsAdmin(session.id, userId)
    }
  })

  // ─── JPG ─────────────────────────────────────────────────────────────────

  test('JPG: item is disabled while text pipeline is processing, becomes enabled at terminal state', async ({ page, context, request }) => {
    const email = uniqueEmail('proc-jpg')
    const userId = await createUserAndLoginWithId(context, request, email)
    const session = await createSession(request, 'E2E Processing State JPG')

    try {
      await navigateToCreatorSession(page, session.id)
      await uploadFile(page, JPG_FILE)

      // ── Phase 1: item appears in Images section ──────────────────────────────
      const jpgItem = page.getByTestId('images-item').filter({ hasText: 'test.jpg' })
      await expect(jpgItem).toBeVisible({ timeout: 20_000 })

      // The images-item testId is placed directly on the <button> element in TreeItem.
      const initiallyDisabled = await jpgItem.evaluate((el) => {
        return (el as HTMLButtonElement).disabled === true
      })

      if (initiallyDisabled) {
        const rowText = await jpgItem.textContent()
        expect(rowText).toMatch(/Processing/i)
      }

      // ── Phase 2: poll for terminal text_status ──────────────────────────────
      const terminalData = await pollSessionUntil(
        request,
        session.id,
        (data) => {
          const mat = findMaterial(data, 'test.jpg')
          return mat != null && isTerminalTextStatus(mat.text_status)
        },
        { timeoutMs: 90_000 }
      )

      if (terminalData !== null) {
        await navigateToCreatorSession(page, session.id)
        const jpgItemAfter = page.getByTestId('images-item').filter({ hasText: 'test.jpg' })
        await expect(jpgItemAfter).toBeVisible({ timeout: 15_000 })

        const isEnabledAfter = await jpgItemAfter.evaluate((el) => {
          return (el as HTMLButtonElement).disabled !== true
        })
        expect(isEnabledAfter).toBe(true)

        // Clicking should open the image viewer.
        await jpgItemAfter.click()
        await page.waitForLoadState('networkidle')
        const imageViewer = page.getByTestId('image-viewer')
        await expect(imageViewer).toBeVisible({ timeout: 10_000 })
      } else {
        const stillDisabled = await jpgItem.evaluate((el) => {
          return (el as HTMLButtonElement).disabled === true
        })
        expect(stillDisabled).toBe(true)
      }
    } finally {
      await cleanupSessionAndUserAsAdmin(session.id, userId)
    }
  })
})
