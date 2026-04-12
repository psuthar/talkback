import { test, expect } from '@playwright/test'
import {
  createUserAndLoginWithId,
  createSession,
  deleteSession,
  deleteUserViaAdmin,
  loginAsAdmin,
  pasteMaterial,
  uniqueEmail,
} from './fixtures'

// Seeded text — keywords used in stable assertions below.
const FIXTURE_TEXT =
  'Meridian proposal approved. APAC expansion confirmed at 2.4M. Churn target below 6% this quarter.'

// The full journey (navigate → material → ask → answer) needs more than the 30 s default.
// 90 s covers: page load (5 s) + material wait (10 s) + answer wait (45 s) + margin.
test.setTimeout(90_000)

// Track seeded IDs for afterAll cleanup.
let seededUserId = ''
let seededSessionId = ''

test.afterAll(async ({ request }) => {
  await loginAsAdmin(request)
  if (seededSessionId) await deleteSession(request, seededSessionId)
  if (seededUserId) await deleteUserViaAdmin(request, seededUserId)
})

test(
  'participant opens prepared session, sees material, asks question, sees answer',
  async ({ page, context, request }) => {
    // --- Setup: seed user + session + material entirely via API ---
    const email = uniqueEmail('happy-path')
    const displayName = 'Happy Path User'
    seededUserId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', displayName)

    const session = await createSession(request, 'E2E Happy Path Session')
    seededSessionId = session.id
    await pasteMaterial(request, session.id, 'Meridian Report', FIXTURE_TEXT)

    // --- 1. Navigate to session in participant view ---
    const params = new URLSearchParams({ session: session.id, mode: 'view' })
    await page.goto(`/?${params.toString()}`)
    await page.waitForLoadState('networkidle')

    // --- 2. Expand materials panel if collapsed ---
    const expandPanelBtn = page.getByRole('button', { name: 'Expand materials panel' })
    if (await expandPanelBtn.isVisible({ timeout: 2_000 }).catch(() => false)) {
      await expandPanelBtn.click()
    }

    // --- 3. Assert seeded material appears in the Documents section ---
    await expect(
      page.getByTestId('material-item').filter({ hasText: 'Meridian Report' })
    ).toBeVisible({ timeout: 10_000 })

    // --- 4. Assert Q&A input is present ---
    const questionInput = page.getByTestId('question-input')
    await expect(questionInput).toBeVisible()

    // --- 5. Type and submit a question ---
    await questionInput.fill('What is the APAC expansion budget?')

    // Set up response listener BEFORE clicking so we don't miss a fast response.
    // The ask endpoint is POST /api/sessions/{id}/ask (not /questions).
    const questionPosted = page.waitForResponse(
      (res) => res.url().includes('/ask') && res.request().method() === 'POST',
      { timeout: 15_000 }
    )
    await page.getByTestId('ask-button').click()
    await questionPosted

    // --- 6 & 7. Wait for a terminal answer status ---
    // QAHistory's auto-expand effect opens the card for the current asker when the
    // answer arrives (matches asked_by to currentAskerName). Trust that and wait for
    // any answer-status badge to become visible. 45 s covers a slow or cold LLM.
    const statusBadge = page.locator('[data-testid="answer-status"]').first()
    await statusBadge.waitFor({ state: 'visible', timeout: 45_000 })

    // --- 8. Assert terminal state class (answered | not_covered | error) ---
    // "error" is a valid terminal state in CI when no OpenAI key is configured.
    await expect(statusBadge).toHaveClass(/answered|not_covered|error/)

    // --- 9. Keyword + citation assertion — only when LLM actually answered ---
    const statusText = (await statusBadge.textContent()) ?? ''
    if (statusText.trim() === 'answered') {
      const answerDiv = page.locator('[data-testid="answer-text"]').first()
      await expect(answerDiv).toBeVisible()
      const answerText = (await answerDiv.textContent()) ?? ''
      expect(answerText.toLowerCase()).toMatch(/meridian|apac|churn/)

      // Assert that at least one citation is rendered.
      // The backend returns citations for paste-based materials (source_type=material,
      // label e.g. "Meridian Report (block 1)"). The UI renders data-testid="citations"
      // when citations.length > 0.
      // TODO: stronger coverage (verifying the exact label text against the material title)
      // would benefit from a real PDF upload fixture so the citation source_id maps to a
      // named file rather than an inline text block.
      const citationsContainer = page.locator('[data-testid="citations"]').first()
      await expect(citationsContainer).toBeVisible({ timeout: 5_000 })
    }
  }
)
