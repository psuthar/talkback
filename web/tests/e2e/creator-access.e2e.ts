import { test, expect } from '@playwright/test'
import { API_BASE, createSession, createUserAndLogin, uniqueEmail } from './fixtures'

// Page load + creator UI render — no LLM call, 30 s is generous.
test.setTimeout(30_000)

test('creator opens session in edit mode, sees creator-only UI', async ({ page, context, request }) => {
  // --- Seed: creator user + session via API ---
  const email = uniqueEmail('creator')
  await createUserAndLogin(context, request, email, 'SmokePass123!', 'Creator User')

  const session = await createSession(request, 'E2E Creator Access Session')

  // --- Navigate to session in edit (creator) mode ---
  await page.goto(`/?session=${session.id}&mode=edit`)
  await page.waitForLoadState('networkidle')

  // --- Assert: "Add content" collapsible button — unique to creator mode ---
  // The AddContentSection renders a button with text "Add content" (aria-expanded, class creator-collapsible-btn).
  const addContentBtn = page.getByRole('button', { name: /add content/i })
  await expect(addContentBtn).toBeVisible({ timeout: 10_000 })

  // --- Assert: creator left panel exists (CSS class unique to creator layout) ---
  const creatorPanel = page.locator('.creator-left-panel')
  await expect(creatorPanel).toBeVisible({ timeout: 5_000 })

  // --- Assert: Members collapsible button (invite UI) is present ---
  const membersBtn = page.getByRole('button', { name: /members/i })
  await expect(membersBtn).toBeVisible({ timeout: 5_000 })
})
