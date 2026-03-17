import type { BrowserContext, APIRequestContext, APIResponse } from '@playwright/test'

/** Backend API base URL — must match the running go API server. */
export const API_BASE = 'http://localhost:8081'

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

  const parsed = cookieDefs.map((line) => {
    const parts = line.split(';').map((p) => p.trim())
    const [nameVal] = parts
    const eqIdx = nameVal.indexOf('=')
    const name = nameVal.slice(0, eqIdx).trim()
    const value = nameVal.slice(eqIdx + 1).trim()
    const pathPart = parts.find((p) => p.toLowerCase().startsWith('path='))
    return {
      name,
      value,
      domain: 'localhost',
      path: pathPart ? pathPart.split('=')[1].trim() : '/',
    }
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
