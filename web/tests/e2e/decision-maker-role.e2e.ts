import { test, expect } from '@playwright/test'
import {
  createUserAndLoginWithId,
  createSession,
  deleteSession,
  deleteUserViaAdmin,
  dismissParticipantOnboardingIfPresent,
  loginAsAdmin,
  setSessionPrimaryDecision,
  uniqueEmail,
  API_BASE,
} from './fixtures'

test.setTimeout(60_000)

let seededUserId = ''
let seededSessionId = ''

test.afterAll(async ({ request }) => {
  await loginAsAdmin(request)
  if (seededSessionId) await deleteSession(request, seededSessionId)
  if (seededUserId) await deleteUserViaAdmin(request, seededUserId)
})

test(
  'creator can pick Decision Maker as an invitation role and the API persists it',
  async ({ page, context, request }) => {
    const email = uniqueEmail('dm-role')
    seededUserId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', 'DM Role Creator')

    const session = await createSession(request, 'Decision Maker Role E2E Session')
    seededSessionId = session.id

    // Creator (edit) mode is where the canonical Members panel + invite role select live.
    const params = new URLSearchParams({ session: session.id, mode: 'edit' })
    await page.goto(`/?${params.toString()}`)
    await page.waitForLoadState('networkidle')
    await dismissParticipantOnboardingIfPresent(page)

    // Expand the Members panel — the role select renders inside its expanded body.
    const membersBtn = page.getByRole('button', { name: /^members/i }).first()
    await expect(membersBtn).toBeVisible({ timeout: 20_000 })
    if ((await membersBtn.getAttribute('aria-expanded')) !== 'true') {
      await membersBtn.click()
    }

    // The creator's invite role dropdown must include the Decision Maker option.
    const roleSelect = page.getByLabel('Invite role').first()
    await expect(roleSelect).toBeVisible({ timeout: 10_000 })
    const optionValues = await roleSelect.evaluate((el: HTMLSelectElement) =>
      Array.from(el.options).map((o) => o.value)
    )
    expect(optionValues).toContain('decision_maker')

    // POST an invitation with role=decision_maker and assert the response carries it.
    const invited = uniqueEmail('dm-invitee')
    const res = await request.post(`${API_BASE}/api/sessions/${session.id}/invitations`, {
      data: { email: invited, role: 'decision_maker' },
    })
    expect(res.status()).toBe(201)
    const body = await res.json()
    expect(body?.invitation?.invited_role).toBe('decision_maker')
    expect(body?.invitation?.invited_email).toBe(invited.toLowerCase())

    // Close-decision CTA is gated by readiness: until at least one DM accepts AND votes,
    // ready_to_close is false and the button must not appear (server-side enforces auth as
    // a backstop; the comprehensive happy-path closeout flow is covered by SCRUM-206).
    await setSessionPrimaryDecision(request, session.id, 'Should we ship Decision Maker?')
    await page.reload()
    await page.waitForLoadState('networkidle')
    await dismissParticipantOnboardingIfPresent(page)
    await expect(page.getByTestId('decision-brief-header')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('close-decision-btn')).toHaveCount(0)
  }
)
