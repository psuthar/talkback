import { test, expect, request as playwrightApiRequest } from '@playwright/test'
import {
  API_BASE,
  createSession,
  createUserAndLoginWithId,
  createInvitation,
  deleteSession,
  deleteUserViaAdmin,
  dismissParticipantOnboardingIfPresent,
  injectCookiesFromResponse,
  loginAsAdmin,
  setSessionPrimaryDecision,
  uniqueEmail,
} from './fixtures'

test.describe.configure({ mode: 'serial' })
test.setTimeout(120_000)

let creatorId = ''
let dmUserId = ''
let sessionId = ''

test.afterAll(async ({ request }) => {
  await loginAsAdmin(request)
  if (sessionId) await deleteSession(request, sessionId)
  if (creatorId) await deleteUserViaAdmin(request, creatorId)
  if (dmUserId) await deleteUserViaAdmin(request, dmUserId)
})

test(
  'happy path: creator assigns Decision Maker, DM votes, creator closes; both views land in read-only state',
  async ({ browser, context, page, request }) => {
    // --- Creator signup + login + session + primary_decision ---
    const creatorEmail = uniqueEmail('dm-closeout-creator')
    creatorId = await createUserAndLoginWithId(context, request, creatorEmail, 'SmokePass123!', 'DM Closeout Creator')

    const session = await createSession(request, 'Decision Maker Closeout E2E Session')
    sessionId = session.id
    await setSessionPrimaryDecision(request, session.id, 'Should we ship the Decision Maker feature?')

    // --- Decision Maker signup, login (separate API context), invite acceptance, stance ---
    const dmEmail = uniqueEmail('dm-closeout-dm')
    const dmRequest = await playwrightApiRequest.newContext()
    const dmSignupRes = await dmRequest.post(`${API_BASE}/api/auth/signup`, {
      data: { email: dmEmail, password: 'SmokePass123!', display_name: 'DM Voter' },
    })
    expect(dmSignupRes.ok()).toBe(true)
    const dmSignup = await dmSignupRes.json()
    dmUserId = dmSignup.id as string
    const dmLoginRes = await dmRequest.post(`${API_BASE}/api/auth/login`, {
      data: { email: dmEmail, password: 'SmokePass123!' },
    })
    expect(dmLoginRes.ok()).toBe(true)

    const invitation = await createInvitation(request, session.id, dmEmail, 'decision_maker')
    expect(invitation.invited_email).toBe(dmEmail.toLowerCase())

    // DM accepts via /api/invitations/accept with the raw token (already-logged-in path).
    const qs = invitation.accept_url.includes('?') ? invitation.accept_url.split('?')[1] : ''
    const token = new URLSearchParams(qs).get('token')
    expect(token).toBeTruthy()
    const acceptRes = await dmRequest.post(`${API_BASE}/api/invitations/accept`, {
      data: { token },
    })
    expect(acceptRes.ok()).toBe(true)

    // DM submits a stance — flips readiness.ready_to_close to true.
    const stanceRes = await dmRequest.post(`${API_BASE}/api/sessions/${session.id}/stance`, {
      data: { stance: 'agree', rationale: 'Aligns with Q2 roadmap' },
    })
    expect(stanceRes.ok()).toBe(true)
    const stancePayload = await stanceRes.json()
    expect(stancePayload?.readiness?.decision_maker_total).toBe(1)
    expect(stancePayload?.readiness?.decision_maker_voted).toBe(1)
    expect(stancePayload?.readiness?.ready_to_close).toBe(true)

    // --- Creator browser navigates to session in edit mode ---
    await page.goto(`/?session=${session.id}&mode=edit`)
    await page.waitForLoadState('networkidle')
    await dismissParticipantOnboardingIfPresent(page)

    // Readiness row reports ready_to_close=true.
    const readiness = page.getByTestId('decision-readiness')
    await expect(readiness).toBeVisible({ timeout: 15_000 })
    await expect(readiness).toHaveAttribute('data-ready', 'true')
    await expect(readiness).toHaveText(/1\/1 Decision Makers submitted/)

    // Close-decision CTA appears for the creator.
    const closeBtn = page.getByTestId('close-decision-btn')
    await expect(closeBtn).toBeVisible({ timeout: 5_000 })
    await closeBtn.click()

    // Inline form: fill outcome and submit.
    const outcomeText = 'Approved — proceed to ship.'
    const outcomeInput = page.getByTestId('close-decision-input')
    await expect(outcomeInput).toBeVisible({ timeout: 5_000 })
    await outcomeInput.fill(outcomeText)
    await page.getByTestId('close-decision-confirm').click()

    // Outcome row appears; close button is gone; readiness row is suppressed.
    const outcomeBadge = page.getByTestId('decision-outcome-badge')
    await expect(outcomeBadge).toBeVisible({ timeout: 10_000 })
    await expect(outcomeBadge).toContainText(outcomeText)
    await expect(page.getByTestId('close-decision-btn')).toHaveCount(0)
    await expect(page.getByTestId('decision-readiness')).toHaveCount(0)

    // Creator's DecisionBar carries the post-closeout lockedNote.
    await expect(page.getByTestId('decision-bar-locked')).toBeVisible({ timeout: 5_000 })

    // --- Server-side stance lock: DM's POST stance now returns 409 ---
    const lockedRes = await dmRequest.post(`${API_BASE}/api/sessions/${session.id}/stance`, {
      data: { stance: 'disagree' },
    })
    expect(lockedRes.status()).toBe(409)

    // --- Switch the browser to DM's identity and verify the participant view ---
    await context.clearCookies()
    const reLoginRes = await request.post(`${API_BASE}/api/auth/login`, {
      data: { email: dmEmail, password: 'SmokePass123!' },
    })
    expect(reLoginRes.ok()).toBe(true)
    await injectCookiesFromResponse(context, reLoginRes)

    await page.goto(`/?session=${session.id}&mode=view`)
    await page.waitForLoadState('networkidle')
    await dismissParticipantOnboardingIfPresent(page)

    // Participant view shows the outcome and the same locked-stance note.
    const pOutcome = page.getByTestId('decision-outcome-badge')
    await expect(pOutcome).toBeVisible({ timeout: 15_000 })
    await expect(pOutcome).toContainText(outcomeText)
    await expect(page.getByTestId('decision-bar-locked')).toBeVisible({ timeout: 5_000 })
    // No close action and no readiness row in the participant view.
    await expect(page.getByTestId('close-decision-btn')).toHaveCount(0)
    await expect(page.getByTestId('decision-readiness')).toHaveCount(0)

    await dmRequest.dispose()

    // Hold the browser shut — the next test reuses sessionId via afterAll cleanup.
    void browser
  }
)

