/**
 * Playwright global teardown — runs once after all E2E tests complete.
 *
 * Deletes all @smoke.test users and their sessions from the local database.
 * Uses the bootstrap admin credentials (TALKBACK_ADMIN_EMAIL / TALKBACK_ADMIN_PASSWORD).
 *
 * Strategy:
 *   1. Login as admin.
 *   2. List all @smoke.test users via GET /api/admin/users.
 *   3. For each smoke-test user, login as that user to call GET /api/sessions
 *      which returns both created and joined sessions. Collect all session IDs.
 *   4. Also collect session_ids from the admin user list (participant memberships).
 *   5. Delete all collected sessions via admin DELETE /api/sessions/:id.
 *   6. Delete all smoke-test users via admin DELETE /api/admin/users/:id.
 *
 * This handles both the current run and any accumulated backlog from prior runs.
 * Individual test files also track and clean up their own data via afterAll hooks
 * (defense-in-depth), but this global pass is the final guarantee.
 */

import { request as playwrightRequest } from '@playwright/test'
import { API_BASE } from './fixtures'

const ADMIN_EMAIL = process.env.TALKBACK_ADMIN_EMAIL || 'paresh@suthar.com'
const ADMIN_PASSWORD = process.env.TALKBACK_ADMIN_PASSWORD || 'your-secure-password'
const SMOKE_PASSWORD = 'SmokePass123!'

export default async function globalTeardown() {
  const ctx = await playwrightRequest.newContext({ baseURL: API_BASE })

  // 1. Login as admin
  const loginRes = await ctx.post(`${API_BASE}/api/auth/login`, {
    data: { email: ADMIN_EMAIL, password: ADMIN_PASSWORD },
  })
  if (!loginRes.ok()) {
    console.warn('[teardown] Admin login failed — skipping E2E cleanup')
    await ctx.dispose()
    return
  }

  // 2. List all users (admin endpoint; no limit param needed — it returns all)
  const usersRes = await ctx.get(`${API_BASE}/api/admin/users`)
  if (!usersRes.ok()) {
    console.warn('[teardown] Could not list users — skipping E2E cleanup')
    await ctx.dispose()
    return
  }
  const users: Array<{ id: string; email: string; session_ids?: string[] }> = await usersRes.json()

  const smokeUsers = users.filter((u) => u.email.endsWith('@smoke.test'))
  if (smokeUsers.length === 0) {
    console.log('[teardown] No @smoke.test users found — nothing to clean up')
    await ctx.dispose()
    return
  }

  // 3. Collect all session IDs: participant memberships (from admin list) + created sessions
  //    (from GET /api/sessions called as each user).
  const sessionIdsToDelete = new Set<string>()

  // From admin user list: participant-membership sessions
  for (const u of smokeUsers) {
    for (const sid of u.session_ids ?? []) {
      if (sid) sessionIdsToDelete.add(sid)
    }
  }

  // From each user's own session list: created sessions (creator owns them, not a member)
  // Use a fresh context per user so cookies don't conflict.
  for (const u of smokeUsers) {
    try {
      const userCtx = await playwrightRequest.newContext({ baseURL: API_BASE })
      const userLogin = await userCtx.post(`${API_BASE}/api/auth/login`, {
        data: { email: u.email, password: SMOKE_PASSWORD },
      })
      if (userLogin.ok()) {
        const sessionsRes = await userCtx.get(`${API_BASE}/api/sessions`)
        if (sessionsRes.ok()) {
          const sessions: Array<{ id: string }> = await sessionsRes.json()
          for (const s of sessions) {
            if (s?.id) sessionIdsToDelete.add(s.id)
          }
        }
      }
      await userCtx.dispose()
    } catch {
      // Non-fatal: user may have already been deleted or may have a different password.
    }
  }

  // 4. Delete all collected sessions via admin context
  let sessionDeleted = 0
  let sessionFailed = 0
  for (const sid of sessionIdsToDelete) {
    const res = await ctx.delete(`${API_BASE}/api/sessions/${sid}`)
    if (res.ok() || res.status() === 404) {
      sessionDeleted++
    } else {
      sessionFailed++
      console.warn(`[teardown] Failed to delete session ${sid}: ${res.status()}`)
    }
  }

  // 5. Delete all smoke-test users via admin context
  let userDeleted = 0
  let userFailed = 0
  for (const u of smokeUsers) {
    const res = await ctx.delete(`${API_BASE}/api/admin/users/${u.id}`)
    if (res.ok() || res.status() === 404 || res.status() === 204) {
      userDeleted++
    } else {
      userFailed++
      console.warn(`[teardown] Failed to delete user ${u.email}: ${res.status()}`)
    }
  }

  console.log(
    `[teardown] Cleaned up: ${sessionDeleted} sessions (${sessionFailed} failed), ` +
      `${userDeleted} users (${userFailed} failed)`
  )

  await ctx.dispose()
}
