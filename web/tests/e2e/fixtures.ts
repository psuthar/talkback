import type { BrowserContext, APIRequestContext, APIResponse } from '@playwright/test'
import { request as playwrightApiRequest } from '@playwright/test'

/** Backend API base URL. Default matches `go run ./cmd/api` (PORT=8080). CI may use 8081 via TALKBACK_API_BASE. */
export const API_BASE = process.env.TALKBACK_API_BASE || 'http://localhost:8080'

/**
 * Parse Set-Cookie response header and inject cookies into the browser context.
 * Supports both single and multi-line Set-Cookie headers.
 */
export async function injectCookiesFromResponse(
  context: BrowserContext,
  response: APIResponse
): Promise<void> {
  const raw = response.headers()['set-cookie']
  if (!raw) return

  const cookieDefs = raw
    .split(/\n/)
    .map((line) => line.trim())
    .filter(Boolean)

  const apiUrl = new URL(API_BASE)
  const cookieDomain = apiUrl.hostname
  const isHttps = apiUrl.protocol === 'https:'

  const parsed = cookieDefs.map((line) => {
    const parts = line.split(';').map((p) => p.trim())
    const [nameVal] = parts
    const eqIdx = nameVal.indexOf('=')
    const name = nameVal.slice(0, eqIdx).trim()
    const value = nameVal.slice(eqIdx + 1).trim()
    const pathPart = parts.find((p) => p.toLowerCase().startsWith('path='))
    const cookie: { name: string; value: string; domain: string; path: string; secure?: boolean; sameSite?: 'Strict' | 'Lax' | 'None' } = {
      name,
      value,
      domain: cookieDomain,
      path: pathPart ? pathPart.split('=')[1].trim() : '/',
    }
    // Cross-origin (e.g. Render): browser only sends cookie to API if Secure + SameSite=None
    if (isHttps) {
      cookie.secure = true
      cookie.sameSite = 'None'
    }
    return cookie
  })

  await context.addCookies(parsed)
}

/**
 * Sign up a new user and inject their session cookie into the browser context.
 */
export async function createUserAndLogin(
  context: BrowserContext,
  request: APIRequestContext,
  email: string,
  password = 'SmokePass123!',
  displayName?: string
): Promise<void> {
  await request.post(`${API_BASE}/api/auth/signup`, {
    data: {
      email,
      password,
      display_name: displayName ?? email.split('@')[0],
    },
  })
  const loginRes = await request.post(`${API_BASE}/api/auth/login`, {
    data: { email, password },
  })
  await injectCookiesFromResponse(context, loginRes)
}

/**
 * Create a session via API. Returns the session object (id, title, …).
 */
export async function createSession(
  request: APIRequestContext,
  title: string
): Promise<{ id: string; title: string }> {
  const res = await request.post(`${API_BASE}/sessions`, {
    data: { title },
  })
  return res.json()
}

/**
 * Paste text material into a session.
 * The material is immediately text_status=ready — no async pipeline.
 */
export async function pasteMaterial(
  request: APIRequestContext,
  sessionId: string,
  title: string,
  text: string
): Promise<{ id: string }> {
  const res = await request.post(`${API_BASE}/sessions/${sessionId}/materials/paste`, {
    data: { title, text },
  })
  return res.json()
}

/**
 * Create an invitation for a participant email in a session.
 * Requires the request context to already be authenticated as a session creator.
 * Returns the invitation object including accept_url (contains the raw token).
 */
export async function createInvitation(
  request: APIRequestContext,
  sessionId: string,
  email: string,
  role = 'participant'
): Promise<{ accept_url: string; invited_email: string; status: string }> {
  const res = await request.post(`${API_BASE}/api/sessions/${sessionId}/invitations`, {
    data: { email, role },
  })
  const data = await res.json()
  return data.invitation
}

/** Unique per-test email — prevents collision between test runs. */
export function uniqueEmail(prefix = 'e2e'): string {
  return `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2, 6)}@smoke.test`
}

/**
 * Admin credentials for teardown operations.
 * Reads from env vars (set in .env or CI); falls back to local bootstrap defaults.
 */
/** Match bootstrap admin (TALKBACK_BOOTSTRAP_ADMIN_*) and release-readiness CI defaults for teardown. */
export const ADMIN_EMAIL = process.env.TALKBACK_ADMIN_EMAIL || 'ci-admin@smoke.test'
export const ADMIN_PASSWORD = process.env.TALKBACK_ADMIN_PASSWORD || 'SmokePass123!'

/**
 * Delete a session via admin API. Requires a request context authenticated as admin.
 * Safe to call with a context that is already logged in as admin.
 * Returns true on success or 404 (already gone).
 */
export async function deleteSession(
  request: APIRequestContext,
  sessionId: string
): Promise<boolean> {
  const res = await request.delete(`${API_BASE}/api/sessions/${sessionId}`)
  return res.ok() || res.status() === 404
}

/**
 * Delete a user via admin API. Requires a request context authenticated as admin.
 * Returns true on success, 404 (already gone), or 204.
 */
export async function deleteUserViaAdmin(
  request: APIRequestContext,
  userId: string
): Promise<boolean> {
  const res = await request.delete(`${API_BASE}/api/admin/users/${userId}`)
  return res.ok() || res.status() === 404 || res.status() === 204
}

/**
 * Login as admin and return a request context cookie-authenticated as admin.
 * Caller must call context.dispose() when done.
 * Returns null if admin login fails.
 */
export async function loginAsAdmin(request: APIRequestContext): Promise<boolean> {
  const res = await request.post(`${API_BASE}/api/auth/login`, {
    data: { email: ADMIN_EMAIL, password: ADMIN_PASSWORD },
  })
  return res.ok()
}

/**
 * Admin teardown using a fresh APIRequestContext. Use in test `finally` blocks when the test may hit
 * its timeout — the injected `request` fixture can be disposed before `finally` runs, which throws
 * "Target page, context or browser has been closed".
 */
export async function cleanupSessionAndUserAsAdmin(sessionId: string, userId: string): Promise<void> {
  const ctx = await playwrightApiRequest.newContext()
  try {
    await loginAsAdmin(ctx)
    await deleteSession(ctx, sessionId)
    await deleteUserViaAdmin(ctx, userId)
  } catch {
    /* best-effort after timeout / worker teardown */
  } finally {
    await ctx.dispose()
  }
}

/**
 * Sign up a new user and return their user ID.
 * Also injects session cookies so the browser context is authenticated.
 */
export async function createUserAndLoginWithId(
  context: BrowserContext,
  request: APIRequestContext,
  email: string,
  password = 'SmokePass123!',
  displayName?: string
): Promise<string> {
  const signupRes = await request.post(`${API_BASE}/api/auth/signup`, {
    data: {
      email,
      password,
      display_name: displayName ?? email.split('@')[0],
    },
  })
  const signupData = await signupRes.json()
  const loginRes = await request.post(`${API_BASE}/api/auth/login`, {
    data: { email, password },
  })
  await injectCookiesFromResponse(context, loginRes)
  return signupData.id as string
}
