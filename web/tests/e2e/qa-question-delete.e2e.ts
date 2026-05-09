import { test, expect, request as playwrightApiRequest } from '@playwright/test'
import {
  ADMIN_EMAIL,
  ADMIN_PASSWORD,
  API_BASE,
  createInvitation,
  createSession,
  createUserAndLoginWithId,
  deleteSession,
  deleteUserViaAdmin,
  injectCookiesFromResponse,
  loginAsAdmin,
  pasteMaterial,
  uniqueEmail,
} from './fixtures'

// SCRUM-367 — creator can delete a top-level question via kebab → modal with undo and live peer tombstone.
//
// Each spec creates and tears down its own users/session so they can run independently. We use 60s
// timeout because some specs cross multiple browser contexts and synchronous /ask calls.
test.setTimeout(60_000)

// Helper: synchronously ask a question against the session and return the question object the
// server persisted. The /api/sessions/:id/ask path is synchronous in this codebase — the Q+A
// pair is in the database before the response returns, so the next GET /questions sees both rows.
async function askQuestion(
  request: import('@playwright/test').APIRequestContext,
  sessionId: string,
  text: string
): Promise<{ id: string; question_text: string }> {
  const res = await request.post(`${API_BASE}/api/sessions/${sessionId}/ask`, {
    data: { question_text: text, asked_via: 'text' },
  })
  expect(res.ok()).toBe(true)
  const body = await res.json()
  // Server returns { question, answer, ... } or { id, question_text } depending on shape — handle both.
  const q = body?.question ?? body
  return { id: q.id, question_text: q.question_text ?? text }
}

async function getQuestionByText(
  request: import('@playwright/test').APIRequestContext,
  sessionId: string,
  text: string
): Promise<{ id: string; answer_id?: string }> {
  const res = await request.get(`${API_BASE}/sessions/${sessionId}/questions`)
  expect(res.ok()).toBe(true)
  const body = await res.json()
  const questions: Array<{ id: string; question_text: string }> = body.questions || []
  const q = questions.find((row) => row.question_text === text)
  expect(q, `seeded question "${text}" not found`).toBeTruthy()
  const answers: Array<{ id: string; question_id: string }> = body.answers || []
  const a = answers.find((row) => row.question_id === q!.id)
  return { id: q!.id, answer_id: a?.id }
}

// --- Spec 1: creator delete-undo (5s window) — question stays after Undo ---
test('creator delete-undo: kebab → modal → confirm → Undo within 5s → question stays', async ({ page, context, request }) => {
  const email = uniqueEmail('q-delete-undo')
  const userId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', 'Undo Creator')
  const session = await createSession(request, 'QA Delete Undo E2E')
  await pasteMaterial(request, session.id, 'Material', 'The board approved Project Alpha at 1.2M.')

  const QUESTION_TEXT = 'What did the board approve?'
  const seeded = await askQuestion(request, session.id, QUESTION_TEXT)

  await page.goto(`/?session=${session.id}&mode=edit`)
  await page.waitForLoadState('networkidle')

  // CreatorMode renders its own thread tree (no question-item testid). The kebab
  // testid is question-kebab-<id>. Use that as the stable selector.
  const kebab = page.getByTestId(`question-kebab-${seeded.id}`)
  await expect(kebab).toBeVisible({ timeout: 15_000 })
  await kebab.click()
  const modal = page.getByTestId('delete-question-modal')
  await expect(modal).toBeVisible()

  // Confirm — optimistic remove + Undo toast.
  await page.getByTestId('delete-question-confirm').click()
  await expect(page.getByTestId('delete-question-undo-toast')).toBeVisible()
  await expect(kebab).toHaveCount(0)

  // Undo within 5s — question reappears.
  await page.getByTestId('delete-question-undo-btn').click()
  await expect(page.getByTestId('delete-question-undo-toast')).toHaveCount(0)
  await expect(page.getByTestId(`question-kebab-${seeded.id}`)).toBeVisible({ timeout: 5_000 })

  // Server-side: question still exists (no DELETE was fired).
  const verify = await request.get(`${API_BASE}/sessions/${session.id}/questions`)
  expect(verify.ok()).toBe(true)
  const verifyBody = await verify.json()
  const stillThere = (verifyBody.questions || []).find((q: { question_text: string }) => q.question_text === QUESTION_TEXT)
  expect(stillThere).toBeTruthy()

  // Cleanup
  await loginAsAdmin(request)
  await deleteSession(request, session.id)
  await deleteUserViaAdmin(request, userId)
})

