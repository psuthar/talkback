// Pure helpers for Google Meet UI state transitions. Extracted from App.jsx so
// the OAuth-callback message mapping and status-response normalization can be
// unit-tested without rendering the whole App tree.
//
// Used by:
// - App.jsx (OAuth ?google_meet=error handler, /api/google-meet/status fetch)
// - subsequent SCRUM-321/322 stories (recordings list + import modal will reuse
//   googleMeetConnectionFromStatus when refetching after Connect).

/**
 * Map an `?google_meet=error&message=<code>` querystring into user-facing copy.
 * Unknown codes pass through (or fall back to a generic message).
 *
 * @param {string|null|undefined} message
 * @returns {string}
 */
export function googleMeetOAuthErrorMessage(message) {
  switch (message) {
    case 'missing_code_or_state':
      return 'Google sign-in was cancelled or incomplete.'
    case 'server_not_configured':
      return 'Google Meet is not configured on the server.'
    case 'exchange_failed':
      return 'Could not complete Google sign-in.'
    case 'save_failed':
      return 'Could not save Google Meet connection.'
    case 'refresh_token_missing':
      return "Google didn't issue offline access. Try again — when Google asks 'Confirm choices', make sure all permissions are checked."
    default:
      return message || 'Google sign-in failed.'
  }
}

/**
 * Map a GET /api/google-meet/recordings error response (`{code, message}`)
 * into user-facing copy. Distinct codes get hand-tuned strings; unknown codes
 * fall back to the server's `message` or a generic.
 *
 * @param {string|null|undefined} code
 * @param {string|null|undefined} message
 * @returns {string}
 */
export function googleMeetRecordingsErrorMessage(code, message) {
  switch (code) {
    case 'google_meet_not_connected':
    case 'meet_auth':
      return 'Google session expired. Disconnect and reconnect Google Meet.'
    case 'meet_missing_scopes':
      return 'Reconnect Google Meet and accept Drive read-only access.'
    case 'workspace_required':
      return 'Google Meet recording import requires Google Workspace Business Standard or higher.'
    default:
      return message || 'Failed to load Google Meet recordings.'
  }
}

/**
 * Resolve a recording's transcript_state ('ready' | 'pending' | 'none') into
 * the badge label, hover tooltip, and a color hint the panel renders.
 * Unknown values map to the same shape as 'none'.
 *
 * @param {string} state
 * @returns {{ label: string, tooltip: string, tone: 'success'|'warning'|'muted' }}
 */
export function googleMeetTranscriptBadge(state) {
  switch (state) {
    case 'ready':
      return { label: 'Transcript', tooltip: 'Transcript available — Q&A will use it.', tone: 'success' }
    case 'pending':
      return { label: 'Transcript pending', tooltip: 'Google is still preparing the transcript. Try again in a few minutes.', tone: 'warning' }
    case 'none':
    default:
      return { label: 'No transcript', tooltip: 'This meeting was not transcribed.', tone: 'muted' }
  }
}

/**
 * Empty-state copy for the recordings list. When the connection is on a
 * non-Workspace account, returns the Workspace-tier explainer instead of the
 * "no recordings found" line.
 *
 * @param {boolean|null|undefined} workspaceEligible
 * @returns {string}
 */
export function googleMeetEmptyStateMessage(workspaceEligible) {
  if (workspaceEligible === false) {
    return "Your Google account isn't on a Workspace edition that records meetings. Recording import is available on Business Standard and higher. You can still create an empty session and upload an MP4 manually."
  }
  return "No Meet recordings found in the last 60 days. Recordings created in Drive's \"Meet Recordings\" folder will also appear here."
}

/**
 * Map a non-2xx response from POST /api/google-meet/import into the inline
 * modal-error copy. Status 409 is the duplicate-title path; 422 surfaces the
 * server message; anything else falls back to a generic.
 *
 * @param {number} status
 * @param {{message?: string}|null|undefined} data
 * @returns {string}
 */
export function googleMeetImportErrorMessage(status, data) {
  const msg = (data && data.message) ? data.message : ''
  if (status === 409) {
    return msg || 'A session with this name already exists. Please use a unique name.'
  }
  if (status === 422) {
    return msg || 'Recording could not be imported.'
  }
  return msg || 'Failed to start import.'
}

/**
 * Inline note rendered above the session-name input in the import modal when
 * the chosen recording has no transcript. Returns empty string for any
 * other transcript_state ('ready' / 'pending').
 *
 * @param {string} transcriptState
 * @returns {string}
 */
export function googleMeetImportTranscriptNote(transcriptState) {
  if (transcriptState !== 'none') return ''
  return "This recording has no transcript. You'll get video and AI-generated answers about anything visible on screen, but Q&A quality is best when a transcript is available."
}

/**
 * Normalize the JSON body of GET /api/google-meet/status into the
 * googleMeetConnection shape the SPA stores. Returns null when the integration
 * is disabled or the user is not connected.
 *
 * @param {object|null|undefined} data
 * @returns {{ google_email: string|null, google_user_id: string|null, workspace_eligible: boolean|null } | null}
 */
export function googleMeetConnectionFromStatus(data) {
  if (!data || data.enabled !== true || data.connected !== true) return null
  return {
    google_email: data.google_email || null,
    google_user_id: data.google_user_id || null,
    workspace_eligible: typeof data.workspace_eligible === 'boolean' ? data.workspace_eligible : null,
  }
}
