import { describe, it, expect } from 'vitest'
import {
  parseSessionIdFromPathname,
  parseSessionNavigationFromLocation,
  canonicalSessionRelativePath,
  buildCanonicalSessionAbsoluteUrl,
  isLikelySessionId,
  historyPathFromLocation,
  sessionLoadMessageForStatus,
  CANONICAL_SESSION_PREFIX,
} from '../sessionNavigation'

const UUID = '550e8400-e29b-41d4-a716-446655440000'

describe('CANONICAL_SESSION_PREFIX', () => {
  it('is /app/sessions', () => {
    expect(CANONICAL_SESSION_PREFIX).toBe('/app/sessions')
  })
})

describe('parseSessionIdFromPathname', () => {
  it('extracts session id from canonical path', () => {
    expect(parseSessionIdFromPathname(`/app/sessions/${UUID}`)).toBe(UUID)
  })

  it('handles trailing slash', () => {
    expect(parseSessionIdFromPathname(`/app/sessions/${UUID}/`)).toBe(UUID)
  })

  it('returns null for root path', () => {
    expect(parseSessionIdFromPathname('/')).toBeNull()
  })

  it('returns null for non-session path', () => {
    expect(parseSessionIdFromPathname('/login')).toBeNull()
  })

  it('returns null for undefined', () => {
    expect(parseSessionIdFromPathname(undefined)).toBeNull()
  })

  it('returns null for null', () => {
    expect(parseSessionIdFromPathname(null)).toBeNull()
  })
})

describe('parseSessionNavigationFromLocation', () => {
  it('parses session id from canonical pathname', () => {
    const result = parseSessionNavigationFromLocation({ pathname: `/app/sessions/${UUID}`, search: '' })
    expect(result.sessionId).toBe(UUID)
    expect(result.sessionSource).toBe('path')
  })

  it('parses session id from query param', () => {
    const result = parseSessionNavigationFromLocation({ pathname: '/', search: `?session=${UUID}` })
    expect(result.sessionId).toBe(UUID)
    expect(result.sessionSource).toBe('query')
  })

  it('prefers path over query param', () => {
    const other = '123e4567-e89b-12d3-a456-426614174000'
    const result = parseSessionNavigationFromLocation({
      pathname: `/app/sessions/${UUID}`,
      search: `?session=${other}`
    })
    expect(result.sessionId).toBe(UUID)
    expect(result.sessionSource).toBe('path')
  })

  it('returns null sessionId when no session in URL', () => {
    const result = parseSessionNavigationFromLocation({ pathname: '/login', search: '' })
    expect(result.sessionId).toBeNull()
    expect(result.sessionSource).toBeNull()
  })

  it('parses mode from query string', () => {
    const result = parseSessionNavigationFromLocation({ pathname: '/', search: `?session=${UUID}&mode=view` })
    expect(result.mode).toBe('view')
  })

  it('parses api_base from query string', () => {
    const result = parseSessionNavigationFromLocation({ pathname: '/', search: '?api_base=https://api.example.com' })
    expect(result.apiFromQuery).toBe('https://api.example.com')
  })
})

describe('canonicalSessionRelativePath', () => {
  it('returns path without query for no queryParams', () => {
    expect(canonicalSessionRelativePath(UUID)).toBe(`/app/sessions/${UUID}`)
  })

  it('includes mode in query string', () => {
    expect(canonicalSessionRelativePath(UUID, { mode: 'view' })).toBe(`/app/sessions/${UUID}?mode=view`)
  })

  it('includes api in query string', () => {
    const result = canonicalSessionRelativePath(UUID, { api: 'https://api.example.com' })
    expect(result).toContain('api=')
  })

  it('encodes session id', () => {
    // plain UUID should remain unchanged (no special chars)
    const path = canonicalSessionRelativePath(UUID)
    expect(path).toContain(UUID)
  })
})

describe('buildCanonicalSessionAbsoluteUrl', () => {
  it('prepends base URL to relative path', () => {
    const result = buildCanonicalSessionAbsoluteUrl('https://app.example.com', UUID)
    expect(result).toBe(`https://app.example.com/app/sessions/${UUID}`)
  })

  it('strips trailing slash from base', () => {
    const result = buildCanonicalSessionAbsoluteUrl('https://app.example.com/', UUID)
    expect(result).toBe(`https://app.example.com/app/sessions/${UUID}`)
  })

  it('returns relative path when base is empty', () => {
    const result = buildCanonicalSessionAbsoluteUrl('', UUID)
    expect(result).toBe(`/app/sessions/${UUID}`)
  })
})

describe('isLikelySessionId', () => {
  it('accepts valid UUID v4', () => {
    expect(isLikelySessionId(UUID)).toBe(true)
  })

  it('accepts UUID with uppercase', () => {
    expect(isLikelySessionId(UUID.toUpperCase())).toBe(true)
  })

  it('rejects short string', () => {
    expect(isLikelySessionId('abc123')).toBe(false)
  })

  it('rejects null', () => {
    expect(isLikelySessionId(null)).toBe(false)
  })

  it('rejects undefined', () => {
    expect(isLikelySessionId(undefined)).toBe(false)
  })

  it('rejects number', () => {
    expect(isLikelySessionId(42)).toBe(false)
  })
})

describe('historyPathFromLocation', () => {
  it('returns canonical path for session in query string', () => {
    const result = historyPathFromLocation({ pathname: '/', search: `?session=${UUID}` })
    expect(result).toBe(`/app/sessions/${UUID}`)
  })

  it('returns canonical path for session in pathname', () => {
    const result = historyPathFromLocation({ pathname: `/app/sessions/${UUID}`, search: '' })
    expect(result).toBe(`/app/sessions/${UUID}`)
  })

  it('preserves non-session paths as-is', () => {
    const result = historyPathFromLocation({ pathname: '/login', search: '', hash: '' })
    expect(result).toBe('/login')
  })

  it('preserves query string on non-session paths', () => {
    const result = historyPathFromLocation({ pathname: '/login', search: '?redirect=1', hash: '' })
    expect(result).toBe('/login?redirect=1')
  })

  it('includes mode param in canonical path', () => {
    const result = historyPathFromLocation({ pathname: '/', search: `?session=${UUID}&mode=view` })
    expect(result).toContain('mode=view')
  })
})

describe('sessionLoadMessageForStatus', () => {
  it('returns not found message for 404', () => {
    expect(sessionLoadMessageForStatus(404)).toContain('does not exist')
  })

  it('returns sign in message for 401', () => {
    expect(sessionLoadMessageForStatus(401)).toContain('Sign in')
  })

  it('returns access denied message for 403', () => {
    expect(sessionLoadMessageForStatus(403)).toContain('access')
  })

  it('returns generic message with status code for other errors', () => {
    expect(sessionLoadMessageForStatus(500)).toContain('500')
  })
})
