import { describe, it, expect } from 'vitest'
import { isValidEmailFormat } from '../utils/inviteMailto'

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
