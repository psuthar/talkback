import { test, expect } from '@playwright/test'
import {
  API_BASE,
  createSession,
  createUserAndLoginWithId,
  deleteSession,
  deleteUserViaAdmin,
  loginAsAdmin,
  pasteMaterial,
  uniqueEmail,
} from './fixtures'

// Page load + question seed + answer wait — no need for LLM answer check here.
// 30 s covers: signup/login, session load, question visibility.
test.setTimeout(30_000)

// Track seeded IDs for afterAll cleanup.
let seededUserId = ''
let seededSessionId = ''

test.afterAll(async ({ request }) => {
  await loginAsAdmin(request)
  if (seededSessionId) await deleteSession(request, seededSessionId)
  if (seededUserId) await deleteUserViaAdmin(request, seededUserId)
})

test('previously asked question is visible in QA history panel', async ({ page, context, request }) => {
  // --- Seed: user + session + material via API ---
  const email = uniqueEmail('qa-history')
  seededUserId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', 'History User')

  const session = await createSession(request, 'E2E QA History Session')
  seededSessionId = session.id
  await pasteMaterial(
    request,
    session.id,
    'History Report',
    'Decision was made to proceed with Project Omega at 1.2M budget.'
  )

  // --- Seed a question via API (synchronous ask — answer already attached) ---
  const askRes = await request.post(`${API_BASE}/api/sessions/${session.id}/ask`, {
    data: { question_text: 'What is the Project Omega budget?', asked_via: 'text' },
  })
  expect(askRes.ok()).toBe(true)

  // --- Navigate to session in view mode ---
  const params = new URLSearchParams({ session: session.id, mode: 'view' })
  await page.goto(`/?${params.toString()}`)
  await page.waitForLoadState('networkidle')

  // --- The question item must be visible in the QA panel ---
  // QACard renders data-testid="question-item" for every question regardless of collapse state.
  const questionItem = page.locator('[data-testid="question-item"]').first()
  await questionItem.waitFor({ state: 'visible', timeout: 10_000 })

  // --- Expand the card so we can assert the question text ---
  // The toggle button has aria-label "Expand" when collapsed.
  const expandBtn = questionItem.getByRole('button', { name: 'Expand' })
  const isExpandable = await expandBtn.isVisible({ timeout: 1_000 }).catch(() => false)
  if (isExpandable) {
    await expandBtn.click()
  }

  // --- Assert question text is present in the expanded card ---
  await expect(
    page.locator('[data-testid="question-item"]').filter({ hasText: 'Project Omega budget' })
  ).toBeVisible({ timeout: 5_000 })
})
