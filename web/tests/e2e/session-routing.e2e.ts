/**
 * session-routing.e2e.ts
 *
 * Browser-first coverage for the same-origin web service URL routing changes.
 *
 * Flows tested:
 *   1. App loads correctly: unauthenticated visitor sees login page; no JS crash.
 *   2. Canonical deep link: /app/sessions/:id?mode=view loads the session.
 *   3. Legacy URL redirect: /?session=:id&mode=view navigates to /app/sessions/:id.
 *   4. Page refresh on canonical route keeps the user on the correct session.
 *   5. Newly generated invite link (accept_url) resolves to the canonical route
 *      after participant registration.
 *   6. Auth/session behavior: authenticated creator opening canonical edit URL
 *      stays on /app/sessions/:id?mode=edit (API base from Playwright storage state / VITE, no ?api=).
 *
 * Seeding strategy: all sessions and users created via API; browser only used for
 * the observable browser-side assertions.
 *
 * Cleanup: afterAll deletes session + user; global-teardown.ts catches any leakage.
 */

import { test, expect } from '@playwright/test'
import {
  API_BASE,
  createSession,
  createInvitation,
  createUserAndLoginWithId,
  deleteSession,
  deleteUserViaAdmin,
  loginAsAdmin,
  pasteMaterial,
  uniqueEmail,
} from './fixtures'

// All tests in this spec are fast (no LLM, no file processing).
test.setTimeout(30_000)

// ─── Shared seed IDs tracked across all tests ─────────────────────────────────
// Each test manages its own IDs via try/finally where they create unique data.
// The afterAll below covers any tests that share the suite-level IDs.
let suiteUserId = ''
let suiteSessionId = ''

test.afterAll(async ({ request }) => {
  await loginAsAdmin(request)
  if (suiteSessionId) await deleteSession(request, suiteSessionId)
  if (suiteUserId) await deleteUserViaAdmin(request, suiteUserId)
})

// ─── Test 1: App loads correctly ──────────────────────────────────────────────

test('app loads without auth and shows login UI', async ({ page }) => {
  await page.goto('/')
  // Auth check must complete — app renders either a session list or login page,
  // never a blank screen or JS crash. "Loading…" should resolve.
  await expect(page.getByText('Loading…')).not.toBeVisible({ timeout: 10_000 })

  // Unauthenticated visitor must see the TalkBack heading.
  await expect(page.getByRole('heading', { name: /talkback/i })).toBeVisible({ timeout: 10_000 })

  // The "Sign in to access sessions" hint text must appear.
  await expect(page.getByText(/sign in to access sessions/i)).toBeVisible()

  // The Log in submit button (type=submit inside the form) must be present and enabled.
  await expect(page.locator('form button[type="submit"]')).toBeEnabled()
})

// ─── Test 2: Canonical deep link loads session ─────────────────────────────────

test('canonical /app/sessions/:id?mode=view deep link loads session for authenticated user', async ({ page, context, request }) => {
  const email = uniqueEmail('routing-canonical')
  suiteUserId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', 'Routing User')
  const session = await createSession(request, 'E2E Routing Canonical Session')
  suiteSessionId = session.id
  await pasteMaterial(request, session.id, 'Routing Doc', 'Content for routing test.')

  // Navigate directly to canonical path — no legacy ?session= param.
  await page.goto(`/app/sessions/${session.id}?mode=view`)
  await page.waitForLoadState('networkidle')

  // The Q&A input is a reliable signal that the session loaded in participant view mode.
  await expect(page.getByTestId('question-input')).toBeVisible({ timeout: 15_000 })

  // URL must remain on the canonical path (not redirected away).
  await expect(page).toHaveURL(new RegExp(`/app/sessions/${session.id}`))
})

// ─── Test 3: Legacy URL redirects to canonical ────────────────────────────────

