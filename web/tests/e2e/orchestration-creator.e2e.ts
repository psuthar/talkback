import { test, expect } from '@playwright/test'
import {
  createSession,
  createUserAndLoginWithId,
  deleteSession,
  deleteUserViaAdmin,
  loginAsAdmin,
  pasteMaterial,
  uniqueEmail,
} from './fixtures'

/**
 * E2E coverage for the creator orchestration panel (SCRUM-18).
 *
 * The orchestration panel is AI-driven: recommendations come from the backend
 * evaluator. In CI there is no live AI backend, so API responses that carry
 * recommendation payloads are route-intercepted with deterministic fixtures.
 * Auth, session creation, and page-load requests hit the real backend.
 *
 * Scenarios
 * ---------
 * A) Panel renders and manual refresh calls the sync endpoint
 * B) Draft-review card: Approve draft → confirm + status-update calls
 * C) Draft-review card: Dismiss draft → delete draft-answer + status-update calls
 * D) Unanswered-question card: Generate draft → POST draft-answers call
 * E) Decision readiness (inputs_complete): Record outcome → PATCH session + PATCH recommendation; card removed
 */

// Allow extra time for CI cold-starts; panel interactions are local (mocked).
test.setTimeout(90_000)

// ── Shared fixture IDs ──────────────────────────────────────────────────────
let seededUserId = ''
let seededSessionId = ''

// ── Mock data ───────────────────────────────────────────────────────────────
const MOCK_ANSWER_ID = 'aaaaaaaa-0000-0000-0000-000000000001'
const MOCK_QUESTION_ID = 'bbbbbbbb-0000-0000-0000-000000000001'

const MOCK_REC_DRAFT: Record<string, unknown> = {
  id: 'rec-draft-001',
  recommendation_type: 'review_draft_answer',
  status: 'new',
  summary: 'A draft answer is ready for your review.',
  suggested_action: 'Review and approve or dismiss the draft below.',
  evidence: [{ answer_id: MOCK_ANSWER_ID, question_id: MOCK_QUESTION_ID }],
}

const MOCK_REC_UNANSWERED: Record<string, unknown> = {
  id: 'rec-unanswered-001',
  recommendation_type: 'unanswered_question',
  status: 'new',
  summary: 'A participant question has not been answered yet.',
  suggested_action: 'Generate an AI draft to speed up your response.',
  evidence: [{ question_id: MOCK_QUESTION_ID }],
}

const MOCK_REC_DECISION_READY: Record<string, unknown> = {
  id: 'rec-decision-ready-001',
  recommendation_type: 'decision_readiness',
  status: 'new',
  summary: 'Core decision inputs are present and participant stances exist; review before finalizing.',
  suggested_action: 'Review stances and decision outcome before closing the session.',
  metadata_json: { readiness: 'inputs_complete' },
}

// ── Helpers ─────────────────────────────────────────────────────────────────

/** Navigate to the creator view for a session. */
async function navigateToCreatorSession(page: import('@playwright/test').Page, sessionId: string) {
  const params = new URLSearchParams({ session: sessionId, mode: 'edit' })
  await page.goto(`/?${params.toString()}`)
  await page.waitForLoadState('networkidle')
}

/** Wait for the orchestration panel to be visible. */
async function waitForPanel(page: import('@playwright/test').Page) {
  await expect(page.getByTestId('orchestration-panel')).toBeVisible({ timeout: 20_000 })
}

// ── Seed once for all tests ─────────────────────────────────────────────────
test.beforeAll(async ({ request, browser }) => {
  const context = await browser.newContext()
  const email = uniqueEmail('orch')
  seededUserId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', 'Orch Creator')
  const session = await createSession(request, 'E2E Orchestration Session')
  seededSessionId = session.id
  // Add paste material so the session has indexable content.
  await pasteMaterial(request, seededSessionId, 'Session notes', 'Key decision: adopt new auth flow.')
  await context.close()
})

test.afterAll(async ({ request }) => {
  await loginAsAdmin(request)
  if (seededSessionId) await deleteSession(request, seededSessionId)
  if (seededUserId) await deleteUserViaAdmin(request, seededUserId)
})