// --- Spec 2: creator confirm-delete past 5s → removed; reload → still gone ---
test('creator confirm-delete: past 5s → question removed; reload → still gone', async ({ page, context, request }) => {
  const email = uniqueEmail('q-delete-confirm')
  const userId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', 'Confirm Creator')
  const session = await createSession(request, 'QA Delete Confirm E2E')
  await pasteMaterial(request, session.id, 'Material', 'Decision: ship the feature this week.')

  const QUESTION_TEXT = 'When are we shipping the feature?'
  const seeded = await askQuestion(request, session.id, QUESTION_TEXT)

  await page.goto(`/?session=${session.id}&mode=edit`)
  await page.waitForLoadState('networkidle')

  const kebab = page.getByTestId(`question-kebab-${seeded.id}`)
  await expect(kebab).toBeVisible({ timeout: 15_000 })
  await kebab.click()
  await expect(page.getByTestId('delete-question-modal')).toBeVisible()
  await page.getByTestId('delete-question-confirm').click()

  // Wait for the 5s undo window to elapse so the DELETE actually fires.
  await expect(page.getByTestId('delete-question-undo-toast')).toBeVisible()
  await page.waitForTimeout(5500)
  await expect(page.getByTestId('delete-question-undo-toast')).toHaveCount(0)

  // Card stays gone for the originating client.
  await expect(page.getByTestId(`question-kebab-${seeded.id}`)).toHaveCount(0)

  // Reload — server confirms it really was deleted.
  await page.reload()
  await page.waitForLoadState('networkidle')
  await expect(page.getByTestId(`question-kebab-${seeded.id}`)).toHaveCount(0)

  const verify = await request.get(`${API_BASE}/sessions/${session.id}/questions`)
  expect(verify.ok()).toBe(true)
  const verifyBody = await verify.json()
  const stillThere = (verifyBody.questions || []).find((q: { question_text: string }) => q.question_text === QUESTION_TEXT)
  expect(stillThere).toBeFalsy()

  // Cleanup
  await loginAsAdmin(request)
  await deleteSession(request, session.id)
  await deleteUserViaAdmin(request, userId)
})

// --- Spec 3: confirmed-answer cascade requires type-to-confirm input ---
test('confirmed-answer cascade: modal shows type-to-confirm input; wrong word disables Delete', async ({ page, context, request }) => {
  const email = uniqueEmail('q-delete-confirmed')
  const userId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', 'Confirmed Creator')
  const session = await createSession(request, 'QA Delete Confirmed-answer E2E')
  await pasteMaterial(request, session.id, 'Material', 'Sales for Q3 were 4.2M dollars.')

  const QUESTION_TEXT = 'Quarterly revenue for Q3?'
  const seeded = await askQuestion(request, session.id, QUESTION_TEXT)
  const { answer_id } = await getQuestionByText(request, session.id, QUESTION_TEXT)
  expect(answer_id, 'expected an answer for the seeded question').toBeTruthy()

  // Mark the answer confirmed=true (creator-only endpoint; this user IS the creator).
  const confirmRes = await request.patch(`${API_BASE}/api/sessions/${session.id}/answers/${answer_id}/confirm`, {
    data: { confirmed: true },
  })
  expect(confirmRes.ok()).toBe(true)

  await page.goto(`/?session=${session.id}&mode=edit`)
  await page.waitForLoadState('networkidle')

  const kebab = page.getByTestId(`question-kebab-${seeded.id}`)
  await expect(kebab).toBeVisible({ timeout: 15_000 })
  await kebab.click()
  const modal = page.getByTestId('delete-question-modal')
  await expect(modal).toBeVisible()

  // Type-to-confirm input must be present.
  const input = page.getByTestId('delete-question-type-confirm')
  await expect(input).toBeVisible()
  const confirmBtn = page.getByTestId('delete-question-confirm')
  await expect(confirmBtn).toBeDisabled()

  // Wrong word: still disabled.
  await input.fill('nope')
  await expect(confirmBtn).toBeDisabled()

  // Correct word ("Quarterly") — case-insensitive — enables Delete.
  await input.fill('quarterly')
  await expect(confirmBtn).toBeEnabled()
  await confirmBtn.click()

  // Optimistic remove + undo toast.
  await expect(page.getByTestId('delete-question-undo-toast')).toBeVisible()
  await expect(page.getByTestId(`question-kebab-${seeded.id}`)).toHaveCount(0)

  // Cleanup
  await loginAsAdmin(request)
  await deleteSession(request, session.id)
  await deleteUserViaAdmin(request, userId)
})

