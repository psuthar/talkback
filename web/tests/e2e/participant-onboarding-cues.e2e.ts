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

// SCRUM-484: dialog copy is state-agnostic; the legacy video-anchored
// "Start here" overlay is replaced by a chip on the primary material row.
// Chip dismissal shares state with the dialog (returning participant sees
// neither cue).

async function seedSession(request, context, prefix: string) {
  const email = uniqueEmail(prefix)
  const userId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', prefix)
  const session = await createSession(request, `E2E ${prefix}`)
  return { userId, sessionId: session.id as string }
}

async function gotoSessionAsParticipant(page, sessionId: string) {
  await page.goto(`/?${new URLSearchParams({ session: sessionId, mode: 'view' })}`)
  await page.waitForLoadState('networkidle')
}

test('doc-primary: dialog → chip on primary doc → dismiss → returning visit shows neither', async ({ page, context, request }) => {
  const { userId, sessionId } = await seedSession(request, context, 'onboarding-cues')
  try {
    const material = await pasteMaterial(request, sessionId, 'Primary Spec', 'Hello world.')
    const r = await request.patch(`${API_BASE}/api/sessions/${sessionId}`, { data: { primary: { kind: 'document', id: material.id } } })
    expect(r.ok()).toBeTruthy()

    await gotoSessionAsParticipant(page, sessionId)

    const dialog = page.getByTestId('participant-onboarding-dialog')
    await expect(dialog).toBeVisible({ timeout: 10_000 })
    await expect(dialog).toContainText(/review the materials in the left panel/i)
    await expect(page.getByTestId('start-here-chip')).toHaveCount(0)

    await page.getByTestId('participant-onboarding-dismiss').click()
    await expect(dialog).toBeHidden()

    const row = page.getByTestId('material-item').filter({ hasText: 'Primary Spec' }).first().locator('xpath=..')
    await expect(row.getByTestId('primary-badge')).toBeVisible()
    await expect(row.getByTestId('start-here-chip')).toBeVisible()

    await page.getByTestId('start-here-chip-dismiss').click()
    await expect(page.getByTestId('start-here-chip')).toHaveCount(0)

    await page.reload()
    await page.waitForLoadState('networkidle')
    await expect(page.getByTestId('participant-onboarding-dialog')).toHaveCount(0)
    await expect(page.getByTestId('start-here-chip')).toHaveCount(0)
    await expect(page.getByTestId('primary-badge')).toBeVisible()
  } finally {
    await cleanupSessionAndUserAsAdmin(sessionId, userId)
  }
})

test('empty session: dialog shows updated copy and no chip ever appears', async ({ page, context, request }) => {
  const { userId, sessionId } = await seedSession(request, context, 'onboarding-empty')
  try {
    await gotoSessionAsParticipant(page, sessionId)

    const dialog = page.getByTestId('participant-onboarding-dialog')
    await expect(dialog).toBeVisible({ timeout: 10_000 })
    await expect(dialog).toContainText(/review the materials in the left panel/i)

    await page.getByTestId('participant-onboarding-dismiss').click()
    await expect(dialog).toBeHidden()
    await expect(page.getByTestId('start-here-chip')).toHaveCount(0)
    await expect(page.getByTestId('primary-badge')).toHaveCount(0)
  } finally {
    await cleanupSessionAndUserAsAdmin(sessionId, userId)
  }
})
