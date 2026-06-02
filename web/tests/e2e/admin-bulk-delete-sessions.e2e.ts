// SCRUM-583: Playwright happy-path for bulk session deletion in Admin →
// Sessions. Creates two throwaway sessions as admin, then selects both via the
// new row checkboxes, runs the bulk delete, and asserts the success toast plus
// the rows leaving the table.
//
// Self-contained: the two sessions it creates are the same two it deletes.
// afterEach best-effort removes any survivor if an assertion fails mid-flow.

import { test, expect, request as playwrightApiRequest } from '@playwright/test'
import {
  ADMIN_EMAIL,
  ADMIN_PASSWORD,
  API_BASE,
  injectCookiesFromResponse,
  createSession,
  deleteSession,
  loginAsAdmin,
} from './fixtures'

test.describe('AdminUsers bulk session delete', () => {
  let titles: string[] = []
  let sessionIds: string[] = []

  test.beforeEach(async ({ context, request }) => {
    // Authenticate the API request context (and the browser) as admin first.
    const loginRes = await request.post(`${API_BASE}/api/auth/login`, {
      data: { email: ADMIN_EMAIL, password: ADMIN_PASSWORD },
    })
    expect(loginRes.ok()).toBeTruthy()
    await injectCookiesFromResponse(context, loginRes)

    const stamp = `${Date.now()}-${Math.random().toString(36).slice(2, 6)}`
    titles = [`e2e bulk-del A ${stamp}`, `e2e bulk-del B ${stamp}`]
    sessionIds = []
    for (const title of titles) {
      const s = await createSession(request, title)
      sessionIds.push(s.id)
    }
  })

  test.afterEach(async () => {
    const ctx = await playwrightApiRequest.newContext()
    try {
      await loginAsAdmin(ctx)
      for (const id of sessionIds) await deleteSession(ctx, id)
    } catch {
      /* best-effort */
    } finally {
      await ctx.dispose()
    }
  })

  test('admin selects multiple sessions and bulk-deletes them', async ({ page }) => {
    await page.goto('/?mode=admin')

    // Expand the Sessions section.
    await page.getByRole('button', { name: /Sessions/ }).first().click()

    // Both created sessions are present and selectable.
    for (const title of titles) {
      await page.getByRole('checkbox', { name: `Select ${title}` }).check()
    }

    await expect(page.getByText('2 sessions selected')).toBeVisible()

    await page.getByRole('button', { name: 'Delete selected' }).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog.getByText('Delete 2 sessions?')).toBeVisible()
    // SCRUM-584: a small (≤10) selection must NOT require type-to-confirm.
    await expect(dialog.getByText(/to confirm/i)).toHaveCount(0)
    await dialog.getByRole('button', { name: /Delete sessions/ }).click()

    await expect(page.getByRole('status')).toContainText('2 sessions deleted.', { timeout: 15_000 })
    for (const title of titles) {
      await expect(page.getByRole('checkbox', { name: `Select ${title}` })).toHaveCount(0)
    }

    for (const errText of ['Failed to', 'Forbidden:', 'Network error', 'Could not delete']) {
      await expect(page.getByText(errText, { exact: false })).not.toBeVisible({ timeout: 1_000 })
    }
  })
})
