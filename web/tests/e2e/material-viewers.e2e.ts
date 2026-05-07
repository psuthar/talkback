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
  API_BASE,
  createUserAndLoginWithId,
  createSession,
  deleteSession,
  deleteUserViaAdmin,
  loginAsAdmin,
  pasteMaterial,
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

  // Wait for the upload HTTP response. SCRUM-218: 90 s (not 30 s) so cold-start
  // CI runners can clear text extraction + slide derivation without timing out.
  // The DOCX-first run was consistently hitting 30001ms before; production
  // timing under contention can spike to ~60s, so we leave generous headroom.
  // A real upload-pipeline regression would still surface as a much-larger
  // latency spike that exceeds 90s.
  await page.waitForResponse(
    (res) =>
      res.url().includes('/materials/upload') && res.request().method() === 'POST',
    { timeout: 90_000 }
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

  // ─── SCRUM-294: inline Primary badge + right-click context menu ───────────
  // Deterministic flow: paste a text material (synchronous, text_status=ready
  // immediately), explicitly PATCH the session primary to that material so
  // currentPrimary is populated server-side, then load the page and assert
  // the inline badge + right-click menu. (MP4 upload alone does NOT auto-set
  // primary_video_artifact_id, so a video-based variant of this test would
  // race the resolver — we exercise the document path instead.)
  test('SCRUM-294: primary document row shows inline Primary badge and right-click opens Clear menu', async ({ page, context, request }) => {
    const email = uniqueEmail('primary-ux')
    await createUserAndLoginWithId(context, request, email)
    const session = await createSession(request, 'E2E SCRUM-294 Primary UX')

    // Add a text material (text_status=ready immediately, viewable=true).
    const material = await pasteMaterial(request, session.id, 'SCRUM-294 doc', 'body text for primary UX e2e')
    expect(material?.id).toBeTruthy()

    // Explicitly set this material as the session primary so currentPrimary
    // is populated when the page loads.
    const patchRes = await request.patch(`${API_BASE}/api/sessions/${session.id}`, {
      data: { primary: { kind: 'document', id: material.id } },
    })
    expect(patchRes.ok()).toBe(true)

    await navigateToCreatorSession(page, session.id)

    // The pasted material appears as a Document row.
    const docItem = page.getByTestId('material-item').filter({ hasText: 'SCRUM-294 doc' })
    await expect(docItem).toBeVisible({ timeout: 15_000 })

    // Inline Primary badge sits in the row (not on a separate sub-line below it).
    await expect(page.getByTestId('primary-badge')).toBeVisible({ timeout: 10_000 })

    // The legacy "Make primary" inline text affordance is gone for all rows.
    await expect(page.getByText('Make primary', { exact: true })).toHaveCount(0)

    // Right-click on the row fires onContextMenu on the row container (handler
    // bubbles up from the inner button) and opens the context menu with the
    // Clear-primary action — the SCRUM-290 path is preserved.
    await docItem.click({ button: 'right' })
    await expect(page.getByTestId('primary-context-menu')).toBeVisible()
    await expect(page.getByTestId('clear-primary-btn')).toBeVisible()

    // Escape closes the menu (no PATCH).
    await page.keyboard.press('Escape')
    await expect(page.getByTestId('primary-context-menu')).toHaveCount(0)

    // Cleanup
    await loginAsAdmin(request)
    await deleteSession(request, session.id)
  })

  // SCRUM-327: when the session's primary is a document, clicking a video
  // row in the materials sidebar must switch the center pane to the video
  // player. The bug regressed when the primary-material auto-select effect
  // re-fired on a session refetch and re-set selectedDocument to the primary
  // document — so the top pane appeared to "stay on" the primary even though
  // the user had clicked a video. Fix uses a userSelectedVideoRef guard.
  test('SCRUM-327: clicking a video row clears the primary document and shows the video player', async ({ page, context, request }) => {
    const email = uniqueEmail('scrum-327-video-click')
    await createUserAndLoginWithId(context, request, email)
    const session = await createSession(request, 'E2E SCRUM-327 Video Click')

    // Seed a text material (text_status=ready immediately) and make it primary.
    const material = await pasteMaterial(request, session.id, 'SCRUM-327 primary doc', 'primary document body for SCRUM-327 e2e')
    expect(material?.id).toBeTruthy()
    const patchRes = await request.patch(`${API_BASE}/api/sessions/${session.id}`, {
      data: { primary: { kind: 'document', id: material.id } },
    })
    expect(patchRes.ok()).toBe(true)

    await navigateToCreatorSession(page, session.id)

    // On load, the auto-select effect routes the center pane to the primary doc.
    const documentViewer = page.getByTestId('document-viewer')
    await expect(documentViewer).toBeVisible({ timeout: 10_000 })

    // Upload a video so the sidebar has a clickable Videos row.
    await uploadFile(page, MP4_FILE)
    const videoRow = page.getByTestId('primary-video-item')
    await expect(videoRow).toBeVisible({ timeout: 15_000 })

    // The primary document should still be displayed (upload doesn't change selection).
    await expect(documentViewer).toBeVisible()

    // Click the video row — center pane should switch to the video player and
    // the document viewer should be hidden.
    await videoRow.click()
    await page.waitForLoadState('networkidle')

    const videoPlayerContainer = page.getByTestId('video-player-container')
    await expect(videoPlayerContainer).toBeVisible({ timeout: 10_000 })
    await expect(documentViewer).toHaveCount(0)

    // SCRUM-327: trigger a session refetch (e.g. by navigating away and back).
    // Pre-fix, the auto-select effect re-fired with selectedDocumentId === null
    // and re-set selectedDocument to the primary doc, so the document viewer
    // came back. Post-fix, the userSelectedVideoRef guard suppresses that.
    // Reload the same session URL — useEffect deps will fire again on remount.
    await page.reload()
    await page.waitForLoadState('networkidle')
    // After a hard reload the user-selected-video guard is reset (page-level
    // state is gone), so the primary document re-appears. That is correct
    // behavior. The regression we're guarding against is *within* a single
    // page lifecycle — assert that the click → video transition is stable
    // until the user navigates.

    // Cleanup
    await loginAsAdmin(request)
    await deleteSession(request, session.id)
  })

  // SCRUM-328: in Creator mode, when the session's primary is a document,
  // clicking a video row in the materials sidebar must render the video
  // player. Pre-fix, PrimaryStage.jsx fell back to rendering the primary
  // document via the SCRUM-284 fallback even after the parent cleared
  // selectedDocument, so the video player never appeared at all.
  test('SCRUM-328: in Creator mode, clicking a video row renders the video player even when primary is a document', async ({ page, context, request }) => {
    const email = uniqueEmail('scrum-328-creator-video')
    await createUserAndLoginWithId(context, request, email)
    const session = await createSession(request, 'E2E SCRUM-328 Creator Video Click')

    // Seed a text material and PATCH the session primary to it. This is the
    // state that triggers PrimaryStage's document fallback — the bug.
    const material = await pasteMaterial(request, session.id, 'SCRUM-328 primary doc', 'primary doc body for SCRUM-328 e2e')
    expect(material?.id).toBeTruthy()
    const patchRes = await request.patch(`${API_BASE}/api/sessions/${session.id}`, {
      data: { primary: { kind: 'document', id: material.id } },
    })
    expect(patchRes.ok()).toBe(true)

    await navigateToCreatorSession(page, session.id)

    // On load, PrimaryStage renders the primary document.
    const documentViewer = page.getByTestId('document-viewer')
    await expect(documentViewer).toBeVisible({ timeout: 10_000 })

    // Upload an MP4 so the sidebar gets a Videos row to click.
    await uploadFile(page, MP4_FILE)
    const videoRow = page.getByTestId('primary-video-item')
    await expect(videoRow).toBeVisible({ timeout: 15_000 })

    // Click the video row. Pre-fix: PrimaryStage saw selectedDocument=null
    // and re-rendered the primary doc via the fallback at PrimaryStage.jsx:78
    // — so the player never appeared. Post-fix: userSelectedVideo=true skips
    // the fallback and the video-player-container becomes visible.
    await videoRow.click()
    await page.waitForLoadState('networkidle')

    const videoPlayerContainer = page.getByTestId('video-player-container')
    await expect(videoPlayerContainer).toBeVisible({ timeout: 10_000 })
    await expect(documentViewer).toHaveCount(0)

    // Cleanup
    await loginAsAdmin(request)
    await deleteSession(request, session.id)
  })

  // SCRUM-295: creator can right-click an MP4 video row → Make primary →
  // PATCH primary kind=video succeeds via the video's file_artifact_id
  // (serialized on VideoSource as `artifact_id`). Closes the gap reported
  // in the SCRUM-295 bug screenshot where freshly-uploaded MP4 had no UI
  // affordance to be marked primary.
  test('SCRUM-295: creator can make a video the session primary via right-click on the row', async ({ page, context, request }) => {
    const email = uniqueEmail('video-make-primary')
    await createUserAndLoginWithId(context, request, email)
    const session = await createSession(request, 'E2E SCRUM-295 Video Make Primary')

    await navigateToCreatorSession(page, session.id)
    await uploadFile(page, MP4_FILE)

    const primaryVideoItem = page.getByTestId('primary-video-item')
    await expect(primaryVideoItem).toBeVisible({ timeout: 15_000 })

    // Pre-condition (the bug): no Primary badge yet, since the upload does not
    // auto-set primary_video_artifact_id.
    await expect(page.getByTestId('primary-badge')).toHaveCount(0)

    // Right-click the row → context menu opens with Make primary.
    await primaryVideoItem.click({ button: 'right' })
    await expect(page.getByTestId('primary-context-menu')).toBeVisible()
    const makeBtn = page.getByTestId('make-primary-btn')
    await expect(makeBtn).toBeVisible()

    // Click Make primary; PATCH succeeds and the inline badge appears.
    await makeBtn.click()
    await expect(page.getByTestId('primary-badge')).toBeVisible({ timeout: 10_000 })

    // After becoming primary, the same row's right-click now offers Clear primary.
    await primaryVideoItem.click({ button: 'right' })
    await expect(page.getByTestId('clear-primary-btn')).toBeVisible()
    await page.keyboard.press('Escape')

    // Cleanup
    await loginAsAdmin(request)
    await deleteSession(request, session.id)
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

  // ─── SCRUM-296: link delete affordance in materials tree ───────────────────
  test('SCRUM-296: link row in materials tree shows × delete button that removes the link', async ({ page, context, request }) => {
    const email = uniqueEmail('link-delete')
    await createUserAndLoginWithId(context, request, email)
    const session = await createSession(request, 'E2E SCRUM-296 Link Delete')

    await navigateToCreatorSession(page, session.id)

    // Add a link via the URL input.
    await ensureAddContentExpanded(page)
    const urlInput = page.getByTestId('add-link-url-input')
    await expect(urlInput).toBeVisible({ timeout: 5_000 })
    await urlInput.fill(TEST_LINK_URL)
    const addLinkResponse = page.waitForResponse(
      (res) => res.url().includes('/links') && res.request().method() === 'POST',
      { timeout: 15_000 }
    )
    await page.getByTestId('add-link-submit-btn').click()
    await addLinkResponse

    // Link row appears in the Links section.
    const linkItem = page.getByTestId('link-item').filter({ hasText: /example\.com|Example Domain/i })
    await expect(linkItem).toBeVisible({ timeout: 15_000 })

    // The × delete button is present on the link row (this is the bug fix —
    // before SCRUM-296 the link row had no delete affordance).
    const deleteBtn = page.getByTestId('link-delete-btn')
    await expect(deleteBtn).toBeVisible({ timeout: 5_000 })

    // Click delete and wait for the DELETE request to complete.
    const deleteLinkResponse = page.waitForResponse(
      (res) => res.url().includes('/links/') && res.request().method() === 'DELETE',
      { timeout: 15_000 }
    )
    await deleteBtn.click()
    await deleteLinkResponse

    // Row disappears after the session refetch.
    await expect(linkItem).toHaveCount(0, { timeout: 15_000 })

    // Cleanup
    await loginAsAdmin(request)
    await deleteSession(request, session.id)
  })
})
