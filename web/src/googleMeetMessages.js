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
