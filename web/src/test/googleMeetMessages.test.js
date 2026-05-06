import { describe, it, expect } from 'vitest'
import {
  googleMeetOAuthErrorMessage,
  googleMeetConnectionFromStatus,
  googleMeetRecordingsErrorMessage,
  googleMeetTranscriptBadge,
  googleMeetEmptyStateMessage,
  googleMeetImportErrorMessage,
  googleMeetImportTranscriptNote,
  googleMeetWaitingCopy,
  googleMeetShouldShowReconnect,
  googleMeetTerminalErrorState,
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

describe('googleMeetImportErrorMessage', () => {
  it('returns the duplicate-title copy on 409 when server omits message', () => {
    expect(googleMeetImportErrorMessage(409, null)).toMatch(/already exists/)
    expect(googleMeetImportErrorMessage(409, {})).toMatch(/already exists/)
  })

  it('uses the server message on 409 when present', () => {
    expect(googleMeetImportErrorMessage(409, { message: 'Custom dup msg' })).toBe('Custom dup msg')
  })

  it('passes 422 server message through and falls back when missing', () => {
    expect(googleMeetImportErrorMessage(422, { message: 'transcript_processing' })).toBe('transcript_processing')
    expect(googleMeetImportErrorMessage(422, null)).toMatch(/could not be imported/)
  })

  it('falls back to a generic on other statuses', () => {
    expect(googleMeetImportErrorMessage(500, null)).toBe('Failed to start import.')
    expect(googleMeetImportErrorMessage(0, { message: 'network' })).toBe('network')
  })
})

describe('googleMeetImportTranscriptNote', () => {
  it("returns the no-transcript explainer only when transcript_state === 'none'", () => {
    expect(googleMeetImportTranscriptNote('none')).toMatch(/no transcript/)
  })

  it("returns empty string for ready / pending / unknown / falsy", () => {
    expect(googleMeetImportTranscriptNote('ready')).toBe('')
    expect(googleMeetImportTranscriptNote('pending')).toBe('')
    expect(googleMeetImportTranscriptNote('something_else')).toBe('')
    expect(googleMeetImportTranscriptNote('')).toBe('')
    expect(googleMeetImportTranscriptNote(undefined)).toBe('')
  })
})

describe('googleMeetWaitingCopy', () => {
  it("returns the Google-finishing copy when sessionSource === 'google_meet'", () => {
    const msg = googleMeetWaitingCopy('google_meet')
    expect(msg).toMatch(/Google to finish preparing/)
  })

  it('returns null for other session sources (caller falls through to existing copy)', () => {
    expect(googleMeetWaitingCopy('zoom')).toBeNull()
    expect(googleMeetWaitingCopy('teams')).toBeNull()
    expect(googleMeetWaitingCopy('upload')).toBeNull()
    expect(googleMeetWaitingCopy('')).toBeNull()
    expect(googleMeetWaitingCopy(undefined)).toBeNull()
  })
})

describe('googleMeetShouldShowReconnect', () => {
  it('returns true for the auth-related Meet error codes', () => {
    expect(googleMeetShouldShowReconnect('meet_auth')).toBe(true)
    expect(googleMeetShouldShowReconnect('meet_not_connected')).toBe(true)
    expect(googleMeetShouldShowReconnect('meet_refresh_token_missing')).toBe(true)
    expect(googleMeetShouldShowReconnect('google_meet_auth')).toBe(true)
    expect(googleMeetShouldShowReconnect('google_meet_not_connected')).toBe(true)
  })

  it('returns false for unrelated codes and empty/null/undefined', () => {
    expect(googleMeetShouldShowReconnect('zoom_auth')).toBe(false)
    expect(googleMeetShouldShowReconnect('teams_auth')).toBe(false)
    expect(googleMeetShouldShowReconnect('something_else')).toBe(false)
    expect(googleMeetShouldShowReconnect('')).toBe(false)
    expect(googleMeetShouldShowReconnect(null)).toBe(false)
    expect(googleMeetShouldShowReconnect(undefined)).toBe(false)
  })
})

describe('googleMeetTerminalErrorState', () => {
  it('returns terminal copy + suppressRetry for meet_workspace_required', () => {
    const t = googleMeetTerminalErrorState('meet_workspace_required')
    expect(t).not.toBeNull()
    expect(t.copy).toMatch(/Workspace Business Standard/)
    expect(t.suppressRetry).toBe(true)
  })

  it('returns terminal copy for meet_recording_deleted', () => {
    const t = googleMeetTerminalErrorState('meet_recording_deleted')
    expect(t.copy).toMatch(/deleted on Google/)
    expect(t.suppressRetry).toBe(true)
  })

  it('returns terminal copy for google_meet_no_drive_file', () => {
    const t = googleMeetTerminalErrorState('google_meet_no_drive_file')
    expect(t.copy).toMatch(/did not produce a downloadable file/)
  })

  it('returns null for retryable / unknown / falsy codes', () => {
    expect(googleMeetTerminalErrorState('meet_auth')).toBeNull()
    expect(googleMeetTerminalErrorState('something_else')).toBeNull()
    expect(googleMeetTerminalErrorState('')).toBeNull()
    expect(googleMeetTerminalErrorState(null)).toBeNull()
    expect(googleMeetTerminalErrorState(undefined)).toBeNull()
  })
})
