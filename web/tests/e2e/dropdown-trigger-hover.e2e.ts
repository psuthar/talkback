import { test, expect } from '@playwright/test'
import {
  createSession,
  createUserAndLoginWithId,
  deleteSession,
  deleteUserViaAdmin,
  dismissParticipantOnboardingIfPresent,
  loginAsAdmin,
  setSessionPrimaryDecision,
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

// SCRUM-211 — the global `button:hover` rule in index.css used to paint
// var(--color-primary-dark) (#1565C0) over every styled button, including the
// stance dropdown trigger, hiding the label. This spec hovers the trigger and
// asserts the computed background does not match that primary-dark color.
test(
  'stance dropdown trigger does not turn solid primary-dark on hover',
  async ({ page, context, request }) => {
    const email = uniqueEmail('trigger-hover')
    seededUserId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', 'Trigger Hover')

    const session = await createSession(request, 'Trigger Hover E2E Session')
    seededSessionId = session.id
    await setSessionPrimaryDecision(request, session.id, 'Should we hover-test?')

    await page.goto(`/?session=${session.id}&mode=view`)
    await page.waitForLoadState('networkidle')
    await dismissParticipantOnboardingIfPresent(page)

    const trigger = page.getByTestId('stance-trigger-btn')
    await expect(trigger).toBeVisible({ timeout: 15_000 })
    await trigger.hover()

    const bg = await trigger.evaluate((el) => getComputedStyle(el).backgroundColor)
    // --color-primary-dark = #1565C0 = rgb(21, 101, 192). This is the legacy
    // bleed color we explicitly do not want.
    expect(bg).not.toBe('rgb(21, 101, 192)')
    // Sanity: alpha must not be transparent — that would mean no rule applied.
    expect(bg).not.toBe('rgba(0, 0, 0, 0)')

    // The label remains visible (defensive — visibility is not a function of
    // background, but verifies the label wasn't overlaid by something else).
    await expect(trigger).toContainText(/stance|choose/i)
  }
)
