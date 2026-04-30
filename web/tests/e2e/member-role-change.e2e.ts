import { test, expect, request as playwrightApiRequest } from '@playwright/test'
import {
  API_BASE,
  createInvitation,
  createSession,
  createUserAndLoginWithId,
  deleteSession,
  deleteUserViaAdmin,
  dismissParticipantOnboardingIfPresent,
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

    // SCRUM-217: drive the creator UI and assert the displayed role flipped to
    // the new role. Before the fix the listing API returned the original
    // invited_role and this assertion would fail because the cell still read
    // "Participant".
    await page.goto(`/?session=${session.id}&mode=edit`)
    await page.waitForLoadState('networkidle')
    await dismissParticipantOnboardingIfPresent(page)

    const membersBtn = page.getByRole('button', { name: /^members/i }).first()
    await expect(membersBtn).toBeVisible({ timeout: 20_000 })
    if ((await membersBtn.getAttribute('aria-expanded')) !== 'true') {
      await membersBtn.click()
    }

    // Locate the participant's row by their email and read the Role cell.
    const row = page.locator('tr', { hasText: participantEmail.toLowerCase() })
    await expect(row).toBeVisible({ timeout: 10_000 })
    await expect(row.locator('td').nth(1)).toHaveText('Decision Maker')

    // Open the row's action menu and assert Decision Maker is the disabled / ✓ item.
    await row.getByTestId('member-row-actions-trigger').click()
    const dmItem = page.getByTestId('member-action-role-decision_maker')
    await expect(dmItem).toBeDisabled()
    await expect(dmItem).toContainText('✓')

    await participantReq.dispose()
  }
)

test(
  'creator promotes a participant to creator; Members panel reflects new role on refetch (SCRUM-225)',
  async ({ context, page, request }) => {
    const creatorEmail = uniqueEmail('role-creator-promote')
    const localCreatorId = await createUserAndLoginWithId(context, request, creatorEmail, 'SmokePass123!', 'Creator Promotion Creator')

    const localSession = await createSession(request, 'Creator Promotion E2E Session')
    await setSessionPrimaryDecision(request, localSession.id, 'Promote a participant to creator?')

    const promoteeEmail = uniqueEmail('role-creator-promotee')
    const promoteeReq = await playwrightApiRequest.newContext()
    const signupRes = await promoteeReq.post(`${API_BASE}/api/auth/signup`, {
      data: { email: promoteeEmail, password: 'SmokePass123!', display_name: 'Promotee' },
    })
    expect(signupRes.ok()).toBe(true)
    const signupData = await signupRes.json()
    const promoteeId = signupData.id as string
    await promoteeReq.post(`${API_BASE}/api/auth/login`, {
      data: { email: promoteeEmail, password: 'SmokePass123!' },
    })

    const inv = await createInvitation(request, localSession.id, promoteeEmail, 'participant')
    const tokenQs = inv.accept_url.includes('?') ? inv.accept_url.split('?')[1] : ''
    const token = new URLSearchParams(tokenQs).get('token')
    const accept = await promoteeReq.post(`${API_BASE}/api/invitations/accept`, { data: { token } })
    expect(accept.ok()).toBe(true)

    // Creator PATCHes the membership to creator. This is the path that the bug
    // ticket reported as silently broken because the listing was sourced from
    // session_invitations.invited_role rather than session_memberships.role.
    const patchRes = await request.patch(`${API_BASE}/api/sessions/${localSession.id}/memberships/${promoteeId}`, {
      data: { role: 'creator' },
    })
    expect(patchRes.status()).toBe(200)
    const patchBody = await patchRes.json()
    expect(patchBody?.membership?.role).toBe('creator')

    // The listing endpoint must reflect 'creator' on the accepted row immediately.
    const listingRes = await request.get(`${API_BASE}/api/sessions/${localSession.id}/invitations`)
    expect(listingRes.ok()).toBe(true)
    const listing = await listingRes.json()
    const acceptedRow = (listing?.invitations || []).find((row: { invited_email?: string }) =>
      (row?.invited_email || '').toLowerCase() === promoteeEmail.toLowerCase()
    )
    expect(acceptedRow?.invited_role).toBe('creator')

    // Drive the creator UI and confirm the Role cell + the menu's ✓ item moved to Creator.
    await page.goto(`/?session=${localSession.id}&mode=edit`)
    await page.waitForLoadState('networkidle')
    await dismissParticipantOnboardingIfPresent(page)

    const membersBtn = page.getByRole('button', { name: /^members/i }).first()
    await expect(membersBtn).toBeVisible({ timeout: 20_000 })
    if ((await membersBtn.getAttribute('aria-expanded')) !== 'true') {
      await membersBtn.click()
    }

    const row = page.locator('tr', { hasText: promoteeEmail.toLowerCase() })
    await expect(row).toBeVisible({ timeout: 10_000 })
    await expect(row.locator('td').nth(1)).toHaveText('Creator')

    await row.getByTestId('member-row-actions-trigger').click()
    const creatorItem = page.getByTestId('member-action-role-creator')
    await expect(creatorItem).toBeDisabled()
    await expect(creatorItem).toContainText('✓')

    // Cleanup local seed (this test owns its own session/users).
    await loginAsAdmin(request)
    await deleteSession(request, localSession.id)
    await deleteUserViaAdmin(request, localCreatorId)
    await deleteUserViaAdmin(request, promoteeId)

    await promoteeReq.dispose()
  }
)