// --- Spec 4: peer tombstone via WS — second context sees tombstone without refresh ---
test('peer tombstone via WS: participant in second context sees tombstone within WS-latency budget', async ({ page, context, request, browser }) => {
  const creatorEmail = uniqueEmail('q-delete-peer-creator')
  const creatorId = await createUserAndLoginWithId(context, request, creatorEmail, 'SmokePass123!', 'Peer Creator')
  const session = await createSession(request, 'QA Delete Peer Tombstone E2E')
  await pasteMaterial(request, session.id, 'Material', 'Quarterly review notes.')

  const QUESTION_TEXT = 'What were the highlights this quarter?'
  const seeded = await askQuestion(request, session.id, QUESTION_TEXT)

  // --- Participant: separate browser context, separate API context for invitation accept ---
  const participantEmail = uniqueEmail('q-delete-peer-participant')
  const participantApi = await playwrightApiRequest.newContext()
  const pSignup = await participantApi.post(`${API_BASE}/api/auth/signup`, {
    data: { email: participantEmail, password: 'SmokePass123!', display_name: 'Peer Participant' },
  })
  expect(pSignup.ok()).toBe(true)
  const pSignupData = await pSignup.json()
  const participantId = pSignupData.id as string
  await participantApi.post(`${API_BASE}/api/auth/login`, {
    data: { email: participantEmail, password: 'SmokePass123!' },
  })
  // Invite + accept so the participant has session access.
  const inv = await createInvitation(request, session.id, participantEmail, 'participant')
  const tokenQs = inv.accept_url.includes('?') ? inv.accept_url.split('?')[1] : ''
  const token = new URLSearchParams(tokenQs).get('token')
  const accept = await participantApi.post(`${API_BASE}/api/invitations/accept`, { data: { token } })
  expect(accept.ok()).toBe(true)

  // Build a separate browser context for the participant and inject their cookies.
  const participantBrowserCtx = await browser.newContext()
  const loginRes = await participantApi.post(`${API_BASE}/api/auth/login`, {
    data: { email: participantEmail, password: 'SmokePass123!' },
  })
  await injectCookiesFromResponse(participantBrowserCtx, loginRes)
  const peerPage = await participantBrowserCtx.newPage()
  await peerPage.goto(`/?session=${session.id}&mode=view`)
  await peerPage.waitForLoadState('networkidle')

  // Both pages now see the question — peer uses QAHistory which sets data-testid="question-item".
  await expect(peerPage.locator('[data-testid="question-item"]').filter({ hasText: QUESTION_TEXT }).first()).toBeVisible({ timeout: 15_000 })

  await page.goto(`/?session=${session.id}&mode=edit`)
  await page.waitForLoadState('networkidle')
  const creatorKebab = page.getByTestId(`question-kebab-${seeded.id}`)
  await expect(creatorKebab).toBeVisible({ timeout: 15_000 })

  // Creator confirms delete.
  await creatorKebab.click()
  await page.getByTestId('delete-question-confirm').click()
  await expect(page.getByTestId('delete-question-undo-toast')).toBeVisible()

  // Wait past the 5s undo window so the DELETE fires and the WS broadcast lands at the peer.
  await page.waitForTimeout(5500)

  // Peer should now show a tombstone for the question.
  const tombstone = peerPage.getByTestId('question-tombstone').first()
  await expect(tombstone).toBeVisible({ timeout: 8_000 })
  await expect(tombstone).toContainText(/deleted/i)

  // Cleanup
  await participantBrowserCtx.close()
  await participantApi.dispose()
  await loginAsAdmin(request)
  await deleteSession(request, session.id)
  await deleteUserViaAdmin(request, creatorId)
  await deleteUserViaAdmin(request, participantId)
})