test(
  'unauthorized closeout: a Decision Maker (non-creator/non-admin) cannot record decision_outcome',
  async ({ request }) => {
    // Spin up a fresh creator/session/DM to keep this test independent of any state
    // mutated by the happy-path test above.
    const localCreatorEmail = uniqueEmail('dm-unauth-creator')
    const creatorReq = await playwrightApiRequest.newContext()
    const creatorSignup = await creatorReq.post(`${API_BASE}/api/auth/signup`, {
      data: { email: localCreatorEmail, password: 'SmokePass123!', display_name: 'Unauth Creator' },
    })
    expect(creatorSignup.ok()).toBe(true)
    const creatorSignupData = await creatorSignup.json()
    await creatorReq.post(`${API_BASE}/api/auth/login`, {
      data: { email: localCreatorEmail, password: 'SmokePass123!' },
    })

    const localSession = await createSession(creatorReq, 'Unauthorized Closeout E2E Session')
    await setSessionPrimaryDecision(creatorReq, localSession.id, 'Authorize anyone?')

    const dmEmailLocal = uniqueEmail('dm-unauth-dm')
    const dmReq = await playwrightApiRequest.newContext()
    const dmSignup = await dmReq.post(`${API_BASE}/api/auth/signup`, {
      data: { email: dmEmailLocal, password: 'SmokePass123!', display_name: 'Unauth DM' },
    })
    expect(dmSignup.ok()).toBe(true)
    const dmSignupData = await dmSignup.json()
    await dmReq.post(`${API_BASE}/api/auth/login`, {
      data: { email: dmEmailLocal, password: 'SmokePass123!' },
    })

    const inv = await createInvitation(creatorReq, localSession.id, dmEmailLocal, 'decision_maker')
    const tokenQs = inv.accept_url.includes('?') ? inv.accept_url.split('?')[1] : ''
    const inviteToken = new URLSearchParams(tokenQs).get('token')
    expect(inviteToken).toBeTruthy()
    const accept = await dmReq.post(`${API_BASE}/api/invitations/accept`, {
      data: { token: inviteToken },
    })
    expect(accept.ok()).toBe(true)

    // The DM is now a member with role=decision_maker. Attempting the closeout PATCH must 403.
    const patchRes = await dmReq.patch(`${API_BASE}/api/sessions/${localSession.id}`, {
      data: { decision_outcome: 'DM tries to close.' },
    })
    expect(patchRes.status()).toBe(403)

    // Cleanup local seed (best-effort; afterAll only knows about the happy-path IDs).
    await loginAsAdmin(request)
    await deleteSession(request, localSession.id)
    await deleteUserViaAdmin(request, creatorSignupData.id as string)
    await deleteUserViaAdmin(request, dmSignupData.id as string)

    await creatorReq.dispose()
    await dmReq.dispose()
  }
)
