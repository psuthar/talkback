import { test, expect } from '@playwright/test'
import {
  createUserAndLoginWithId,
  createSession,
  deleteSession,
  deleteUserViaAdmin,
  dismissParticipantOnboardingIfPresent,
  loginAsAdmin,
  uniqueEmail,
} from './fixtures'

// Smoke E2E for the new ParticipantSessionMenu (SCRUM-157, refresh under SCRUM-159).
// Verifies the menu trigger appears in the participant top bar, opens on click,
// shows the user identity row, exposes the Show all sessions / Debug toggle / Log out
// items, and closes on Escape.
test.setTimeout(60_000)

let seededUserId = ''
let seededSessionId = ''

test.afterAll(async ({ request }) => {
  await loginAsAdmin(request)
  if (seededSessionId) await deleteSession(request, seededSessionId)
  if (seededUserId) await deleteUserViaAdmin(request, seededUserId)
})

test(
  'participant session menu exposes identity, sessions, debug, and logout',
  async ({ page, context, request }) => {
    const email = uniqueEmail('participant-menu')
    const displayName = 'Menu Test User'
    seededUserId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', displayName)

    const session = await createSession(request, 'Participant Session Menu E2E Session')
    seededSessionId = session.id

    const params = new URLSearchParams({ session: session.id, mode: 'view' })
    await page.goto(`/?${params.toString()}`)
    await page.waitForLoadState('networkidle')
    await dismissParticipantOnboardingIfPresent(page)

    // The trigger has an accessible label and a visible "Session menu" string.
    const trigger = page.getByTestId('participant-session-menu-btn')
    await expect(trigger).toBeVisible({ timeout: 10_000 })
    expect(await trigger.getAttribute('aria-label')).toBe('Session menu')
    expect(await trigger.getAttribute('aria-haspopup')).toBe('menu')
    expect(await trigger.getAttribute('aria-expanded')).toBe('false')

    // While the menu is closed, the App-header auth cluster must be hidden so we
    // do not duplicate identity / logout for participants. (SCRUM-157.)
    await expect(page.getByTestId('app-auth-cluster')).toHaveCount(0)

    // Open the menu and assert the identity row, Show all sessions, debug toggle, and logout.
    await trigger.click()
    const menu = page.getByTestId('participant-session-menu')
    await expect(menu).toBeVisible({ timeout: 3_000 })
    await expect(menu).toContainText(displayName)
    await expect(menu).toContainText(email)
    await expect(page.getByTestId('menu-show-all-sessions')).toBeVisible()
    await expect(page.getByTestId('menu-debug-toggle')).toBeVisible()
    await expect(page.getByTestId('menu-logout')).toBeVisible()

    // Escape closes the menu and returns aria-expanded to false.
    await page.keyboard.press('Escape')
    await expect(page.getByTestId('participant-session-menu')).toHaveCount(0)
    expect(await trigger.getAttribute('aria-expanded')).toBe('false')
  }
)
