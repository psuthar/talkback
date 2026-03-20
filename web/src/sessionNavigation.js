/**
 * Canonical browser routes for session deep links.
 * Backend owns JSON/API under /sessions/:id — SPA uses /app/sessions/:id to avoid collisions.
 */

export const CANONICAL_SESSION_PREFIX = '/app/sessions'

const PATH_RE = /^\/app\/sessions\/([^/]+)\/?$/

/** @param {string} [pathname] */
export function parseSessionIdFromPathname(pathname) {
  if (!pathname) return null
  const m = pathname.replace(/\/$/, '') || '/'
  const match = m.match(PATH_RE)
  return match ? match[1].trim() : null
}

/**
 * Resolve session id and mode from the current URL.
 * Precedence: pathname (/app/sessions/:id) wins over ?session= when both are present.
 *
 * @param {{ pathname?: string, search?: string }} loc
 * @returns {{ sessionId: string|null, mode: string|null, apiFromQuery: string|null, sessionSource: 'path'|'query'|null }}
 */
export function parseSessionNavigationFromLocation(loc) {
  const pathname = loc.pathname || '/'
  const params = new URLSearchParams(loc.search || '')
  const pathSessionId = parseSessionIdFromPathname(pathname)
  const querySessionId = params.get('session')
  const sessionId = pathSessionId || querySessionId || null
  const sessionSource = pathSessionId ? 'path' : (querySessionId ? 'query' : null)
  return {
    sessionId,
    mode: params.get('mode'),
    apiFromQuery: params.get('api') || params.get('api_base'),
    sessionSource,
  }
}

/**
 * Absolute URL for opening a session in the SPA (same origin).
 * @param {string} sessionId
 * @param {{ mode?: string, api?: string, zoom?: string }} [queryParams]
 */
export function buildCanonicalSessionUrl(sessionId, queryParams = {}) {
  const origin = typeof window !== 'undefined' ? window.location.origin : ''
  const sp = new URLSearchParams()
  if (queryParams.mode) sp.set('mode', queryParams.mode)
  if (queryParams.api) sp.set('api', queryParams.api)
  if (queryParams.zoom) sp.set('zoom', queryParams.zoom)
  const q = sp.toString()
  return `${origin}${CANONICAL_SESSION_PREFIX}/${encodeURIComponent(sessionId)}${q ? `?${q}` : ''}`
}
