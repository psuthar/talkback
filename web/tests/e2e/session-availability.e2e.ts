import { test, expect } from '@playwright/test'
import { API_BASE, createSession, createUserAndLogin, pasteMaterial, uniqueEmail } from './fixtures'

// Page load + materials panel — no LLM call.
test.setTimeout(20_000)

test('participant opens prepared session, sees materials panel and QA input', async ({ page, context, request }) => {
  // --- Seed: user + session + material via API ---
  const email = uniqueEmail('session-avail')
  await createUserAndLogin(context, request, email, 'SmokePass123!', 'Availability User')

  const session = await createSession(request, 'E2E Session Availability Test')
  await pasteMaterial(request, session.id, 'Overview Doc', 'This document covers project scope and timeline.')

  // --- Navigate to session in view mode (api= so app uses same backend on Render) ---
  const params = new URLSearchParams({ session: session.id, mode: 'view', api: API_BASE })
  await page.goto(`/?${params.toString()}`)
  await page.waitForLoadState('networkidle')

  // --- Expand materials panel if it loaded collapsed ---
  const expandPanelBtn = page.getByRole('button', { name: 'Expand materials panel' })
  if (await expandPanelBtn.isVisible({ timeout: 2_000 }).catch(() => false)) {
    await expandPanelBtn.click()
  }

  // --- Assert: seeded material item is visible in the Documents section ---
  await expect(
    page.getByTestId('material-item').filter({ hasText: 'Overview Doc' })
  ).toBeVisible({ timeout: 10_000 })

  // --- Assert: Q&A input is present ---
  await expect(page.getByTestId('question-input')).toBeVisible({ timeout: 5_000 })
})
