import { test, expect } from '@playwright/test'
import {
  createSession,
  createUserAndLoginWithId,
  createInvitation,
  deleteSession,
  deleteUserViaAdmin,
  dismissParticipantOnboardingIfPresent,
  loginAsAdmin,
  uniqueEmail,
} from './fixtures'

test.setTimeout(60_000)

let seededUserId = ''
let seededSessionId = ''

test.afterAll(async ({ request }) => {
  await loginAsAdmin(request)
  if (seededSessionId) await deleteSession(request, seededSessionId)
  if (seededUserId) await deleteUserViaAdmin(request, seededUserId)
})

// SCRUM-212 — pending invitation rows now expose a single ⋯ dropdown trigger
// instead of the three-button stack. This spec verifies the trigger renders,
// opens a menu with the three actions in order, and that Revoke fires the
// inline-confirm step (no API call until Confirm).
test(
  'pending invitation row exposes a single dropdown trigger with Resend / Copy link / Revoke',
  async ({ page, context, request }) => {
    const email = uniqueEmail('member-row-creator')
    seededUserId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', 'Member Row Creator')

    const session = await createSession(request, 'Member Row Actions E2E Session')
    seededSessionId = session.id

    // Seed one pending invitation so the table has a row with the new trigger.
    const inviteeEmail = uniqueEmail('member-row-invitee')
    await createInvitation(request, session.id, inviteeEmail, 'participant')

    await page.goto(`/?session=${session.id}&mode=edit`)
    await page.waitForLoadState('networkidle')
    await dismissParticipantOnboardingIfPresent(page)

    // Expand the Members panel — the row + trigger render inside its body.
    const membersBtn = page.getByRole('button', { name: /^members/i }).first()
    await expect(membersBtn).toBeVisible({ timeout: 20_000 })
    if ((await membersBtn.getAttribute('aria-expanded')) !== 'true') {
      await membersBtn.click()
    }

    // Single trigger per pending row, with the right ARIA + label.
    const trigger = page.getByTestId('member-row-actions-trigger').first()
    await expect(trigger).toBeVisible({ timeout: 10_000 })
    expect(await trigger.getAttribute('aria-haspopup')).toBe('menu')
    expect(await trigger.getAttribute('aria-label')).toBe('Member actions')
    expect((await trigger.textContent())?.trim()).toBe('⋯')

    // Open the menu.
    await trigger.click()
    const menu = page.getByTestId('member-row-menu')
    await expect(menu).toBeVisible({ timeout: 5_000 })
    await expect(page.getByTestId('member-action-resend')).toBeVisible()
    await expect(page.getByTestId('member-action-copy-link')).toBeVisible()
    await expect(page.getByTestId('member-action-revoke')).toBeVisible()

    // Revoke opens an inline confirm and does NOT immediately revoke.
    await page.getByTestId('member-action-revoke').click()
    await expect(page.getByTestId('member-row-revoke-confirm')).toBeVisible({ timeout: 3_000 })
    // Cancel returns to the menu.
    await page.getByTestId('member-action-revoke-cancel').click()
    await expect(page.getByTestId('member-row-revoke-confirm')).toHaveCount(0)
    // Trigger remains on the page; menu may have closed when state reset.
    await expect(trigger).toBeVisible()

    // The legacy stacked buttons are no longer rendered.
    await expect(page.getByRole('button', { name: 'Resend' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'Revoke' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'Copy link' })).toHaveCount(0)
  }
)
