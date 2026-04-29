import { test, expect, request as playwrightApiRequest } from '@playwright/test'
import {
  API_BASE,
  createInvitation,
  createSession,
  createUserAndLoginWithId,
  deleteSession,
  deleteUserViaAdmin,
  loginAsAdmin,
  setSessionPrimaryDecision,
  uniqueEmail,
} from './fixtures'

test.describe.configure({ mode: 'serial' })
test.setTimeout(120_000)

let creatorId = ''
let participantId = ''
let sessionId = ''

test.afterAll(async ({ request }) => {
  await loginAsAdmin(request)
  if (sessionId) await deleteSession(request, sessionId)
  if (creatorId) await deleteUserViaAdmin(request, creatorId)
  if (participantId) await deleteUserViaAdmin(request, participantId)
})

test(
  'creator promotes a participant to decision_maker; readiness payload reflects new total',
  async ({ context, page, request }) => {
    const creatorEmail = uniqueEmail('role-change-creator')
    creatorId = await createUserAndLoginWithId(context, request, creatorEmail, 'SmokePass123!', 'Role Change Creator')

    const session = await createSession(request, 'Member Role Change E2E Session')
    sessionId = session.id
    await setSessionPrimaryDecision(request, session.id, 'Promote a participant?')

    // Sign up a separate user, accept the invitation as a participant, and capture the user id.
    const participantEmail = uniqueEmail('role-change-participant')
    const participantReq = await playwrightApiRequest.newContext()
    const signupRes = await participantReq.post(`${API_BASE}/api/auth/signup`, {
      data: { email: participantEmail, password: 'SmokePass123!', display_name: 'Promotee' },
    })
    expect(signupRes.ok()).toBe(true)
    const signupData = await signupRes.json()
    participantId = signupData.id as string
    const loginRes = await participantReq.post(`${API_BASE}/api/auth/login`, {
      data: { email: participantEmail, password: 'SmokePass123!' },
    })
    expect(loginRes.ok()).toBe(true)

    const inv = await createInvitation(request, session.id, participantEmail, 'participant')
    const tokenQs = inv.accept_url.includes('?') ? inv.accept_url.split('?')[1] : ''
    const token = new URLSearchParams(tokenQs).get('token')
    const accept = await participantReq.post(`${API_BASE}/api/invitations/accept`, {
      data: { token },
    })
    expect(accept.ok()).toBe(true)

    // Baseline readiness: 0 decision_makers, since the only member is a participant.
    const baselineStances = await participantReq.get(`${API_BASE}/api/sessions/${session.id}/stances`)
    expect(baselineStances.ok()).toBe(true)
    const baseline = await baselineStances.json()
    expect(baseline?.readiness?.decision_maker_total).toBe(0)

    // Creator PATCHes the membership to promote the participant to decision_maker.
    const patchRes = await request.patch(`${API_BASE}/api/sessions/${session.id}/memberships/${participantId}`, {
      data: { role: 'decision_maker' },
    })
    expect(patchRes.status()).toBe(200)
    const patchBody = await patchRes.json()
    expect(patchBody?.membership?.role).toBe('decision_maker')

    // Readiness now reports 1 decision_maker total, 0 voted.
    const promotedStances = await participantReq.get(`${API_BASE}/api/sessions/${session.id}/stances`)
    expect(promotedStances.ok()).toBe(true)
    const promoted = await promotedStances.json()
    expect(promoted?.readiness?.decision_maker_total).toBe(1)
    expect(promoted?.readiness?.decision_maker_voted).toBe(0)
    expect(promoted?.readiness?.ready_to_close).toBe(false)

    // The new decision_maker submits a stance — readiness flips to ready.
    const stanceRes = await participantReq.post(`${API_BASE}/api/sessions/${session.id}/stance`, {
      data: { stance: 'agree' },
    })
    expect(stanceRes.ok()).toBe(true)
    const stancePayload = await stanceRes.json()
    expect(stancePayload?.readiness?.decision_maker_total).toBe(1)
    expect(stancePayload?.readiness?.decision_maker_voted).toBe(1)
    expect(stancePayload?.readiness?.ready_to_close).toBe(true)

    await participantReq.dispose()
    void page // keep the browser fixture so afterAll cleanup runs identically
  }
)

test(
  'a non-creator membership-role PATCH is rejected with 403',
  async ({ request }) => {
    // Fresh creator/session/intruder so this test is independent of the happy path.
    const creatorEmail = uniqueEmail('role-403-creator')
    const creatorReq = await playwrightApiRequest.newContext()
    const cSignup = await creatorReq.post(`${API_BASE}/api/auth/signup`, {
      data: { email: creatorEmail, password: 'SmokePass123!', display_name: 'Forbidden Creator' },
    })
    expect(cSignup.ok()).toBe(true)
    const cData = await cSignup.json()
    await creatorReq.post(`${API_BASE}/api/auth/login`, {
      data: { email: creatorEmail, password: 'SmokePass123!' },
    })
    const localSession = await createSession(creatorReq, 'Forbidden Role Change E2E Session')

    // Invite an intruder and have them accept so they have a membership row.
    const intruderEmail = uniqueEmail('role-403-intruder')
    const intruderReq = await playwrightApiRequest.newContext()
    const iSignup = await intruderReq.post(`${API_BASE}/api/auth/signup`, {
      data: { email: intruderEmail, password: 'SmokePass123!', display_name: 'Intruder' },
    })
    expect(iSignup.ok()).toBe(true)
    const iData = await iSignup.json()
    await intruderReq.post(`${API_BASE}/api/auth/login`, {
      data: { email: intruderEmail, password: 'SmokePass123!' },
    })

    const inv = await createInvitation(creatorReq, localSession.id, intruderEmail, 'participant')
    const tokenQs = inv.accept_url.includes('?') ? inv.accept_url.split('?')[1] : ''
    const token = new URLSearchParams(tokenQs).get('token')
    await intruderReq.post(`${API_BASE}/api/invitations/accept`, { data: { token } })

    // The intruder tries to PATCH their own role — must 403.
    const patchRes = await intruderReq.patch(
      `${API_BASE}/api/sessions/${localSession.id}/memberships/${iData.id}`,
      { data: { role: 'creator' } }
    )
    expect(patchRes.status()).toBe(403)

    // Cleanup local seed.
    await loginAsAdmin(request)
    await deleteSession(request, localSession.id)
    await deleteUserViaAdmin(request, cData.id as string)
    await deleteUserViaAdmin(request, iData.id as string)

    await creatorReq.dispose()
    await intruderReq.dispose()
  }
)
