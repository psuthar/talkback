import { test, expect } from '@playwright/test'
import {
  createSession,
  createUserAndLoginWithId,
  deleteSession,
  deleteUserViaAdmin,
  loginAsAdmin,
  uniqueEmail,
} from './fixtures'

// Page load + creator UI render — no LLM call. 30s local; Render can be slower (cold start).
test.setTimeout(60_000)

// Track seeded IDs for afterAll cleanup.
let seededUserId = ''
let seededSessionId = ''

test.afterAll(async ({ request }) => {
  await loginAsAdmin(request)
  if (seededSessionId) await deleteSession(request, seededSessionId)
  if (seededUserId) await deleteUserViaAdmin(request, seededUserId)
})

test('creator opens session in edit mode, sees creator-only UI', async ({ page, context, request }) => {
  // --- Seed: creator user + session via API ---
  const email = uniqueEmail('creator')
  seededUserId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', 'Creator User')

  const session = await createSession(request, 'E2E Creator Access Session')
  seededSessionId = session.id

  // --- Navigate to session in edit (creator) mode (API base from Playwright storage state / VITE) ---
  const params = new URLSearchParams({ session: session.id, mode: 'edit' })
  await page.goto(`/?${params.toString()}`)
  await page.waitForLoadState('networkidle')

  // --- Assert: "Add content" collapsible button — unique to creator mode ---
  // The AddContentSection renders a button with text "Add content" (aria-expanded, class creator-collapsible-btn).
  const addContentBtn = page.getByRole('button', { name: /add content/i })
  try {
    await expect(addContentBtn).toBeVisible({ timeout: 20_000 })
  } catch {
    // If the app showed the login page, the auth cookie wasn't sent (cross-origin). Tell the user how to fix.
    const loginVisible = await page.getByText(/Sign in to access sessions/i).isVisible().catch(() => false)
    if (loginVisible) {
      throw new Error(
        'Creator UI not found and login page is visible. On Render API (talkback-895n) set TB_COOKIE_SECURE=1 and TB_ALLOWED_ORIGINS=https://talkback-ux.onrender.com (name is TB_COOKIE_SECURE not TB_SECURE_COOKIE). Leave TB_COOKIE_DOMAIN unset. Redeploy and check API logs for "cookie SameSite=None".'
      )
    }
    throw new Error('Creator "Add content" button did not appear within 20s. Check that the session loaded and creator mode is active.')
  }

  // --- Assert: creator left panel exists (CSS class unique to creator layout) ---
  const creatorPanel = page.locator('.creator-left-panel')
  await expect(creatorPanel).toBeVisible({ timeout: 10_000 })

  // --- Assert: Members collapsible button (invite UI) is present ---
  const membersBtn = page.getByRole('button', { name: /members/i })
  await expect(membersBtn).toBeVisible({ timeout: 10_000 })

  // --- SCRUM-186: creator topbar shows participant-style title + Creator badge + Session menu trigger ---
  const creatorBadge = page.locator('.creator-layout-topbar').getByText('Creator', { exact: true })
  await expect(creatorBadge).toBeVisible({ timeout: 10_000 })

  const menuTrigger = page.getByTestId('participant-session-menu-btn')
  await expect(menuTrigger).toBeVisible({ timeout: 10_000 })
  expect(await menuTrigger.getAttribute('aria-label')).toBe('Session menu')

  // --- SCRUM-186: opening the menu reveals identity + Show all sessions + debug + logout ---
  await menuTrigger.click()
  const menu = page.getByTestId('participant-session-menu')
  await expect(menu).toBeVisible({ timeout: 3_000 })
  await expect(menu).toContainText(email)
  await expect(page.getByTestId('menu-show-all-sessions')).toBeVisible()
  await expect(page.getByTestId('menu-debug-toggle')).toBeVisible()
  await expect(page.getByTestId('menu-logout')).toBeVisible()

  // --- SCRUM-186: legacy red "Show All Sessions" inline button no longer rendered inside the topbar ---
  await page.keyboard.press('Escape')
  await expect(page.getByTestId('participant-session-menu')).toHaveCount(0)
  const inlineShowAllBtn = page
    .locator('.creator-layout-topbar')
    .getByRole('button', { name: /show all sessions/i })
  await expect(inlineShowAllBtn).toHaveCount(0)
})
