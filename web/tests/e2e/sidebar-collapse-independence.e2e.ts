import { test, expect } from '@playwright/test'
import {
  API_BASE,
  createSession,
  createUserAndLoginWithId,
  deleteSession,
  deleteUserViaAdmin,
  dismissParticipantOnboardingIfPresent,
  loginAsAdmin,
  setSessionPrimaryDecision,
  uniqueEmail,
} from './fixtures'

// SCRUM-455: cover the lifted sidebar Context / Members / Materials siblings.
// Page load + simple DOM toggling — no LLM, no slow backend work.
test.setTimeout(60_000)

let seededUserId = ''
let seededSessionId = ''

test.afterAll(async ({ request }) => {
  await loginAsAdmin(request)
  if (seededSessionId) await deleteSession(request, seededSessionId)
  if (seededUserId) await deleteUserViaAdmin(request, seededUserId)
})

test(
  'creator: Context, Members, Materials are siblings and toggle independently (SCRUM-455)',
  async ({ page, context, request }) => {
    const email = uniqueEmail('sidebar-creator')
    seededUserId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', 'Sidebar Creator')

    const session = await createSession(request, 'SCRUM-455 Creator Sidebar Session')
    seededSessionId = session.id
    await setSessionPrimaryDecision(request, session.id, 'Approve the sidebar lift?')

    await page.goto(`/?session=${session.id}&mode=edit`)
    await page.waitForLoadState('networkidle')
    await dismissParticipantOnboardingIfPresent(page)

    const contextToggle = page.getByTestId('creator-context-toggle')
    const membersToggle = page.getByTestId('creator-members-toggle')
    const materialsToggle = page.getByTestId('creator-materials-tree-toggle')

    // All three sibling toggles render as part of the creator left rail.
    await expect(contextToggle).toBeVisible({ timeout: 20_000 })
    await expect(membersToggle).toBeVisible({ timeout: 10_000 })
    await expect(materialsToggle).toBeVisible({ timeout: 10_000 })

    // Materials sub-header toggles only the materials region; Context and Members
    // stay mounted. We assert the toggles remain on the page (sibling structure
    // is preserved) when Materials collapses.
    if ((await materialsToggle.getAttribute('aria-expanded')) === 'true') {
      await materialsToggle.click()
    }
    await expect(materialsToggle).toHaveAttribute('aria-expanded', 'false')
    await expect(contextToggle).toBeVisible()
    await expect(membersToggle).toBeVisible()
    // AddContentSection lives inside the materials sub-collapse, so when Materials
    // is collapsed the "Add content" button is no longer in the DOM.
    await expect(page.getByRole('button', { name: /add content/i })).toHaveCount(0)

    // Re-expand Materials so subsequent assertions get the full structure back.
    await materialsToggle.click()
    await expect(materialsToggle).toHaveAttribute('aria-expanded', 'true')

    // Members toggles independently of Context and Materials.
    const membersInitial = await membersToggle.getAttribute('aria-expanded')
    await membersToggle.click()
    await expect(membersToggle).not.toHaveAttribute('aria-expanded', membersInitial || 'false')
    await expect(contextToggle).toBeVisible()
    await expect(materialsToggle).toBeVisible()

    // Context toggles independently of Members and Materials.
    const contextInitial = await contextToggle.getAttribute('aria-expanded')
    await contextToggle.click()
    await expect(contextToggle).not.toHaveAttribute('aria-expanded', contextInitial || 'false')
    await expect(membersToggle).toBeVisible()
    await expect(materialsToggle).toBeVisible()

    // Column-level collapse (48px) hides all three siblings.
    const columnHeader = page.getByRole('button', { name: /collapse session panel/i })
    if (await columnHeader.isVisible({ timeout: 2_000 }).catch(() => false)) {
      await columnHeader.click()
      await expect(contextToggle).toHaveCount(0)
      await expect(membersToggle).toHaveCount(0)
      await expect(materialsToggle).toHaveCount(0)
    }
  }
)