test(
  'creator is listed in Members panel of a freshly created session and member_count includes them (SCRUM-226)',
  async ({ context, page, request }) => {
    const creatorEmail = uniqueEmail('creator-self-listing')
    const localCreatorId = await createUserAndLoginWithId(context, request, creatorEmail, 'SmokePass123!', 'Self Listing Creator')

    const localSession = await createSession(request, 'Creator Self Listing E2E Session')

    // Listing endpoint must include the creator with role='creator', status='accepted'.
    const listingRes = await request.get(`${API_BASE}/api/sessions/${localSession.id}/invitations`)
    expect(listingRes.ok()).toBe(true)
    const listing = await listingRes.json()
    const rows: Array<{ invited_email?: string; invited_role?: string; status?: string }> =
      listing?.invitations || []
    const creatorRow = rows.find((row) => (row?.invited_email || '').toLowerCase() === creatorEmail.toLowerCase())
    expect(creatorRow).toBeDefined()
    expect(creatorRow?.invited_role).toBe('creator')
    expect(creatorRow?.status).toBe('accepted')

    // member_count on Session Selection cards should include the creator (off-by-one regression check).
    const sessionsRes = await request.get(`${API_BASE}/api/sessions`)
    expect(sessionsRes.ok()).toBe(true)
    const sessions: Array<{ id?: string; member_count?: number }> = await sessionsRes.json()
    const ownedRow = sessions.find((s) => s?.id === localSession.id)
    expect(ownedRow).toBeDefined()
    expect(ownedRow?.member_count).toBeGreaterThanOrEqual(1)

    // Drive the UI: creator opens their own session and sees themselves in the Members panel.
    await page.goto(`/?session=${localSession.id}&mode=edit`)
    await page.waitForLoadState('networkidle')
    await dismissParticipantOnboardingIfPresent(page)

    const membersBtn = page.getByRole('button', { name: /^members/i }).first()
    await expect(membersBtn).toBeVisible({ timeout: 20_000 })
    if ((await membersBtn.getAttribute('aria-expanded')) !== 'true') {
      await membersBtn.click()
    }

    const creatorRowLocator = page.locator('tr', { hasText: creatorEmail.toLowerCase() })
    await expect(creatorRowLocator).toBeVisible({ timeout: 10_000 })
    await expect(creatorRowLocator.locator('td').nth(1)).toHaveText('Creator')

    // Cleanup
    await loginAsAdmin(request)
    await deleteSession(request, localSession.id)
    await deleteUserViaAdmin(request, localCreatorId)
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
