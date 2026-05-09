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

// SCRUM-367 (initial delivery) + SCRUM-370 (trash icon, immediate-delete) — creator/admin can
// delete a top-level question via trash icon → modal → confirm. DELETE fires immediately on
// confirm; no undo window. Peer viewers see the question_deleted WS broadcast and render a
// ~10s tombstone before the row is dropped.
//
// Each spec creates and tears down its own users/session so they can run independently.
test.setTimeout(60_000)

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

// --- Spec 1: creator confirm-delete fires DELETE immediately; row stays gone after reload ---
test('creator confirm-delete: confirm → DELETE fires immediately → row gone; reload → still gone', async ({ page, context, request }) => {
  const email = uniqueEmail('q-delete-confirm')
  const userId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', 'Confirm Creator')
  const session = await createSession(request, 'QA Delete Confirm E2E')
  await pasteMaterial(request, session.id, 'Material', 'Decision: ship the feature this week.')

  const QUESTION_TEXT = 'When are we shipping the feature?'
  const seeded = await askQuestion(request, session.id, QUESTION_TEXT)

  await page.goto(`/?session=${session.id}&mode=edit`)
  await page.waitForLoadState('networkidle')

  const trigger = page.getByTestId(`delete-question-trigger-${seeded.id}`)
  await expect(trigger).toBeVisible({ timeout: 15_000 })
  await trigger.click()
  await expect(page.getByTestId('delete-question-modal')).toBeVisible()

  // Confirm: DELETE should fire immediately (no undo window).
  const deletePromise = page.waitForResponse(
    (resp) => resp.url().includes(`/sessions/${session.id}/questions/${seeded.id}`) && resp.request().method() === 'DELETE'
  )
  await page.getByTestId('delete-question-confirm').click()
  const deleteResp = await deletePromise
  expect(deleteResp.status()).toBe(204)

  // Trigger gone for the originating client.
  await expect(page.getByTestId(`delete-question-trigger-${seeded.id}`)).toHaveCount(0)

  // Reload — server confirms it really was deleted.
  await page.reload()
  await page.waitForLoadState('networkidle')
  await expect(page.getByTestId(`delete-question-trigger-${seeded.id}`)).toHaveCount(0)

  const verify = await request.get(`${API_BASE}/sessions/${session.id}/questions`)
  expect(verify.ok()).toBe(true)
  const verifyBody = await verify.json()
  const stillThere = (verifyBody.questions || []).find((q: { question_text: string }) => q.question_text === QUESTION_TEXT)
  expect(stillThere).toBeFalsy()

  await loginAsAdmin(request)
  await deleteSession(request, session.id)
  await deleteUserViaAdmin(request, userId)
})

// --- Spec 2: confirmed-answer cascade requires type-to-confirm input ---
test('confirmed-answer cascade: modal shows type-to-confirm input; wrong word disables Delete; correct word fires DELETE', async ({ page, context, request }) => {
  const email = uniqueEmail('q-delete-confirmed')
  const userId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', 'Confirmed Creator')
  const session = await createSession(request, 'QA Delete Confirmed-answer E2E')
  await pasteMaterial(request, session.id, 'Material', 'Sales for Q3 were 4.2M dollars.')

  const QUESTION_TEXT = 'Quarterly revenue for Q3?'
  const seeded = await askQuestion(request, session.id, QUESTION_TEXT)
  const { answer_id } = await getQuestionByText(request, session.id, QUESTION_TEXT)
  expect(answer_id, 'expected an answer for the seeded question').toBeTruthy()

  const confirmRes = await request.patch(`${API_BASE}/api/sessions/${session.id}/answers/${answer_id}/confirm`, {
    data: { confirmed: true },
  })
  expect(confirmRes.ok()).toBe(true)

  await page.goto(`/?session=${session.id}&mode=edit`)
  await page.waitForLoadState('networkidle')

  const trigger = page.getByTestId(`delete-question-trigger-${seeded.id}`)
  await expect(trigger).toBeVisible({ timeout: 15_000 })
  await trigger.click()
  const modal = page.getByTestId('delete-question-modal')
  await expect(modal).toBeVisible()

  const input = page.getByTestId('delete-question-type-confirm')
  await expect(input).toBeVisible()
  const confirmBtn = page.getByTestId('delete-question-confirm')
  await expect(confirmBtn).toBeDisabled()

  await input.fill('nope')
  await expect(confirmBtn).toBeDisabled()

  // Correct first word ("Quarterly") — case-insensitive — enables Delete.
  await input.fill('quarterly')
  await expect(confirmBtn).toBeEnabled()

  const deletePromise = page.waitForResponse(
    (resp) => resp.url().includes(`/sessions/${session.id}/questions/${seeded.id}`) && resp.request().method() === 'DELETE'
  )
  await confirmBtn.click()
  const deleteResp = await deletePromise
  expect(deleteResp.status()).toBe(204)

  await expect(page.getByTestId(`delete-question-trigger-${seeded.id}`)).toHaveCount(0)

  await loginAsAdmin(request)
  await deleteSession(request, session.id)
  await deleteUserViaAdmin(request, userId)
})