// ── Scenario A: panel renders + refresh calls sync endpoint ─────────────────
test('orchestration panel renders and refresh triggers sync', async ({ page, context, request }) => {
  // Re-login so this browser context is authenticated.
  const email = uniqueEmail('orch-a')
  const userId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', 'Orch A')
  const session = await createSession(request, 'E2E Orch-A Session')

  // Mock the sync endpoint to return an empty list (no AI dependency).
  let syncCalled = false
  await page.route(`**/api/sessions/${session.id}/orchestration/recommendations/sync`, (route) => {
    syncCalled = true
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ recommendations: [] }) })
  })
  // Mock the initial GET as well (called on load).
  await page.route(`**/api/sessions/${session.id}/orchestration/recommendations`, (route) => {
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ recommendations: [] }) })
  })

  await navigateToCreatorSession(page, session.id)
  await waitForPanel(page)

  // Empty state shown when no recommendations.
  const emptyState = page.getByTestId('orchestration-empty')
  await expect(emptyState).toBeVisible({ timeout: 10_000 })
  await expect(emptyState).toContainText('Add transcript or materials, then collect participant questions', { timeout: 10_000 })

  // Click refresh — should call sync endpoint.
  // SCRUM-195: refresh is now an icon-only button labeled by aria-label.
  const refreshBtn = page.getByTestId('orchestration-refresh-btn')
  await expect(refreshBtn).toBeVisible()
  expect(await refreshBtn.getAttribute('aria-label')).toBe('Refresh suggestions')
  await refreshBtn.click()

  // Wait for the sync call to be intercepted.
  await expect.poll(() => syncCalled, { timeout: 10_000 }).toBe(true)
  // Refresh button returns to ready state — the spinner-on-loading is gone and
  // the arrow icon is back. The icon is testid'd as orchestration-refresh-icon.
  await expect(page.getByTestId('orchestration-refresh-icon')).toBeVisible({ timeout: 5_000 })
  await expect(page.getByTestId('orchestration-refresh-spinner')).toHaveCount(0)

  // Teardown
  await loginAsAdmin(request)
  await deleteSession(request, session.id)
  await deleteUserViaAdmin(request, userId)
})

// ── Scenario B: approve draft ────────────────────────────────────────────────
test('orchestration panel: approve draft calls confirm + approved status update', async ({ page, context, request }) => {
  const email = uniqueEmail('orch-b')
  const userId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', 'Orch B')
  const session = await createSession(request, 'E2E Orch-B Session')
  const recId = MOCK_REC_DRAFT.id as string

  // Mock sync to return one draft-review recommendation.
  await page.route(`**/api/sessions/${session.id}/orchestration/recommendations**`, (route) => {
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ recommendations: [MOCK_REC_DRAFT] }) })
  })

  // Capture the confirm PATCH call.
  let confirmCalled = false
  let confirmPayload: unknown = null
  await page.route(`**/sessions/${session.id}/answers/${MOCK_ANSWER_ID}/confirm`, async (route) => {
    confirmCalled = true
    confirmPayload = JSON.parse(route.request().postData() ?? '{}')
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ id: MOCK_ANSWER_ID, confirmed: true }) })
  })

  // Capture the recommendation status PATCH.
  let statusUpdateCalled = false
  let statusUpdatePayload: unknown = null
  await page.route(`**/api/sessions/${session.id}/orchestration/recommendations/${recId}`, async (route) => {
    if (route.request().method() === 'PATCH') {
      statusUpdateCalled = true
      statusUpdatePayload = JSON.parse(route.request().postData() ?? '{}')
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ...MOCK_REC_DRAFT, status: 'approved' }) })
    } else {
      route.continue()
    }
  })

  await navigateToCreatorSession(page, session.id)
  await waitForPanel(page)

  // Recommendation card visible.
  await expect(page.getByTestId(`orchestration-rec-${recId}`)).toBeVisible({ timeout: 10_000 })

  // Click Approve draft.
  await page.getByTestId(`orchestration-approve-${recId}`).click()

  // Both API calls should have fired.
  await expect.poll(() => confirmCalled, { timeout: 10_000 }).toBe(true)
  await expect.poll(() => statusUpdateCalled, { timeout: 10_000 }).toBe(true)

  expect((confirmPayload as Record<string, unknown>).confirmed).toBe(true)
  expect((statusUpdatePayload as Record<string, unknown>).status).toBe('approved')

  // Teardown
  await loginAsAdmin(request)
  await deleteSession(request, session.id)
  await deleteUserViaAdmin(request, userId)
})

