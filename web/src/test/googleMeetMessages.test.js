import { describe, it, expect } from 'vitest'
import {
  googleMeetOAuthErrorMessage,
  googleMeetConnectionFromStatus,
  googleMeetRecordingsErrorMessage,
  googleMeetTranscriptBadge,
  googleMeetEmptyStateMessage,
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

describe('googleMeetRecordingsErrorMessage', () => {
  it('maps reconnect-flavored codes to a "disconnect and reconnect" hint', () => {
    expect(googleMeetRecordingsErrorMessage('google_meet_not_connected', null)).toMatch(/Disconnect and reconnect/)
    expect(googleMeetRecordingsErrorMessage('meet_auth', null)).toMatch(/Disconnect and reconnect/)
  })

  it('maps meet_missing_scopes to a Drive-readonly reconnect prompt', () => {
    expect(googleMeetRecordingsErrorMessage('meet_missing_scopes', null)).toMatch(/Drive read-only/)
  })

  it('maps workspace_required to the Workspace-tier explainer', () => {
    expect(googleMeetRecordingsErrorMessage('workspace_required', null)).toMatch(/Workspace Business Standard/)
  })

  it('falls back to the server message and then a generic when code is unknown', () => {
    expect(googleMeetRecordingsErrorMessage('weird_code', 'server says')).toBe('server says')
    expect(googleMeetRecordingsErrorMessage(null, null)).toBe('Failed to load Google Meet recordings.')
  })
})

describe('googleMeetTranscriptBadge', () => {
  it('returns success tone with "Transcript" label for ready', () => {
    const b = googleMeetTranscriptBadge('ready')
    expect(b.label).toBe('Transcript')
    expect(b.tone).toBe('success')
  })

  it('returns warning tone with the "still preparing" tooltip for pending', () => {
    const b = googleMeetTranscriptBadge('pending')
    expect(b.label).toBe('Transcript pending')
    expect(b.tone).toBe('warning')
    expect(b.tooltip).toMatch(/still preparing/)
  })

  it('returns muted tone with "No transcript" for none and unknown values', () => {
    expect(googleMeetTranscriptBadge('none').label).toBe('No transcript')
    expect(googleMeetTranscriptBadge('').label).toBe('No transcript')
    expect(googleMeetTranscriptBadge('something_else').tone).toBe('muted')
  })
})

describe('googleMeetEmptyStateMessage', () => {
  it('returns the Workspace-tier explainer when workspace_eligible is false', () => {
    const msg = googleMeetEmptyStateMessage(false)
    expect(msg).toMatch(/Workspace edition/)
    expect(msg).toMatch(/Business Standard/)
  })

  it('returns the no-recordings-found copy for true / null / undefined', () => {
    const generic = "No Meet recordings found in the last 60 days"
    expect(googleMeetEmptyStateMessage(true)).toContain(generic)
    expect(googleMeetEmptyStateMessage(null)).toContain(generic)
    expect(googleMeetEmptyStateMessage(undefined)).toContain(generic)
  })
})
