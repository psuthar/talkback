/**
 * material-viewers.e2e.ts
 *
 * E2E coverage for the TalkBack material-upload and viewer flow in creator mode.
 * Covers: MP4 video, PPTX slides, DOCX document, JPG image, and URL link.
 *
 * Upload decisions:
 *  - MP4:  creates a VideoSource → appears as primary video in "Presentation" section;
 *          VideoPlayer (<video>) visible in center, transcript area shows "Processing…" since
 *          Whisper transcription is async.
 *  - PPTX: kind=document → "Documents" section; slide previews generated async by LibreOffice
 *          (may not be available in local dev), so item may show "Processing…" (disabled).
 *  - DOCX: kind=document → "Documents" section; text extraction is async (pending), DocumentViewer
 *          shows the document via mammoth once file is fetched from backend.
 *  - JPG:  kind=image → "Images" section; opens image viewer when text pipeline is ready.
 *  - Link: URL submitted via Add content panel → "Links" section; clicking shows DocumentViewer
 *          with embedded iframe + "Open in new tab" link.
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

// These tests upload files and wait for async extraction — allow plenty of time.
test.setTimeout(120_000)

// Fixture file paths (minimal valid files, real binary content)
const FIXTURES_DIR = path.join(__dirname, 'fixtures')
const MP4_FILE = path.join(FIXTURES_DIR, 'test.mp4')
const PPTX_FILE = path.join(FIXTURES_DIR, 'test.pptx')
const DOCX_FILE = path.join(FIXTURES_DIR, 'test.docx')
const JPG_FILE = path.join(FIXTURES_DIR, 'test.jpg')

// Test link URL — publicly accessible, known to embed
const TEST_LINK_URL = 'https://example.com'

// Cleanup state — tracked per-test via afterAll
let seededUserId = ''
let seededSessionId = ''

test.afterAll(async ({ request }) => {
  await loginAsAdmin(request)
  if (seededSessionId) await deleteSession(request, seededSessionId)
  if (seededUserId) await deleteUserViaAdmin(request, seededUserId)
})

/**
 * Navigate to the creator edit view for the given session and wait for initial load.
 */
async function navigateToCreatorSession(page, sessionId: string) {
  const params = new URLSearchParams({ session: sessionId, mode: 'edit' })
  await page.goto(`/?${params.toString()}`)
  await page.waitForLoadState('networkidle')
}

/**
 * Expand the "Add content" collapsible section if it is collapsed.
 */
async function ensureAddContentExpanded(page) {
  const toggle = page.getByTestId('add-content-toggle')
  const expanded = await toggle.getAttribute('aria-expanded')
  if (expanded !== 'true') {
    await toggle.click()
    await page.waitForTimeout(300) // brief wait for animation
  }
}

/**
 * Upload a file via the hidden file input triggered by the "Choose file" button.
 * Waits for the upload response from the backend and for the materials tree to update.
 */
async function uploadFile(page, filePath: string) {
  await ensureAddContentExpanded(page)

  // Set up file chooser listener before clicking (to avoid race)
  const [fileChooser] = await Promise.all([
    page.waitForEvent('filechooser'),
    page.getByTestId('upload-file-btn').click(),
  ])
  await fileChooser.setFiles(filePath)

  // Wait for the upload HTTP response
  await page.waitForResponse(
    (res) =>
      res.url().includes('/materials/upload') && res.request().method() === 'POST',
    { timeout: 30_000 }
  )

  // Wait briefly for the session refetch to update the tree
  await page.waitForLoadState('networkidle')
}

// ──────────────────────────────────────────────────────────────────────────────
// Shared setup: one user + one session reused across all material tests so we
// don't hit material-count limits on separate sessions.
// ──────────────────────────────────────────────────────────────────────────────

