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
  'participant: Session sidebar never shows Context, regardless of session content (SCRUM-458)',
  async ({ page, context, request }) => {
    // Two sessions: one with all Context fields populated, one blank. The
    // participant Session sidebar must omit the Context block in both cases —
    // premise/primary_decision/decision_outcome are surfaced via
    // DecisionBriefHeader at the top of the participant view instead.
    const emailA = uniqueEmail('sidebar-participant-populated')
    const userA = await createUserAndLoginWithId(context, request, emailA, 'SmokePass123!', 'Sidebar Participant Populated')
    const sessionA = await createSession(request, 'SCRUM-458 Participant Populated Context')
    await request.patch(`${API_BASE}/api/sessions/${sessionA.id}`, {
      data: {
        premise: 'We are evaluating sidebar layout changes.',
        primary_decision: 'Hide Context from participant sidebar.',
        decision_outcome: 'Hidden — discovered via DecisionBriefHeader instead.',
      },
    })

    try {
      await page.goto(`/?session=${sessionA.id}&mode=view`)
      await page.waitForLoadState('networkidle')
      await dismissParticipantOnboardingIfPresent(page)

      const expandColumn = page.getByRole('button', { name: /expand session panel/i })
      if (await expandColumn.isVisible({ timeout: 2_000 }).catch(() => false)) {
        await expandColumn.click()
      }

      // Members and Materials sub-header render; Context block must be absent.
      await expect(page.getByTestId('participant-members-toggle')).toBeVisible({ timeout: 15_000 })
      await expect(page.getByTestId('participant-materials-tree-toggle')).toBeVisible()
      await expect(page.getByTestId('participant-sidebar-context')).toHaveCount(0)
      await expect(page.getByTestId('participant-context-toggle')).toHaveCount(0)

      // DecisionBriefHeader at the top continues to surface the same content
      // (no in-sidebar duplicate). The header is global to the participant
      // view, so we only assert that the premise text shows up *somewhere*
      // on the page outside the sidebar — the sidebar Context assertions
      // above already pinned its absence.
      await expect(page.getByText('We are evaluating sidebar layout changes.')).toBeVisible()
    } finally {
      await loginAsAdmin(request)
      await deleteSession(request, sessionA.id)
      await deleteUserViaAdmin(request, userA)
    }

    const emailB = uniqueEmail('sidebar-participant-blank')
    const userB = await createUserAndLoginWithId(context, request, emailB, 'SmokePass123!', 'Sidebar Participant Blank')
    const sessionB = await createSession(request, 'SCRUM-458 Participant Blank Context')
    // Intentionally leave premise/primary_decision/decision_outcome blank.

    try {
      await page.goto(`/?session=${sessionB.id}&mode=view`)
      await page.waitForLoadState('networkidle')
      await dismissParticipantOnboardingIfPresent(page)

      const expandColumn = page.getByRole('button', { name: /expand session panel/i })
      if (await expandColumn.isVisible({ timeout: 2_000 }).catch(() => false)) {
        await expandColumn.click()
      }

      await expect(page.getByTestId('participant-members-toggle')).toBeVisible({ timeout: 15_000 })
      await expect(page.getByTestId('participant-materials-tree-toggle')).toBeVisible()
      await expect(page.getByTestId('participant-sidebar-context')).toHaveCount(0)
    } finally {
      await loginAsAdmin(request)
      await deleteSession(request, sessionB.id)
      await deleteUserViaAdmin(request, userB)
    }
  }
)
