import { test, expect } from '@playwright/test'
import {
  cleanupSessionAndUserAsAdmin,
  createUserAndLoginWithId,
  uniqueEmail,
} from './fixtures'

// SCRUM-222: Session Selection cards show member count + creator label, with
// fallbacks driven entirely by the wire shape from GET /api/sessions. The
// backend wire shape is locked by SCRUM-221's Go tests; this spec verifies the
// frontend conditional rendering across the three label paths.

// Light page-load test, no LLM call.
test.setTimeout(20_000)

let viewerEmail = ''
let viewerUserId = ''

test.afterAll(async () => {
  // No real session was created (we mock GET /api/sessions); just clean up the
  // viewer user so repeated runs don't accumulate accounts.
  if (viewerUserId) await cleanupSessionAndUserAsAdmin('', viewerUserId)
})

test('Session Selection cards render member count + creator label across all label paths', async ({ page, context, request }) => {
  // --- Seed: real user so the browser context has a valid session cookie. ---
  viewerEmail = uniqueEmail('card-meta-viewer')
  viewerUserId = await createUserAndLoginWithId(context, request, viewerEmail, 'SmokePass123!', 'Card Meta Viewer')

  // --- Mock GET /api/sessions to control wire shape per AC.
  // Three rows exercising the three label paths in App.jsx:
  //   A) viewer-is-creator -> "you"
  //   B) display_name present -> use display_name verbatim
  //   C) display_name missing -> local-part of created_by (no "@" leaks)
  const ownedByViewerId = '00000000-0000-0000-0000-000000000aaa'
  const aliceSessionId = '00000000-0000-0000-0000-000000000bbb'
  const orphanSessionId = '00000000-0000-0000-0000-000000000ccc'

  await page.route('**/api/sessions', async (route) => {
    const json = [
      {
        my_role: 'creator',
        member_count: 3,
        created_by_display_name: 'Card Meta Viewer',
        session: {
          id: ownedByViewerId,
          title: 'Owned by viewer',
          created_by: viewerEmail,
          status: 'open',
          updated_at: '2026-04-30T10:00:00Z',
        },
      },
      {
        my_role: 'participant',
        member_count: 5,
        created_by_display_name: 'Alice Wonder',
        session: {
          id: aliceSessionId,
          title: 'Alice with display name',
          created_by: 'alice@acme.com',
          status: 'open',
          updated_at: '2026-04-30T09:00:00Z',
        },
      },
      {
        my_role: 'participant',
        member_count: 0,
        // created_by_display_name omitted (omitempty server-side)
        session: {
          id: orphanSessionId,
          title: 'Orphan creator email',
          created_by: 'jane.doe@acme.com',
          status: 'open',
          updated_at: '2026-04-30T08:00:00Z',
        },
      },
    ]
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(json),
    })
  })

  // --- Land on the Session Selection screen.
  await page.goto('/')
  await page.waitForLoadState('networkidle')

  // --- Assertions: each card in mocked order surfaces the right metadata.
  const metas = page.getByTestId('session-card-meta')
  await expect(metas).toHaveCount(3)

  // Card A — viewer is creator -> "you", member count = 3.
  const metaA = metas.nth(0)
  await expect(metaA).toContainText('3 members')
  await expect(metaA).toContainText('Created by you')
  // Make sure no email leaked into the card UI under any path.
  await expect(metaA).not.toContainText('@')

  // Card B — display_name present -> use it verbatim. member_count=5.
  const metaB = metas.nth(1)
  await expect(metaB).toContainText('5 members')
  await expect(metaB).toContainText('Created by Alice Wonder')
  await expect(metaB).not.toContainText('alice@acme.com')

  // Card C — display_name missing -> local-part fallback ("jane.doe"), no @.
  // Also verifies the 0-member suppression rule: the segment is hidden.
  const metaC = metas.nth(2)
  await expect(metaC).toContainText('Created by jane.doe')
  await expect(metaC).not.toContainText('jane.doe@acme.com')
  await expect(metaC).not.toContainText('member') // 0 members -> segment suppressed
})
