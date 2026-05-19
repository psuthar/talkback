import { test, expect } from '@playwright/test'
import {
  API_BASE,
  cleanupSessionAndUserAsAdmin,
  createSession,
  createUserAndLoginWithId,
  pasteMaterial,
  uniqueEmail,
} from './fixtures'

test.setTimeout(60_000)

// SCRUM-484: the onboarding dialog uses state-agnostic copy ("Review the
// materials in the left panel."), and the legacy video-anchored "Start here"
// overlay is replaced by a chip on whichever row matches currentPrimary. The
// chip's dismissal shares state with the dialog so a returning participant
// sees neither cue.

test.describe('participant onboarding cues (SCRUM-484)', () => {
  test('doc-primary session: dialog → chip on primary doc → chip dismisses → returning visit shows neither', async ({ page, context, request }) => {
    const email = uniqueEmail('onboarding-cues')
    const userId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', 'Onboarding User')
    const session = await createSession(request, 'E2E Onboarding Cues Session')
    const sessionId = session.id

    try {
      const material = await pasteMaterial(request, sessionId, 'Primary Spec', 'Hello world.')
      const setPrimaryRes = await request.patch(`${API_BASE}/api/sessions/${sessionId}`, {
        data: { primary: { kind: 'document', id: material.id } },
      })
      expect(setPrimaryRes.ok()).toBeTruthy()

      const params = new URLSearchParams({ session: sessionId, mode: 'view' })
      await page.goto(`/?${params.toString()}`)
      await page.waitForLoadState('networkidle')

      // 1. Dialog appears with the new state-agnostic copy.
      const dialog = page.getByTestId('participant-onboarding-dialog')
      await expect(dialog).toBeVisible({ timeout: 10_000 })
      await expect(dialog).toContainText(/review the materials in the left panel/i)

      // 2. Chip is NOT yet visible (it only appears after the dialog is dismissed).
      await expect(page.getByTestId('start-here-chip')).toHaveCount(0)

      // 3. Dismiss the dialog via "Got it".
      await page.getByTestId('participant-onboarding-dismiss').click()
      await expect(dialog).toBeHidden()

      // 4. Chip is now visible on the primary doc row, next to the Primary badge.
      const chip = page.getByTestId('start-here-chip')
      await expect(chip).toBeVisible()
      const primaryRow = page.getByTestId('material-item').filter({ hasText: 'Primary Spec' }).first()
      const rowContainer = primaryRow.locator('xpath=..')
      await expect(rowContainer.getByTestId('primary-badge')).toBeVisible()
      await expect(rowContainer.getByTestId('start-here-chip')).toBeVisible()

      // 5. Dismiss the chip via its × button.
      await page.getByTestId('start-here-chip-dismiss').click()
      await expect(chip).toHaveCount(0)

      // 6. Reload — returning participant sees neither cue (shared dismissal flag).
      await page.reload()
      await page.waitForLoadState('networkidle')
      await expect(page.getByTestId('participant-onboarding-dialog')).toHaveCount(0)
      await expect(page.getByTestId('start-here-chip')).toHaveCount(0)
      // Primary badge persists (it is not gated by onboarding state).
      await expect(page.getByTestId('primary-badge')).toBeVisible()
    } finally {
      await cleanupSessionAndUserAsAdmin(sessionId, userId)
    }
  })

  test('empty session: dialog still shows the updated copy and no chip ever appears', async ({ page, context, request }) => {
    const email = uniqueEmail('onboarding-empty')
    const userId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', 'Empty Onboarding User')
    const session = await createSession(request, 'E2E Onboarding Empty Session')
    const sessionId = session.id

    try {
      const params = new URLSearchParams({ session: sessionId, mode: 'view' })
      await page.goto(`/?${params.toString()}`)
      await page.waitForLoadState('networkidle')

      const dialog = page.getByTestId('participant-onboarding-dialog')
      await expect(dialog).toBeVisible({ timeout: 10_000 })
      await expect(dialog).toContainText(/review the materials in the left panel/i)

      await page.getByTestId('participant-onboarding-dismiss').click()
      await expect(dialog).toBeHidden()

      // No primary material exists → chip never renders even after dialog dismissal.
      await expect(page.getByTestId('start-here-chip')).toHaveCount(0)
      await expect(page.getByTestId('primary-badge')).toHaveCount(0)
    } finally {
      await cleanupSessionAndUserAsAdmin(sessionId, userId)
    }
  })
})
