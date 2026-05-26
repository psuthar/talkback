// SCRUM-580 (Slice 3 of SCRUM-577): Playwright happy-path for the
// AdminGuardrailStats section. Logs in as admin, navigates to
// ?mode=admin, expands the section, asserts the section renders and
// that `/api/admin/llm-stats` is hit on every action (initial fetch
// + days-change + Refresh).
//
// Doesn't assert exact metric counts — dev DB content is unpredictable;
// the e2e is a round-trip smoke, not a regression on values. Vitest
// unit tests cover the value-shape assertions deterministically.

import { test, expect } from '@playwright/test'
import { ADMIN_EMAIL, ADMIN_PASSWORD, API_BASE, injectCookiesFromResponse } from './fixtures'

test.describe('AdminGuardrailStats', () => {
  test.beforeEach(async ({ context, request }) => {
    const loginRes = await request.post(`${API_BASE}/api/auth/login`, {
      data: { email: ADMIN_EMAIL, password: ADMIN_PASSWORD },
    })
    expect(loginRes.ok()).toBeTruthy()
    await injectCookiesFromResponse(context, loginRes)
  })

  test('admin can expand, change window, and refresh the section', async ({ page }) => {
    await page.goto('/?mode=admin')

    // First fetch fires on expand with the default ?days=7 window.
    const firstResponse = page.waitForResponse(
      (res) =>
        res.url().includes('/api/admin/llm-stats') &&
        res.url().includes('days=7') &&
        res.status() === 200,
      { timeout: 15_000 }
    )

    await page.getByRole('button', { name: /Guardrail telemetry/i }).click()
    await firstResponse

    // The five big-number card labels are visible. The Token usage
    // card is SCRUM-580's contribution; the other four ship in
    // SCRUM-579's slice but still need to be present here.
    for (const label of ['Total calls', 'p95 latency', 'Dropped rows', 'Refused', 'Token usage']) {
      await expect(page.getByText(label, { exact: true })).toBeVisible()
    }

    // Clicking [1d] refetches with ?days=1.
    const oneDayResponse = page.waitForResponse(
      (res) =>
        res.url().includes('/api/admin/llm-stats') &&
        res.url().includes('days=1') &&
        res.status() === 200,
      { timeout: 15_000 }
    )
    await page.getByRole('button', { name: '1d' }).click()
    await oneDayResponse

    // Clicking Refresh fires another call (window stays at 1d).
    const refreshResponse = page.waitForResponse(
      (res) =>
        res.url().includes('/api/admin/llm-stats') &&
        res.url().includes('days=1') &&
        res.status() === 200,
      { timeout: 15_000 }
    )
    await page.getByRole('button', { name: /Refresh/i }).click()
    await refreshResponse

    // No error banner. (Match the AdminUsers voice — "Failed to load",
    // "Forbidden", "Network error" — any of these means a regression.)
    for (const errText of ['Failed to load', 'Forbidden:', 'Network error']) {
      await expect(page.getByText(errText, { exact: false })).not.toBeVisible({
        timeout: 1_000,
      })
    }
  })
})
