// SCRUM-582: Playwright happy-path for bulk user deletion in Admin → Users.
// Creates two throwaway users via signup, logs in as admin, then selects both
// via the new row checkboxes, runs the bulk delete, and asserts the success
// toast plus the rows leaving the table.
//
// Self-contained: the two users it creates are the same two it deletes, so the
// e2e doesn't depend on (or disturb) other dev-DB users. afterEach best-effort
// removes any survivor if an assertion fails mid-flow.

import { test, expect, request as playwrightApiRequest } from '@playwright/test'
import {
  ADMIN_EMAIL,
  ADMIN_PASSWORD,
  API_BASE,
  injectCookiesFromResponse,
  uniqueEmail,
  deleteUserViaAdmin,
  loginAsAdmin,
} from './fixtures'

test.describe('AdminUsers bulk delete', () => {
  let emails: string[] = []
  let userIds: string[] = []

  test.beforeEach(async ({ context, request }) => {
    emails = [uniqueEmail('bulk1'), uniqueEmail('bulk2')]
    userIds = []
    for (const email of emails) {
      const res = await request.post(`${API_BASE}/api/auth/signup`, {
        data: { email, password: 'SmokePass123!', display_name: email },
      })
      expect(res.ok()).toBeTruthy()
      const data = await res.json()
      userIds.push(data.id)
    }

    const loginRes = await request.post(`${API_BASE}/api/auth/login`, {
      data: { email: ADMIN_EMAIL, password: ADMIN_PASSWORD },
    })
    expect(loginRes.ok()).toBeTruthy()
    await injectCookiesFromResponse(context, loginRes)
  })

  test.afterEach(async () => {
    const ctx = await playwrightApiRequest.newContext()
    try {
      await loginAsAdmin(ctx)
      for (const id of userIds) await deleteUserViaAdmin(ctx, id)
    } catch {
      /* best-effort */
    } finally {
      await ctx.dispose()
    }
  })

  test('admin selects multiple users and bulk-deletes them', async ({ page }) => {
    await page.goto('/?mode=admin')

    // Expand the Users section.
    await page.getByRole('button', { name: /Users/ }).first().click()

    // Both created users are present and selectable.
    for (const email of emails) {
      await page.getByRole('checkbox', { name: `Select ${email}` }).check()
    }

    // Bulk action bar reflects the live count.
    await expect(page.getByText('2 users selected')).toBeVisible()

    // Open the confirm dialog and confirm.
    await page.getByRole('button', { name: 'Delete selected' }).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog.getByText('Delete 2 users?')).toBeVisible()
    await dialog.getByRole('button', { name: /Delete users/ }).click()

    // Success toast and rows removed.
    await expect(page.getByRole('status')).toContainText('2 users deleted.', { timeout: 15_000 })
    for (const email of emails) {
      await expect(page.getByRole('checkbox', { name: `Select ${email}` })).toHaveCount(0)
    }

    // No error banner regressed in the process.
    for (const errText of ['Failed to', 'Forbidden:', 'Network error']) {
      await expect(page.getByText(errText, { exact: false })).not.toBeVisible({ timeout: 1_000 })
    }
  })
})
