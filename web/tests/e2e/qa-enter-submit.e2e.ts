import { test, expect } from '@playwright/test'
import {
  createSession,
  createUserAndLoginWithId,
  deleteSession,
  deleteUserViaAdmin,
  dismissParticipantOnboardingIfPresent,
  loginAsAdmin,
  pasteMaterial,
  uniqueEmail,
} from './fixtures'

// SCRUM-310: Enter in the Q&A textarea submits the question (no Ask click needed).
// We don't wait for the LLM answer — the assertion is that the POST /ask fires when
// Enter is pressed, which proves the keyboard shortcut is wired to handleAsk.

test.setTimeout(60_000)

let seededUserId = ''
let seededSessionId = ''

test.afterAll(async ({ request }) => {
  await loginAsAdmin(request)
  if (seededSessionId) await deleteSession(request, seededSessionId)
  if (seededUserId) await deleteUserViaAdmin(request, seededUserId)
})

test('pressing Enter in the Q&A textarea submits the question', async ({ page, context, request }) => {
  const email = uniqueEmail('qa-enter')
  seededUserId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', 'Enter Submit User')

  const session = await createSession(request, 'E2E Enter Submit Session')
  seededSessionId = session.id
  await pasteMaterial(request, session.id, 'Budget Report', 'APAC budget approved at 2.4M.')

  const params = new URLSearchParams({ session: session.id, mode: 'view' })
  await page.goto(`/?${params.toString()}`)
  await page.waitForLoadState('networkidle')
  await dismissParticipantOnboardingIfPresent(page)

  const questionInput = page.getByTestId('question-input')
  await expect(questionInput).toBeVisible()

  // Sanity: typing + Shift+Enter inserts a newline (no submit).
  // Listen for an unexpected /ask POST during this phase — should NOT fire.
  let unexpectedAskFired = false
  const shiftEnterListener = (req: import('@playwright/test').Request) => {
    if (req.url().includes('/ask') && req.method() === 'POST') {
      unexpectedAskFired = true
    }
  }
  page.on('request', shiftEnterListener)

  await questionInput.fill('Line one')
  await questionInput.press('Shift+Enter')
  await questionInput.type('Line two')
  // Brief wait so any (incorrect) request would have fired.
  await page.waitForTimeout(500)
  expect(unexpectedAskFired).toBe(false)
  page.off('request', shiftEnterListener)

  // Verify the textarea actually contains a newline between the two lines.
  await expect(questionInput).toHaveValue(/Line one\nLine two/)

  // Enter (no modifier) submits — the /ask POST must fire.
  const askPosted = page.waitForResponse(
    (res) => res.url().includes('/ask') && res.request().method() === 'POST',
    { timeout: 15_000 }
  )
  await questionInput.press('Enter')
  const askResponse = await askPosted
  expect(askResponse.ok()).toBe(true)
})

test('pressing Enter in an empty Q&A textarea is a no-op', async ({ page, context, request }) => {
  const email = uniqueEmail('qa-enter-empty')
  const userId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', 'Empty Enter User')

  const session = await createSession(request, 'E2E Empty Enter Session')
  await pasteMaterial(request, session.id, 'Note', 'Quick note.')

  const params = new URLSearchParams({ session: session.id, mode: 'view' })
  await page.goto(`/?${params.toString()}`)
  await page.waitForLoadState('networkidle')
  await dismissParticipantOnboardingIfPresent(page)

  const questionInput = page.getByTestId('question-input')
  await expect(questionInput).toBeVisible()

  let askFired = false
  page.on('request', (req) => {
    if (req.url().includes('/ask') && req.method() === 'POST') {
      askFired = true
    }
  })

  // Empty textarea: Enter should be a no-op (matches Ask button's disabled state).
  await questionInput.focus()
  await questionInput.press('Enter')
  // Whitespace-only: also a no-op.
  await questionInput.fill('   ')
  await questionInput.press('Enter')
  await page.waitForTimeout(500)
  expect(askFired).toBe(false)

  await loginAsAdmin(request)
  await deleteSession(request, session.id)
  await deleteUserViaAdmin(request, userId)
})