// --- Spec 5: participant has no kebab on any question ---
test('participant view: no kebab affordance is shown on any question', async ({ page, context, request, browser }) => {
  const creatorEmail = uniqueEmail('q-delete-noperm-creator')
  const creatorId = await createUserAndLoginWithId(context, request, creatorEmail, 'SmokePass123!', 'NoPerm Creator')
  const session = await createSession(request, 'QA Delete No-Permission E2E')
  await pasteMaterial(request, session.id, 'Material', 'Some material content.')

  const QUESTION_TEXT = 'A participant-visible question'
  await askQuestion(request, session.id, QUESTION_TEXT)

  // Participant signs up and accepts the invite.
  const participantEmail = uniqueEmail('q-delete-noperm-participant')
  const participantApi = await playwrightApiRequest.newContext()
  const pSignup = await participantApi.post(`${API_BASE}/api/auth/signup`, {
    data: { email: participantEmail, password: 'SmokePass123!', display_name: 'NoPerm Participant' },
  })
  expect(pSignup.ok()).toBe(true)
  const pSignupData = await pSignup.json()
  const participantId = pSignupData.id as string
  await participantApi.post(`${API_BASE}/api/auth/login`, {
    data: { email: participantEmail, password: 'SmokePass123!' },
  })
  const inv = await createInvitation(request, session.id, participantEmail, 'participant')
  const tokenQs = inv.accept_url.includes('?') ? inv.accept_url.split('?')[1] : ''
  const token = new URLSearchParams(tokenQs).get('token')
  const accept = await participantApi.post(`${API_BASE}/api/invitations/accept`, { data: { token } })
  expect(accept.ok()).toBe(true)

  const partCtx = await browser.newContext()
  const loginRes = await participantApi.post(`${API_BASE}/api/auth/login`, {
    data: { email: participantEmail, password: 'SmokePass123!' },
  })
  await injectCookiesFromResponse(partCtx, loginRes)
  const participantPage = await partCtx.newPage()
  await participantPage.goto(`/?session=${session.id}&mode=view`)
  await participantPage.waitForLoadState('networkidle')

  // Question card is visible to the participant.
  const card = participantPage.locator('[data-testid="question-item"]').filter({ hasText: QUESTION_TEXT }).first()
  await expect(card).toBeVisible({ timeout: 15_000 })
  // Kebab must NOT exist for participants.
  const kebabCount = await participantPage.locator('[data-testid^="question-kebab-"]').count()
  expect(kebabCount).toBe(0)

  // Cleanup
  await partCtx.close()
  await participantApi.dispose()
  await loginAsAdmin(request)
  await deleteSession(request, session.id)
  await deleteUserViaAdmin(request, creatorId)
  await deleteUserViaAdmin(request, participantId)
})

// --- Spec 6: reply has no kebab in creator view ---
test('reply has no kebab in creator view (only root cards)', async ({ page, context, request }) => {
  const email = uniqueEmail('q-delete-reply')
  const userId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', 'Reply Creator')
  const session = await createSession(request, 'QA Delete Reply E2E')
  await pasteMaterial(request, session.id, 'Material', 'Reply test material.')

  const ROOT_TEXT = 'Root question for reply test'
  const REPLY_TEXT = 'Follow-up reply to the root question'
  const root = await askQuestion(request, session.id, ROOT_TEXT)
  const replyRes = await request.post(`${API_BASE}/api/sessions/${session.id}/ask`, {
    data: { question_text: REPLY_TEXT, asked_via: 'text', parent_question_id: root.id },
  })
  expect(replyRes.ok()).toBe(true)

  await page.goto(`/?session=${session.id}&mode=edit`)
  await page.waitForLoadState('networkidle')

  // Root card has a kebab.
  const rootCardKebab = page.locator(`[data-testid="question-kebab-${root.id}"]`)
  await expect(rootCardKebab).toBeVisible({ timeout: 15_000 })

  // Get the reply ID via the API response (parent_question_id linkage) and assert no kebab on it.
  const replyBody = await replyRes.json()
  const replyId = (replyBody?.question?.id ?? replyBody?.id) as string
  expect(replyId).toBeTruthy()
  // The reply card is hidden by default until root is expanded; expand it first.
  // The CreatorMode toggle button is the ▼/▷ next to the root card. Click it.
  const expandBtn = page.locator(`button[aria-label="Expand"]`).first()
  if (await expandBtn.isVisible({ timeout: 1_000 }).catch(() => false)) {
    await expandBtn.click()
  }
  // Reply kebab must NOT exist.
  await expect(page.locator(`[data-testid="question-kebab-${replyId}"]`)).toHaveCount(0)

  // Cleanup
  await loginAsAdmin(request)
  await deleteSession(request, session.id)
  await deleteUserViaAdmin(request, userId)
})