test('legacy /?session=:id&mode=view redirects URL to canonical /app/sessions/:id', async ({ page, context, request }) => {
  // Reuse suite-level seed if already created; otherwise create fresh ones.
  if (!suiteUserId || !suiteSessionId) {
    const email = uniqueEmail('routing-legacy')
    suiteUserId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', 'Legacy Routing User')
    const session = await createSession(request, 'E2E Routing Legacy Session')
    suiteSessionId = session.id
    await pasteMaterial(request, session.id, 'Legacy Doc', 'Content for legacy routing test.')
  } else {
    // Re-authenticate: cookies may not be present if test 2 ran in a fresh context.
    // Nothing to do — context fixture is per-test in Playwright; injectCookies was called
    // in test 2's context, not this one. Re-seed auth for this test's context.
    const email = uniqueEmail('routing-legacy')
    const tempUserId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', 'Legacy Routing User')
    const session = await createSession(request, 'E2E Routing Legacy Session')
    // Track for cleanup
    const oldUserId = suiteUserId
    const oldSessionId = suiteSessionId
    suiteUserId = tempUserId
    suiteSessionId = session.id
    // Clean up the previously tracked ones synchronously via request context
    await loginAsAdmin(request)
    await deleteSession(request, oldSessionId)
    await deleteUserViaAdmin(request, oldUserId)
    // Re-login as the new user so subsequent ops work
    await request.post(`${API_BASE}/api/auth/login`, {
      data: { email, password: 'SmokePass123!' },
    })
  }

  // Navigate to the legacy query-param URL — app should show session and normalise URL.
  await page.goto(`/?session=${suiteSessionId}&mode=view`)
  await page.waitForLoadState('networkidle')

  // The session must load successfully.
  await expect(page.getByTestId('question-input')).toBeVisible({ timeout: 15_000 })

  // App normalises the URL to canonical path after loading.
  await expect(page).toHaveURL(new RegExp(`/app/sessions/${suiteSessionId}`))
})

// ─── Test 4: Refresh on canonical route keeps session ─────────────────────────

test('refreshing canonical session route keeps the user on the correct session', async ({ page, context, request }) => {
  const email = uniqueEmail('routing-refresh')
  const userId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', 'Refresh User')
  const session = await createSession(request, 'E2E Routing Refresh Session')
  await pasteMaterial(request, session.id, 'Refresh Doc', 'Content for refresh routing test.')

  try {
    // Land on canonical URL.
    await page.goto(`/app/sessions/${session.id}?mode=view`)
    await page.waitForLoadState('networkidle')
    await expect(page.getByTestId('question-input')).toBeVisible({ timeout: 15_000 })

    // Hard-refresh the page.
    await page.reload()
    await page.waitForLoadState('networkidle')

    // After reload, the session must still be visible and URL must still reference the same session.
    await expect(page.getByTestId('question-input')).toBeVisible({ timeout: 15_000 })
    await expect(page).toHaveURL(new RegExp(`/app/sessions/${session.id}`))
  } finally {
    await loginAsAdmin(request)
    await deleteSession(request, session.id)
    await deleteUserViaAdmin(request, userId)
  }
})

// ─── Test 5: Invite link lands on canonical route ─────────────────────────────
// Covered already by participant-acceptance.e2e.ts which checks the post-accept redirect.
// This test validates the raw invite accept_url structure — the accept_url produced by the
// API must point to the same origin's /accept-invite path (not a legacy URL).

test('invitation accept_url uses same-origin accept-invite path', async ({ request }) => {
  // Seed creator + session + invitation via API.
  const creatorEmail = uniqueEmail('routing-inv-creator')
  await request.post(`${API_BASE}/api/auth/signup`, {
    data: { email: creatorEmail, password: 'SmokePass123!', display_name: 'Inv Creator' },
  })
  const loginRes = await request.post(`${API_BASE}/api/auth/login`, {
    data: { email: creatorEmail, password: 'SmokePass123!' },
  })
  expect(loginRes.ok()).toBe(true)

  const sessionData = await (await request.post(`${API_BASE}/sessions`, { data: { title: 'E2E Invite URL Test Session' } })).json()
  const invRes = await request.post(`${API_BASE}/api/sessions/${sessionData.id}/invitations`, {
    data: { email: uniqueEmail('routing-inv-ptc'), role: 'participant' },
  })
  const invData = await invRes.json()
  const invitation = invData.invitation
  expect(invitation?.accept_url).toBeTruthy()

  // The accept_url must contain /accept-invite — the same-origin SPA route.
  expect(invitation.accept_url).toContain('/accept-invite')

  // The URL must also carry a token query param.
  const token = new URL(invitation.accept_url).searchParams.get('token')
  expect(token).toBeTruthy()

  // Cleanup
  await loginAsAdmin(request)
  await deleteSession(request, sessionData.id)
  // Locate and delete creator
  const usersRes = await request.get(`${API_BASE}/api/admin/users?limit=500`)
  if (usersRes.ok()) {
    const users: Array<{ id: string; email: string }> = await usersRes.json()
    const creator = users.find((u) => u.email === creatorEmail)
    if (creator) await deleteUserViaAdmin(request, creator.id)
  }
})

// ─── Test 6: Auth/session behavior — canonical edit URL without ?api= ─────────