// --- Spec 3: peer tombstone via WS — second context sees tombstone without refresh ---
test('peer tombstone via WS: participant in second context sees tombstone within WS-latency budget', async ({ page, context, request, browser }) => {
  const creatorEmail = uniqueEmail('q-delete-peer-creator')
  const creatorId = await createUserAndLoginWithId(context, request, creatorEmail, 'SmokePass123!', 'Peer Creator')
  const session = await createSession(request, 'QA Delete Peer Tombstone E2E')
  await pasteMaterial(request, session.id, 'Material', 'Quarterly review notes.')

  const QUESTION_TEXT = 'What were the highlights this quarter?'
  const seeded = await askQuestion(request, session.id, QUESTION_TEXT)

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
  const inv = await createInvitation(request, session.id, participantEmail, 'participant')
  const tokenQs = inv.accept_url.includes('?') ? inv.accept_url.split('?')[1] : ''
  const token = new URLSearchParams(tokenQs).get('token')
  const accept = await participantApi.post(`${API_BASE}/api/invitations/accept`, { data: { token } })
  expect(accept.ok()).toBe(true)

  const participantBrowserCtx = await browser.newContext()
  const loginRes = await participantApi.post(`${API_BASE}/api/auth/login`, {
    data: { email: participantEmail, password: 'SmokePass123!' },
  })
  await injectCookiesFromResponse(participantBrowserCtx, loginRes)
  const peerPage = await participantBrowserCtx.newPage()
  await peerPage.goto(`/?session=${session.id}&mode=view`)
  await peerPage.waitForLoadState('networkidle')

  await expect(peerPage.locator('[data-testid="question-item"]').filter({ hasText: QUESTION_TEXT }).first()).toBeVisible({ timeout: 15_000 })

  await page.goto(`/?session=${session.id}&mode=edit`)
  await page.waitForLoadState('networkidle')
  const creatorTrigger = page.getByTestId(`delete-question-trigger-${seeded.id}`)
  await expect(creatorTrigger).toBeVisible({ timeout: 15_000 })

  await creatorTrigger.click()
  await page.getByTestId('delete-question-confirm').click()

  // DELETE fires immediately; the WS broadcast lands at the peer once the server processes it.
  const tombstone = peerPage.getByTestId('question-tombstone').first()
  await expect(tombstone).toBeVisible({ timeout: 8_000 })
  await expect(tombstone).toContainText(/deleted/i)

  await participantBrowserCtx.close()
  await participantApi.dispose()
  await loginAsAdmin(request)
  await deleteSession(request, session.id)
  await deleteUserViaAdmin(request, creatorId)
  await deleteUserViaAdmin(request, participantId)
})

// --- Spec 4: participant has no trash icon on any question ---
test('participant view: no delete affordance is shown on any question', async ({ page, context, request, browser }) => {
  const creatorEmail = uniqueEmail('q-delete-noperm-creator')
  const creatorId = await createUserAndLoginWithId(context, request, creatorEmail, 'SmokePass123!', 'NoPerm Creator')
  const session = await createSession(request, 'QA Delete No-Permission E2E')
  await pasteMaterial(request, session.id, 'Material', 'Some material content.')

  const QUESTION_TEXT = 'A participant-visible question'
  await askQuestion(request, session.id, QUESTION_TEXT)

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

  const card = participantPage.locator('[data-testid="question-item"]').filter({ hasText: QUESTION_TEXT }).first()
  await expect(card).toBeVisible({ timeout: 15_000 })
  const triggerCount = await participantPage.locator('[data-testid^="delete-question-trigger-"]').count()
  expect(triggerCount).toBe(0)

  await partCtx.close()
  await participantApi.dispose()
  await loginAsAdmin(request)
  await deleteSession(request, session.id)
  await deleteUserViaAdmin(request, creatorId)
  await deleteUserViaAdmin(request, participantId)
})