// --- Spec 7: error path — server returns 404 (already deleted) → optimistic stays committed ---
test('error path: server returns 404 (already deleted) → optimistic removal stays committed', async ({ page, context, request }) => {
  const email = uniqueEmail('q-delete-404')
  const userId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', '404 Creator')
  const session = await createSession(request, 'QA Delete 404 E2E')
  await pasteMaterial(request, session.id, 'Material', '404 path material.')

  const QUESTION_TEXT = 'Already-deleted-elsewhere question'
  const seeded = await askQuestion(request, session.id, QUESTION_TEXT)

  // Pre-route: stub the DELETE to return 404 from the network layer so the client hits the
  // 404 branch even though the question is still in the DB on this run.
  await page.route(`**/api/sessions/${session.id}/questions/${seeded.id}`, async (route) => {
    if (route.request().method() === 'DELETE') {
      await route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ error: 'gone' }) })
      return
    }
    await route.continue()
  })

  await page.goto(`/?session=${session.id}&mode=edit`)
  await page.waitForLoadState('networkidle')

  const kebab = page.getByTestId(`question-kebab-${seeded.id}`)
  await expect(kebab).toBeVisible({ timeout: 15_000 })
  await kebab.click()
  await page.getByTestId('delete-question-confirm').click()
  await expect(page.getByTestId('delete-question-undo-toast')).toBeVisible()

  // Wait past 5s for the DELETE to fire and return 404.
  await page.waitForTimeout(5500)
  // Optimistic removal is committed; no error toast.
  await expect(page.getByTestId('delete-question-error-toast')).toHaveCount(0)
  await expect(page.getByTestId(`question-kebab-${seeded.id}`)).toHaveCount(0)

  // Cleanup (server still has the row — admin teardown drops the session).
  await page.unroute(`**/api/sessions/${session.id}/questions/${seeded.id}`)
  await loginAsAdmin(request)
  await deleteSession(request, session.id)
  await deleteUserViaAdmin(request, userId)
})

// --- Spec 8 (auth-scope): global admin who is NOT the session creator can delete ---
test('global admin (not session creator) sees the kebab and can delete a root question', async ({ page, context, request, browser }) => {
  // Creator owns the session.
  const creatorEmail = uniqueEmail('q-delete-admin-creator')
  const creatorApi = await playwrightApiRequest.newContext()
  const cSignup = await creatorApi.post(`${API_BASE}/api/auth/signup`, {
    data: { email: creatorEmail, password: 'SmokePass123!', display_name: 'Admin-Test Creator' },
  })
  expect(cSignup.ok()).toBe(true)
  const cData = await cSignup.json()
  const creatorId = cData.id as string
  await creatorApi.post(`${API_BASE}/api/auth/login`, {
    data: { email: creatorEmail, password: 'SmokePass123!' },
  })

  const session = await createSession(creatorApi, 'QA Delete Admin-Override E2E')
  await pasteMaterial(creatorApi, session.id, 'Material', 'Admin-override material.')
  const QUESTION_TEXT = 'Question deletable by admin only'
  const seeded = await askQuestion(creatorApi, session.id, QUESTION_TEXT)

  // Admin signs into the page browser. Reuse the bootstrap admin (matches teardown).
  const adminLoginRes = await request.post(`${API_BASE}/api/auth/login`, {
    data: { email: ADMIN_EMAIL, password: ADMIN_PASSWORD },
  })
  if (!adminLoginRes.ok()) {
    test.skip(true, `bootstrap admin (${ADMIN_EMAIL}) is not present in this API; set TALKBACK_ADMIN_EMAIL/TALKBACK_ADMIN_PASSWORD to a valid admin to run this spec`)
  }
  await injectCookiesFromResponse(context, adminLoginRes)

  // Admin opens the session in edit mode. Server treats admin as a session editor.
  await page.goto(`/?session=${session.id}&mode=edit`)
  await page.waitForLoadState('networkidle')

  const kebab = page.getByTestId(`question-kebab-${seeded.id}`)
  await expect(kebab).toBeVisible({ timeout: 15_000 })
  await kebab.click()
  await expect(page.getByTestId('delete-question-modal')).toBeVisible()
  await page.getByTestId('delete-question-confirm').click()
  await expect(page.getByTestId('delete-question-undo-toast')).toBeVisible()

  // Wait past 5s — server-side DELETE fires under admin auth.
  await page.waitForTimeout(5500)
  await expect(page.getByTestId(`question-kebab-${seeded.id}`)).toHaveCount(0)

  // Verify server-side deletion via creator's API context.
  const verify = await creatorApi.get(`${API_BASE}/sessions/${session.id}/questions`)
  expect(verify.ok()).toBe(true)
  const verifyBody = await verify.json()
  const stillThere = (verifyBody.questions || []).find((q: { question_text: string }) => q.question_text === QUESTION_TEXT)
  expect(stillThere).toBeFalsy()

  // Cleanup
  await creatorApi.dispose()
  await loginAsAdmin(request)
  await deleteSession(request, session.id)
  await deleteUserViaAdmin(request, creatorId)
})