test('authenticated creator opens canonical edit URL without api= param and sees creator UI', async ({ page, context, request }) => {
  const email = uniqueEmail('routing-creator-same-origin')
  const userId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', 'Same Origin Creator')
  const session = await createSession(request, 'E2E Same-Origin Creator Session')

  try {
    // Navigate to canonical edit URL with NO ?api= (storage state seeds talkback.apiBaseUrl).
    await page.goto(`/app/sessions/${session.id}?mode=edit`)
    await page.waitForLoadState('networkidle')

    // Creator UI: "Add content" collapsible button is unique to creator mode.
    const addContentBtn = page.getByRole('button', { name: /add content/i })
    await expect(addContentBtn).toBeVisible({ timeout: 20_000 })

    // Creator left panel must be present.
    await expect(page.locator('.creator-left-panel')).toBeVisible({ timeout: 10_000 })

    // URL must remain on the canonical edit path.
    await expect(page).toHaveURL(new RegExp(`/app/sessions/${session.id}`))
    await expect(page).toHaveURL(/mode=edit/)
  } finally {
    await loginAsAdmin(request)
    await deleteSession(request, session.id)
    await deleteUserViaAdmin(request, userId)
  }
})

// ─── Test 7b: Skeleton loading state appears instead of raw error during participant session load ──

test('skeleton loading state shown during participant session load (no raw error flash)', async ({ page, context, request }) => {
  const email = uniqueEmail('routing-skeleton')
  const userId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', 'Skeleton User')
  const session = await createSession(request, 'E2E Skeleton Loading Session')

  try {
    // Slow down the session GET so the skeleton has time to appear.
    // NOTE: the SPA fetches `${apiBase}/sessions/:id` (un-prefixed) — NOT `/api/sessions/:id` —
    // so the pattern must cover both. `**/sessions/:id**` matches both routes (the leading `**`
    // absorbs any `/api` prefix when present).
    await page.route(`**/sessions/${session.id}**`, async (route) => {
      await new Promise((r) => setTimeout(r, 600))
      await route.continue()
    })

    // Navigate to the session URL
    page.goto(`/app/sessions/${session.id}?mode=view`)

    // After 300ms (before session response arrives), the skeleton must be visible
    await page.waitForTimeout(300)
    await expect(page.getByTestId('session-skeleton')).toBeVisible({ timeout: 2_000 })

    // The raw error message must NOT appear during loading
    await expect(page.getByText('Unable to load session')).not.toBeVisible()

    // Eventually the session loads and skeleton disappears
    await expect(page.getByTestId('question-input')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('session-skeleton')).not.toBeVisible()
  } finally {
    await loginAsAdmin(request)
    await deleteSession(request, session.id)
    await deleteUserViaAdmin(request, userId)
  }
})

// ─── Test 7: Creator header "View as Participant" link uses canonical route ────
// Validates Flow 5: newly generated share/session links land on the canonical route.
// The "View as Participant" anchor rendered in the creator header must use
// /app/sessions/:id?mode=view, not the legacy /?session=:id&mode=view format.

test('creator header participant link uses canonical /app/sessions/:id path', async ({ page, context, request }) => {
  const email = uniqueEmail('routing-share-link')
  const userId = await createUserAndLoginWithId(context, request, email, 'SmokePass123!', 'Share Link Creator')
  const session = await createSession(request, 'E2E Share Link Routing Session')

  try {
    // Navigate to canonical edit URL (no ?api=; API base from storage state / VITE).
    await page.goto(`/app/sessions/${session.id}?mode=edit`)
    await page.waitForLoadState('networkidle')

    // Wait for creator UI to be ready — "Add content" button is the reliable signal.
    await expect(page.getByRole('button', { name: /add content/i })).toBeVisible({ timeout: 20_000 })

    // Locate the "View as Participant" header link.
    // This anchor is rendered only for authenticated creators who have an open session.
    const participantLink = page.getByRole('link', { name: /view as participant/i })
    await expect(participantLink).toBeVisible({ timeout: 10_000 })

    // The href must use the canonical /app/sessions/ prefix — not the legacy ?session= format.
    const href = await participantLink.getAttribute('href')
    expect(href).toBeTruthy()
    expect(href).toMatch(new RegExp(`/app/sessions/${session.id}`))

    // The mode param must be view (not edit).
    const hrefUrl = new URL(href!, 'http://localhost:3000')
    expect(hrefUrl.searchParams.get('mode')).toBe('view')

    // There must be no legacy ?session= param.
    expect(hrefUrl.searchParams.has('session')).toBe(false)
  } finally {
    await loginAsAdmin(request)
    await deleteSession(request, session.id)
    await deleteUserViaAdmin(request, userId)
  }
})
