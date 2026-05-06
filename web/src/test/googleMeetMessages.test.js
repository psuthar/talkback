import { describe, it, expect } from 'vitest'
import {
  googleMeetOAuthErrorMessage,
  googleMeetConnectionFromStatus,
} from '../googleMeetMessages'

describe('googleMeetOAuthErrorMessage', () => {
  it('maps known error codes to user-friendly copy', () => {
    expect(googleMeetOAuthErrorMessage('missing_code_or_state')).toMatch(/cancelled or incomplete/)
    expect(googleMeetOAuthErrorMessage('server_not_configured')).toMatch(/not configured on the server/)
    expect(googleMeetOAuthErrorMessage('exchange_failed')).toMatch(/Could not complete Google sign-in/)
    expect(googleMeetOAuthErrorMessage('save_failed')).toMatch(/save Google Meet connection/)
  })

  it('returns the refresh_token_missing reconnect hint with the Confirm-choices guidance', () => {
    const msg = googleMeetOAuthErrorMessage('refresh_token_missing')
    expect(msg).toMatch(/offline access/)
    expect(msg).toMatch(/Confirm choices/)
  })

  it('passes unknown codes through and falls back when message is empty', () => {
    expect(googleMeetOAuthErrorMessage('something_else')).toBe('something_else')
    expect(googleMeetOAuthErrorMessage(null)).toBe('Google sign-in failed.')
    expect(googleMeetOAuthErrorMessage('')).toBe('Google sign-in failed.')
    expect(googleMeetOAuthErrorMessage(undefined)).toBe('Google sign-in failed.')
  })
})

describe('googleMeetConnectionFromStatus', () => {
  it('returns null when disabled, not connected, or null/undefined', () => {
    expect(googleMeetConnectionFromStatus(null)).toBeNull()
    expect(googleMeetConnectionFromStatus(undefined)).toBeNull()
    expect(googleMeetConnectionFromStatus({ enabled: false })).toBeNull()
    expect(googleMeetConnectionFromStatus({ enabled: true, connected: false })).toBeNull()
  })

  it('returns the canonical shape when enabled and connected', () => {
    const conn = googleMeetConnectionFromStatus({
      enabled: true,
      connected: true,
      google_email: 'alice@workspace.example',
      google_user_id: 'sub-1',
      workspace_eligible: true,
    })
    expect(conn).toEqual({
      google_email: 'alice@workspace.example',
      google_user_id: 'sub-1',
      workspace_eligible: true,
    })
  })

  it('preserves workspace_eligible:false (Workspace tier warning)', () => {
    const conn = googleMeetConnectionFromStatus({
      enabled: true,
      connected: true,
      google_email: 'consumer@gmail.com',
      workspace_eligible: false,
    })
    expect(conn?.workspace_eligible).toBe(false)
  })

  it('coerces missing or non-boolean workspace_eligible to null', () => {
    const conn = googleMeetConnectionFromStatus({
      enabled: true,
      connected: true,
      google_email: 'x@example.com',
    })
    expect(conn?.workspace_eligible).toBeNull()
    const conn2 = googleMeetConnectionFromStatus({
      enabled: true,
      connected: true,
      workspace_eligible: 'yes',
    })
    expect(conn2?.workspace_eligible).toBeNull()
  })

  it('null email/user_id default to null fields, not undefined', () => {
    const conn = googleMeetConnectionFromStatus({
      enabled: true,
      connected: true,
    })
    expect(conn).toEqual({
      google_email: null,
      google_user_id: null,
      workspace_eligible: null,
    })
  })
})
