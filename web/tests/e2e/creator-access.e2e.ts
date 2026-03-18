import { test, expect } from '@playwright/test'
import {
  API_BASE,
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

  // --- Navigate to session in edit (creator) mode ---
  // Pass api= so the app uses the same backend we used (critical on Render: frontend must call the API service).
  const params = new URLSearchParams({ session: session.id, mode: 'edit', api: API_BASE })
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
})