// ── Scenario C: dismiss draft ────────────────────────────────────────────────
test('orchestration panel: dismiss draft calls delete draft-answer + dismissed status update', async ({ page, context, request }) => {
  const email = uniqueEmail('orch-c')
  const userId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', 'Orch C')
  const session = await createSession(request, 'E2E Orch-C Session')
  const recId = MOCK_REC_DRAFT.id as string

  await page.route(`**/api/sessions/${session.id}/orchestration/recommendations**`, (route) => {
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ recommendations: [MOCK_REC_DRAFT] }) })
  })

  // Capture the DELETE draft-answer call.
  let deleteCalled = false
  await page.route(`**/api/sessions/${session.id}/orchestration/draft-answers/${MOCK_ANSWER_ID}`, (route) => {
    if (route.request().method() === 'DELETE') {
      deleteCalled = true
      route.fulfill({ status: 204, body: '' })
    } else {
      route.continue()
    }
  })

  // Capture the recommendation status PATCH.
  let statusUpdatePayload: unknown = null
  await page.route(`**/api/sessions/${session.id}/orchestration/recommendations/${recId}`, async (route) => {
    if (route.request().method() === 'PATCH') {
      statusUpdatePayload = JSON.parse(route.request().postData() ?? '{}')
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ...MOCK_REC_DRAFT, status: 'dismissed' }) })
    } else {
      route.continue()
    }
  })

  await navigateToCreatorSession(page, session.id)
  await waitForPanel(page)
  await expect(page.getByTestId(`orchestration-rec-${recId}`)).toBeVisible({ timeout: 10_000 })

  await page.getByTestId(`orchestration-dismiss-draft-${recId}`).click()

  await expect.poll(() => deleteCalled, { timeout: 10_000 }).toBe(true)
  await expect.poll(() => statusUpdatePayload !== null, { timeout: 10_000 }).toBe(true)
  expect((statusUpdatePayload as Record<string, unknown>).status).toBe('dismissed')

  // Teardown
  await loginAsAdmin(request)
  await deleteSession(request, session.id)
  await deleteUserViaAdmin(request, userId)
})

// ── Scenario D: generate draft for unanswered question ───────────────────────
test('orchestration panel: generate draft calls POST draft-answers with question_id', async ({ page, context, request }) => {
  const email = uniqueEmail('orch-d')
  const userId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', 'Orch D')
  const session = await createSession(request, 'E2E Orch-D Session')
  const recId = MOCK_REC_UNANSWERED.id as string

  await page.route(`**/api/sessions/${session.id}/orchestration/recommendations**`, (route) => {
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ recommendations: [MOCK_REC_UNANSWERED] }) })
  })

  // Capture the POST draft-answers call.
  let generateCalled = false
  let generatePayload: unknown = null
  await page.route(`**/api/sessions/${session.id}/orchestration/draft-answers`, async (route) => {
    if (route.request().method() === 'POST') {
      generateCalled = true
      generatePayload = JSON.parse(route.request().postData() ?? '{}')
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ id: MOCK_ANSWER_ID, question_id: MOCK_QUESTION_ID, confirmed: false }),
      })
    } else {
      route.continue()
    }
  })

  await navigateToCreatorSession(page, session.id)
  await waitForPanel(page)
  await expect(page.getByTestId(`orchestration-rec-${recId}`)).toBeVisible({ timeout: 10_000 })

  await page.getByTestId(`orchestration-generate-${recId}`).click()

  await expect.poll(() => generateCalled, { timeout: 10_000 }).toBe(true)
  expect((generatePayload as Record<string, unknown>).question_id).toBe(MOCK_QUESTION_ID)

  // Feedback can be the draft success message or the subsequent sync-refresh message.
  await expect(page.getByTestId('orchestration-feedback')).toContainText(/Draft answer generated|Recommendations refreshed\./, { timeout: 5_000 })

  // Teardown
  await loginAsAdmin(request)
  await deleteSession(request, session.id)
  await deleteUserViaAdmin(request, userId)
})