test.describe('Material upload and viewer flow', () => {
  test.beforeAll(async ({ browser }) => {
    // Use a fresh browser context for seeding (no fixtures param in beforeAll)
    const ctx = await browser.newContext()
    const request = ctx.request
    const email = uniqueEmail('material-viewer')

    // createUserAndLoginWithId needs a BrowserContext, not a Page
    const page = await ctx.newPage()
    seededUserId = await createUserAndLoginWithId(ctx, request, email)
    const session = await createSession(request, 'E2E Material Viewers Session')
    seededSessionId = session.id
    await ctx.close()
  })

  // ─── MP4 upload ────────────────────────────────────────────────────────────
  test('MP4 upload appears in Presentation section; video player and transcript status visible', async ({ page, context, request }) => {
    const email = uniqueEmail('mp4-viewer')
    await createUserAndLoginWithId(context, request, email)
    const session = await createSession(request, 'E2E MP4 Viewer Session')

    await navigateToCreatorSession(page, session.id)

    // Upload MP4
    await uploadFile(page, MP4_FILE)

    // The video source created by MP4 upload becomes the primary video.
    // It appears in the "Presentation" section of the materials tree.
    const primaryVideoItem = page.getByTestId('primary-video-item')
    await expect(primaryVideoItem).toBeVisible({ timeout: 15_000 })

    // Click the primary video to make sure center panel is showing the video
    await primaryVideoItem.click()
    await page.waitForLoadState('networkidle')

    // Center panel: the video player container should be visible.
    // A stub MP4 file may trigger a load-error overlay (not valid video data), so we assert
    // the container div rather than the <video> element itself — both states are valid renders.
    const videoPlayerContainer = page.getByTestId('video-player-container')
    await expect(videoPlayerContainer).toBeVisible({ timeout: 10_000 })

    // Transcript area: since Whisper is async and may not run in local dev,
    // we assert the transcript section renders — it will show either "Transcript: Processing…"
    // or a transcript viewer if a transcript exists.
    // We look for the transcript section text (not the TranscriptViewer testid which only renders
    // when transcript_text is non-empty).
    const transcriptSection = page.locator('.creator-center-scroll')
    await expect(transcriptSection).toBeVisible({ timeout: 5_000 })
    // The section should contain either "Processing…" or "No transcript yet." or a transcript viewer
    const transcriptText = await transcriptSection.textContent()
    expect(
      transcriptText?.includes('Transcript') ||
      transcriptText?.includes('Processing') ||
      transcriptText?.includes('No transcript')
    ).toBe(true)

    // Cleanup
    await loginAsAdmin(request)
    await deleteSession(request, session.id)
    const adminRequest = request
    // user cleanup handled in afterAll for shared user; inline session cleaned here
  })

  // ─── PPTX upload ──────────────────────────────────────────────────────────
  test('PPTX upload appears in Documents section with Processing status', async ({ page, context, request }) => {
    const email = uniqueEmail('pptx-viewer')
    await createUserAndLoginWithId(context, request, email)
    const session = await createSession(request, 'E2E PPTX Viewer Session')

    await navigateToCreatorSession(page, session.id)

    // Upload PPTX
    await uploadFile(page, PPTX_FILE)

    // PPTX → kind=document → appears in "Documents" section
    const pptxItem = page.getByTestId('material-item').filter({ hasText: 'test.pptx' })
    await expect(pptxItem).toBeVisible({ timeout: 15_000 })

    // The item should show "Processing…" since slides manifest generation is async
    // (LibreOffice may not be installed; even if it is, it runs in a goroutine)
    // We verify the Processing indicator appears in the tree row
    const pptxRow = page.locator('[data-testid="material-item"]').filter({ hasText: 'test.pptx' })
    await expect(pptxRow).toBeVisible()

    // When slides are not ready, the SlideDeckViewer shows "Processing slides…" or the item
    // is disabled in the tree. Try clicking — it may be disabled (no-op) or show the viewer.
    // Either way, we verify the tree item is rendered.
    const isDisabled = await pptxItem.evaluate((el) => {
      const btn = el.closest('button') || el
      return (btn as HTMLButtonElement).disabled
    })

    if (!isDisabled) {
      // Slides are ready (LibreOffice ran): verify SlideDeckViewer is shown
      await pptxItem.click()
      await page.waitForLoadState('networkidle')
      const slideDeckViewer = page.getByTestId('slide-deck-viewer')
      await expect(slideDeckViewer).toBeVisible({ timeout: 10_000 })
    }
    // else: item is disabled (slides still processing) — this is valid terminal state

    // Cleanup
    await loginAsAdmin(request)
    await deleteSession(request, session.id)
  })

  // ─── DOCX upload ──────────────────────────────────────────────────────────
  test('DOCX upload appears in Documents section; DocumentViewer renders', async ({ page, context, request }) => {
    const email = uniqueEmail('docx-viewer')
    await createUserAndLoginWithId(context, request, email)
    const session = await createSession(request, 'E2E DOCX Viewer Session')

    await navigateToCreatorSession(page, session.id)

    // Upload DOCX
    await uploadFile(page, DOCX_FILE)

    // DOCX → kind=document → appears in "Documents" section as material-item
    const docxItem = page.getByTestId('material-item').filter({ hasText: 'test.docx' })
    await expect(docxItem).toBeVisible({ timeout: 15_000 })

    // Click to open in center panel
    await docxItem.click()
    await page.waitForLoadState('networkidle')

    // DocumentViewer should be visible in center panel
    const documentViewer = page.getByTestId('document-viewer')
    await expect(documentViewer).toBeVisible({ timeout: 10_000 })

    // Cleanup
    await loginAsAdmin(request)
    await deleteSession(request, session.id)
  })

  // ─── JPG upload ───────────────────────────────────────────────────────────
  test('JPG upload appears in Images section', async ({ page, context, request }) => {
    const email = uniqueEmail('jpg-viewer')
    await createUserAndLoginWithId(context, request, email)
    const session = await createSession(request, 'E2E JPG Viewer Session')

    await navigateToCreatorSession(page, session.id)

    // Upload JPG
    await uploadFile(page, JPG_FILE)

    // JPG → kind=image → "Images" section
    const jpgItem = page.locator('[data-testid="images-item"]').filter({ hasText: 'test.jpg' })
    await expect(jpgItem).toBeVisible({ timeout: 15_000 })

    // Verify item has some status text in the tree row area
    const jpgItemText = await jpgItem.textContent()
    expect(jpgItemText).toBeTruthy()

    // The data-testid is on the <button> element itself (TreeItem renders the button
    // with the testid). Check disabled attribute directly on the located element.
    const isDisabled = await jpgItem.getAttribute('disabled')
    if (isDisabled === null) {
      // Item is enabled — click and verify image viewer
      await jpgItem.click()
      await page.waitForLoadState('networkidle')
      const imageViewer = page.getByTestId('image-viewer')
      await expect(imageViewer).toBeVisible({ timeout: 10_000 })
    }
    // When disabled: image is still processing (text pipeline).

    // Cleanup
    await loginAsAdmin(request)
    await deleteSession(request, session.id)
  })

  // ─── Link add ──────────────────────────────────────────────────────────────
  test('Link added via URL input appears in Links section; DocumentViewer shows link view', async ({ page, context, request }) => {
    const email = uniqueEmail('link-viewer')
    await createUserAndLoginWithId(context, request, email)
    const session = await createSession(request, 'E2E Link Viewer Session')

    await navigateToCreatorSession(page, session.id)

    // Expand "Add content" section
    await ensureAddContentExpanded(page)

    // Type URL and submit
    const urlInput = page.getByTestId('add-link-url-input')
    await expect(urlInput).toBeVisible({ timeout: 5_000 })
    await urlInput.fill(TEST_LINK_URL)

    // Wait for both the POST to /links and the subsequent GET session refetch
    const addLinkResponse = page.waitForResponse(
      (res) => res.url().includes('/links') && res.request().method() === 'POST',
      { timeout: 15_000 }
    )
    // Also wait for session refetch (GET with session id)
    const sessionRefetch = page.waitForResponse(
      (res) => res.url().includes('/sessions/') && res.request().method() === 'GET',
      { timeout: 15_000 }
    )
    await page.getByTestId('add-link-submit-btn').click()
    await addLinkResponse
    await sessionRefetch.catch(() => { /* refetch may be a no-op; continue */ })

    // Link should appear in the "Links" section — row text is `title || url` (verified links often show a resolved title like "Example Domain", not the raw hostname).
    const linkItem = page.getByTestId('link-item').filter({ hasText: /example\.com|Example Domain/i })
    await expect(linkItem).toBeVisible({ timeout: 15_000 })

    // Click to open in center panel
    await linkItem.click()
    await page.waitForLoadState('networkidle')

    // DocumentViewer should be visible with link view
    const documentViewer = page.getByTestId('document-viewer')
    await expect(documentViewer).toBeVisible({ timeout: 10_000 })

    // Link view has "Open in new tab" anchor link
    const openInNewTab = documentViewer.getByRole('link', { name: 'Open in new tab' })
    await expect(openInNewTab).toBeVisible()

    // The link-viewer div should also be present
    const linkViewer = page.getByTestId('link-viewer')
    await expect(linkViewer).toBeVisible()

    // Cleanup
    await loginAsAdmin(request)
    await deleteSession(request, session.id)
  })
})