test(
  'participant: read-only Context block renders when premise/decision/outcome are populated (SCRUM-455)',
  async ({ page, context, request }) => {
    const email = uniqueEmail('sidebar-participant-populated')
    const localUserId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', 'Sidebar Participant Populated')

    const session = await createSession(request, 'SCRUM-455 Participant Context Populated')
    // Populate all three Context fields via PATCH so the participant sidebar Context block renders.
    await request.patch(`${API_BASE}/api/sessions/${session.id}`, {
      data: {
        premise: 'We are evaluating sidebar layout changes.',
        primary_decision: 'Adopt the lift-out of Members and Context.',
        decision_outcome: 'Adopted for the next release.',
      },
    })

    try {
      await page.goto(`/?session=${session.id}&mode=view`)
      await page.waitForLoadState('networkidle')
      await dismissParticipantOnboardingIfPresent(page)

      const expandColumn = page.getByRole('button', { name: /expand session panel/i })
      if (await expandColumn.isVisible({ timeout: 2_000 }).catch(() => false)) {
        await expandColumn.click()
      }

      const contextSection = page.getByTestId('participant-sidebar-context')
      const contextToggle = page.getByTestId('participant-context-toggle')
      const membersToggle = page.getByTestId('participant-members-toggle')
      const materialsToggle = page.getByTestId('participant-materials-tree-toggle')

      await expect(contextSection).toBeVisible({ timeout: 15_000 })
      await expect(membersToggle).toBeVisible()
      await expect(materialsToggle).toBeVisible()

      // Expand Context and assert the three labeled values render read-only.
      if ((await contextToggle.getAttribute('aria-expanded')) !== 'true') {
        await contextToggle.click()
      }
      const region = page.locator('#participant-sidebar-context-region')
      await expect(region).toContainText('We are evaluating sidebar layout changes.')
      await expect(region).toContainText('Adopt the lift-out of Members and Context.')
      await expect(region).toContainText('Adopted for the next release.')

      // Materials sub-collapse toggles independently of Context/Members.
      if ((await materialsToggle.getAttribute('aria-expanded')) === 'true') {
        await materialsToggle.click()
      }
      await expect(materialsToggle).toHaveAttribute('aria-expanded', 'false')
      await expect(contextToggle).toBeVisible()
      await expect(membersToggle).toBeVisible()
    } finally {
      await loginAsAdmin(request)
      await deleteSession(request, session.id)
      await deleteUserViaAdmin(request, localUserId)
    }
  }
)

test(
  'participant: Context block is absent when premise/decision/outcome are all empty (SCRUM-455)',
  async ({ page, context, request }) => {
    const email = uniqueEmail('sidebar-participant-empty')
    const localUserId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', 'Sidebar Participant Empty')

    const session = await createSession(request, 'SCRUM-455 Participant Context Empty')
    // Intentionally do not patch any Context fields — premise, primary_decision,
    // and decision_outcome remain blank on the session row.

    try {
      await page.goto(`/?session=${session.id}&mode=view`)
      await page.waitForLoadState('networkidle')
      await dismissParticipantOnboardingIfPresent(page)

      const expandColumn = page.getByRole('button', { name: /expand session panel/i })
      if (await expandColumn.isVisible({ timeout: 2_000 }).catch(() => false)) {
        await expandColumn.click()
      }

      // Members and Materials sub-header still render; Context block does not.
      await expect(page.getByTestId('participant-members-toggle')).toBeVisible({ timeout: 15_000 })
      await expect(page.getByTestId('participant-materials-tree-toggle')).toBeVisible()
      await expect(page.getByTestId('participant-sidebar-context')).toHaveCount(0)
    } finally {
      await loginAsAdmin(request)
      await deleteSession(request, session.id)
      await deleteUserViaAdmin(request, localUserId)
    }
  }
)