// ── Scenario E: record decision outcome (SCRUM-21) ───────────────────────────
test('orchestration panel: record outcome PATCHes session then recommendation; card removed', async ({ page, context, request }) => {
  const email = uniqueEmail('orch-e')
  const userId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', 'Orch E')
  const session = await createSession(request, 'E2E Orch-E Session')
  const recId = MOCK_REC_DECISION_READY.id as string

  await page.route(`**/api/sessions/${session.id}/orchestration/recommendations**`, (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ recommendations: [MOCK_REC_DECISION_READY] }),
    })
  })

  let sessionPatchCalled = false
  let sessionPatchPayload: unknown = null
  const sessionResourcePath = `/api/sessions/${session.id}`
  await page.route(
    (url) => {
      const path = url.pathname
      return path === sessionResourcePath || path === `${sessionResourcePath}/`
    },
    async (route) => {
      if (route.request().method() === 'PATCH') {
        sessionPatchCalled = true
        sessionPatchPayload = JSON.parse(route.request().postData() ?? '{}')
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            id: session.id,
            title: session.title ?? 'E2E Orch-E Session',
            decision_outcome: (sessionPatchPayload as Record<string, unknown>).decision_outcome,
          }),
        })
      }
      return route.continue()
    },
  )

  let recPatchCalled = false
  let recPatchPayload: unknown = null
  await page.route(`**/api/sessions/${session.id}/orchestration/recommendations/${recId}`, async (route) => {
    if (route.request().method() === 'PATCH') {
      recPatchCalled = true
      recPatchPayload = JSON.parse(route.request().postData() ?? '{}')
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ ...MOCK_REC_DECISION_READY, status: 'completed' }),
      })
    } else {
      route.continue()
    }
  })

  await navigateToCreatorSession(page, session.id)
  await waitForPanel(page)
  await expect(page.getByTestId(`orchestration-rec-${recId}`)).toBeVisible({ timeout: 10_000 })

  await page.getByTestId(`orchestration-record-outcome-${recId}`).click()
  const input = page.getByTestId(`orchestration-outcome-input-${recId}`)
  await expect(input).toBeVisible()
  await input.fill('Approved: ship v1 in Q2.')

  await page.getByTestId(`orchestration-save-outcome-${recId}`).click()

  await expect.poll(() => sessionPatchCalled, { timeout: 10_000 }).toBe(true)
  await expect.poll(() => recPatchCalled, { timeout: 10_000 }).toBe(true)
  expect((sessionPatchPayload as Record<string, unknown>).decision_outcome).toBe('Approved: ship v1 in Q2.')
  expect((recPatchPayload as Record<string, unknown>).status).toBe('completed')

  await expect(page.getByTestId(`orchestration-rec-${recId}`)).not.toBeVisible({ timeout: 5_000 })
  await expect(page.getByTestId('orchestration-feedback')).toContainText('Decision outcome recorded', { timeout: 5_000 })

  await loginAsAdmin(request)
  await deleteSession(request, session.id)
  await deleteUserViaAdmin(request, userId)
})

// ── Scenario F: audit trail (SCRUM-22) ──────────────────────────────────────
test('orchestration panel: activity section collapsed by default, expands and shows entries', async ({ page, context, request }) => {
  const email = uniqueEmail('orch-f')
  const userId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', 'Orch F')
  const session = await createSession(request, 'E2E Orch-F Session')

  const MOCK_AUDIT = [
    {
      id: 'audit-001',
      recommendation_type: 'review_draft_answer',
      from_status: 'new',
      to_status: 'approved',
      changed_by: 'orch-f@example.com',
      changed_at: '2026-01-15T10:30:00Z',
    },
  ]

  await page.route(`**/api/sessions/${session.id}/orchestration/recommendations**`, (route) => {
    const url = route.request().url()
    if (url.includes('/audit')) {
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ status_audit: MOCK_AUDIT }) })
    } else {
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ recommendations: [] }) })
    }
  })

  await navigateToCreatorSession(page, session.id)
  await waitForPanel(page)

  // Audit list should not be visible initially (collapsed).
  await expect(page.getByTestId('orchestration-audit-list')).not.toBeVisible()

  // Toggle open.
  const toggle = page.getByTestId('orchestration-audit-toggle')
  await expect(toggle).toBeVisible()
  await toggle.click()

  // Audit list should now be visible with the mocked entry.
  const auditList = page.getByTestId('orchestration-audit-list')
  await expect(auditList).toBeVisible({ timeout: 10_000 })
  const entry = page.getByTestId('orchestration-audit-entry-0')
  await expect(entry).toBeVisible()
  await expect(entry).toContainText('review draft answer')
  await expect(entry).toContainText('new → approved')

  // Toggle closed again — list hidden.
  await toggle.click()
  await expect(auditList).not.toBeVisible()

  // Teardown
  await loginAsAdmin(request)
  await deleteSession(request, session.id)
  await deleteUserViaAdmin(request, userId)
})
