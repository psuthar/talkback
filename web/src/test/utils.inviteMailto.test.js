import { describe, it, expect } from 'vitest'
import { isValidEmailFormat, buildInviteMailto, buildInviteMessageBody } from '../utils/inviteMailto'

describe('isValidEmailFormat', () => {
  it('accepts a valid email', () => {
    expect(isValidEmailFormat('user@example.com')).toBe(true)
  })

  it('accepts email with subdomain', () => {
    expect(isValidEmailFormat('user@mail.example.com')).toBe(true)
  })

  it('rejects empty string', () => {
    expect(isValidEmailFormat('')).toBe(false)
  })

  it('rejects null', () => {
    expect(isValidEmailFormat(null)).toBe(false)
  })

  it('rejects undefined', () => {
    expect(isValidEmailFormat(undefined)).toBe(false)
  })

  it('rejects missing @', () => {
    expect(isValidEmailFormat('userexample.com')).toBe(false)
  })

  it('rejects @ at start', () => {
    expect(isValidEmailFormat('@example.com')).toBe(false)
  })

  it('rejects @ at end', () => {
    expect(isValidEmailFormat('user@')).toBe(false)
  })

  it('rejects domain without dot', () => {
    expect(isValidEmailFormat('user@localhost')).toBe(false)
  })

  it('handles whitespace-only input', () => {
    expect(isValidEmailFormat('   ')).toBe(false)
  })
})

describe('buildInviteMailto', () => {
  it('returns null when invited_email is missing', () => {
    expect(buildInviteMailto({ accept_url: 'https://example.com/accept' })).toBeNull()
  })

  it('returns null when accept_url is missing', () => {
    expect(buildInviteMailto({ invited_email: 'user@example.com' })).toBeNull()
  })

  it('returns null for null input', () => {
    expect(buildInviteMailto(null)).toBeNull()
  })

  it('returns a mailto: URL with correct recipient', () => {
    const result = buildInviteMailto({
      invited_email: 'guest@example.com',
      accept_url: 'https://app.example.com/accept/abc123',
      session_title: 'Test Session',
      inviter_name: 'Alice',
      inviter_email: 'alice@example.com',
    })
    expect(result).toMatch(/^mailto:/)
    expect(result).toContain(encodeURIComponent('guest@example.com'))
    expect(result).toContain('subject=')
    expect(result).toContain('body=')
  })

  it('includes the accept URL in the body', () => {
    const result = buildInviteMailto({
      invited_email: 'guest@example.com',
      accept_url: 'https://app.example.com/accept/abc123',
    })
    expect(result).toContain(encodeURIComponent('https://app.example.com/accept/abc123'))
  })
})

describe('buildInviteMessageBody', () => {
  it('includes session title', () => {
    const body = buildInviteMessageBody({ session_title: 'My Session', accept_url: 'https://x.com/a' })
    expect(body).toContain('My Session')
  })

  it('falls back to "a session" when no title', () => {
    const body = buildInviteMessageBody({ accept_url: 'https://x.com/a' })
    expect(body).toContain('a session')
  })

  it('includes the accept URL', () => {
    const body = buildInviteMessageBody({ accept_url: 'https://example.com/accept/xyz' })
    expect(body).toContain('https://example.com/accept/xyz')
  })

  it('uses inviter_email when available', () => {
    const body = buildInviteMessageBody({ inviter_email: 'boss@company.com', accept_url: 'https://x.com/a' })
    expect(body).toContain('boss@company.com')
  })

  it('falls back to inviter_name when no inviter_email', () => {
    const body = buildInviteMessageBody({ inviter_name: 'Bob', accept_url: 'https://x.com/a' })
    expect(body).toContain('Bob')
  })

  it('falls back to "Someone" when neither inviter field is present', () => {
    const body = buildInviteMessageBody({ accept_url: 'https://x.com/a' })
    expect(body).toContain('Someone')
  })

  it('mentions expiry when expires_at is provided', () => {
    const body = buildInviteMessageBody({
      accept_url: 'https://x.com/a',
      expires_at: '2026-12-31T23:59:00Z'
    })
    expect(body).toMatch(/expires/i)
    expect(body).not.toContain('soon')
  })

  it('uses "soon" when no expires_at', () => {
    const body = buildInviteMessageBody({ accept_url: 'https://x.com/a' })
    expect(body).toContain('soon')
  })
})
