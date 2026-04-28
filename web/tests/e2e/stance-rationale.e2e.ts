import { test, expect } from '@playwright/test'
import {
  createUserAndLoginWithId,
  createSession,
  deleteSession,
  deleteUserViaAdmin,
  dismissParticipantOnboardingIfPresent,
  loginAsAdmin,
  setSessionPrimaryDecision,
  uniqueEmail,
} from './fixtures'

test.setTimeout(60_000)

let seededUserId = ''
let seededSessionId = ''

test.afterAll(async ({ request }) => {
  await loginAsAdmin(request)
  if (seededSessionId) await deleteSession(request, seededSessionId)
  if (seededUserId) await deleteUserViaAdmin(request, seededUserId)
})

test(
  'rationale does not submit on blur; Save rationale button submits explicitly',
  async ({ page, context, request }) => {
    const email = uniqueEmail('stance-rationale')
    seededUserId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', 'Stance Test User')

    const session = await createSession(request, 'Stance Rationale E2E Session')
    seededSessionId = session.id

    // Decisions section only renders when session.primary_decision is set
    await setSessionPrimaryDecision(request, session.id, 'Adopt the proposed approach')

    // Navigate to session in participant view
    const params = new URLSearchParams({ session: session.id, mode: 'view' })
    await page.goto(`/?${params.toString()}`)
    await page.waitForLoadState('networkidle')
    await dismissParticipantOnboardingIfPresent(page)

    // DecisionBar sits at the top of the participant content — no toggle to expand
    const decisionBar = page.getByTestId('decision-bar')
    await expect(decisionBar).toBeVisible({ timeout: 10_000 })

    // Open the stance dropdown first; the dropdown trigger is the only stance affordance
    const trigger = page.getByTestId('stance-trigger-btn')
    await expect(trigger).toBeVisible({ timeout: 5_000 })
    await trigger.click()

    // Select Need More Info (explicit bug regression target for SCRUM-164)
    const needMoreInfoBtn = page.getByTestId('stance-btn-need_more_info')
    await expect(needMoreInfoBtn).toBeVisible({ timeout: 5_000 })
    await needMoreInfoBtn.click()

    // Wait for stance to save (feedback disappears or updates)
    await page.waitForTimeout(1_000)

    // After submitting, the dropdown trigger morphs into the edit affordance
    const editBtn = page.getByTestId('stance-edit-btn')
    await expect(editBtn).toBeVisible({ timeout: 5_000 })

    // Rationale input remains available and aligned inline to the right of stance control
    const rationaleInput = page.getByTestId('stance-rationale-input')
    await expect(rationaleInput).toBeVisible({ timeout: 3_000 })
    const triggerBox = await editBtn.boundingBox()
    const rationaleBox = await rationaleInput.boundingBox()
    expect(triggerBox && rationaleBox).toBeTruthy()
    // same row (rough vertical alignment) and rationale starts to the right
    expect(Math.abs((triggerBox?.y ?? 0) - (rationaleBox?.y ?? 0))).toBeLessThan(12)
    expect((rationaleBox?.x ?? 0)).toBeGreaterThan((triggerBox?.x ?? 0))
    await rationaleInput.fill('My rationale text\nSecond line')
    await expect(rationaleInput).toHaveValue('My rationale text\nSecond line')

    // "Save rationale" button should appear (rationale differs from saved)
    const saveBtn = page.getByTestId('stance-save-rationale-btn')
    await expect(saveBtn).toBeVisible({ timeout: 3_000 })

    // Blur the input — Save button must still be visible (no auto-submit on blur)
    await rationaleInput.blur()
    await expect(saveBtn).toBeVisible()

    // Click Save rationale explicitly — button should disappear after save
    await saveBtn.click()
    await expect(saveBtn).not.toBeVisible({ timeout: 5_000 })
  }
)