// --- Spec 5: reply has no trash icon in creator view ---
test('reply has no delete affordance in creator view (only root cards)', async ({ page, context, request }) => {
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

  const rootTrigger = page.locator(`[data-testid="delete-question-trigger-${root.id}"]`)
  await expect(rootTrigger).toBeVisible({ timeout: 15_000 })

  const replyBody = await replyRes.json()
  const replyId = (replyBody?.question?.id ?? replyBody?.id) as string
  expect(replyId).toBeTruthy()
  const expandBtn = page.locator(`button[aria-label="Expand"]`).first()
  if (await expandBtn.isVisible({ timeout: 1_000 }).catch(() => false)) {
    await expandBtn.click()
  }
  await expect(page.locator(`[data-testid="delete-question-trigger-${replyId}"]`)).toHaveCount(0)

  await loginAsAdmin(request)
  await deleteSession(request, session.id)
  await deleteUserViaAdmin(request, userId)
})

// --- Spec 6: error path — server returns 404 (already deleted) → optimistic stays committed, no error toast ---
test('error path: server returns 404 (already deleted) → row stays gone, no error toast', async ({ page, context, request }) => {
  const email = uniqueEmail('q-delete-404')
  const userId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', '404 Creator')
  const session = await createSession(request, 'QA Delete 404 E2E')
  await pasteMaterial(request, session.id, 'Material', '404 path material.')

  const QUESTION_TEXT = 'Already-deleted-elsewhere question'
  const seeded = await askQuestion(request, session.id, QUESTION_TEXT)

  // Stub the DELETE to return 404 from the network layer.
  await page.route(`**/api/sessions/${session.id}/questions/${seeded.id}`, async (route) => {
    if (route.request().method() === 'DELETE') {
      await route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ error: 'gone' }) })
      return
    }
    await route.continue()
  })

  await page.goto(`/?session=${session.id}&mode=edit`)
  await page.waitForLoadState('networkidle')

  const trigger = page.getByTestId(`delete-question-trigger-${seeded.id}`)
  await expect(trigger).toBeVisible({ timeout: 15_000 })
  await trigger.click()

  const deletePromise = page.waitForResponse(
    (resp) => resp.url().includes(`/sessions/${session.id}/questions/${seeded.id}`) && resp.request().method() === 'DELETE'
  )
  await page.getByTestId('delete-question-confirm').click()
  const deleteResp = await deletePromise
  expect(deleteResp.status()).toBe(404)

  // Optimistic removal stays committed; no error toast on 404.
  await expect(page.getByTestId('delete-question-error-toast')).toHaveCount(0)
  await expect(page.getByTestId(`delete-question-trigger-${seeded.id}`)).toHaveCount(0)

  await page.unroute(`**/api/sessions/${session.id}/questions/${seeded.id}`)
  await loginAsAdmin(request)
  await deleteSession(request, session.id)
  await deleteUserViaAdmin(request, userId)
})

// --- Spec 7 (auth-scope): global admin who is NOT the session creator can delete ---
test('global admin (not session creator) sees the delete affordance and can delete a root question', async ({ page, context, request }) => {
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

  const adminLoginRes = await request.post(`${API_BASE}/api/auth/login`, {
    data: { email: ADMIN_EMAIL, password: ADMIN_PASSWORD },
  })
  if (!adminLoginRes.ok()) {
    test.skip(true, `bootstrap admin (${ADMIN_EMAIL}) is not present in this API; set TALKBACK_ADMIN_EMAIL/TALKBACK_ADMIN_PASSWORD to a valid admin to run this spec`)
  }
  await injectCookiesFromResponse(context, adminLoginRes)

  await page.goto(`/?session=${session.id}&mode=edit`)
  await page.waitForLoadState('networkidle')

  const trigger = page.getByTestId(`delete-question-trigger-${seeded.id}`)
  await expect(trigger).toBeVisible({ timeout: 15_000 })
  await trigger.click()
  await expect(page.getByTestId('delete-question-modal')).toBeVisible()

  const deletePromise = page.waitForResponse(
    (resp) => resp.url().includes(`/sessions/${session.id}/questions/${seeded.id}`) && resp.request().method() === 'DELETE'
  )
  await page.getByTestId('delete-question-confirm').click()
  const deleteResp = await deletePromise
  expect(deleteResp.status()).toBe(204)

  await expect(page.getByTestId(`delete-question-trigger-${seeded.id}`)).toHaveCount(0)

  const verify = await creatorApi.get(`${API_BASE}/sessions/${session.id}/questions`)
  expect(verify.ok()).toBe(true)
  const verifyBody = await verify.json()
  const stillThere = (verifyBody.questions || []).find((q: { question_text: string }) => q.question_text === QUESTION_TEXT)
  expect(stillThere).toBeFalsy()

  await creatorApi.dispose()
  await loginAsAdmin(request)
  await deleteSession(request, session.id)
  await deleteUserViaAdmin(request, creatorId)
})
