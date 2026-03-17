import { test, expect } from '@playwright/test'

// No LLM or long async — 15 s is plenty.
test.setTimeout(15_000)

test('invalid invite token shows error state on accept-invite page', async ({ page }) => {
  // Navigate unauthenticated browser directly to an invite link with a bogus token.
  await page.goto('/accept-invite?token=invalid-token-xyz-000')

  // The AcceptInvitePage calls GET /api/invitations/resolve?token=... which returns 404/error.
  // Once resolve completes, it renders the error card (resolveError branch).
  // The card renders the error text inside a styled div — no data-testid, so match by text.
  await expect(
    page.getByText(/invalid|expired|not found/i)
  ).toBeVisible({ timeout: 10_000 })
})
