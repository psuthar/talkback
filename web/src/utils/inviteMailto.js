/**
 * Limited email format check: non-empty, single @, non-empty local and domain with at least one dot in domain.
 * @param {string} email
 * @returns {boolean}
 */
export function isValidEmailFormat(email) {
  if (!email || typeof email !== 'string') return false
  const s = email.trim()
  if (s.length === 0) return false
  const at = s.indexOf('@')
  if (at <= 0 || at === s.length - 1) return false
  const local = s.slice(0, at)
  const domain = s.slice(at + 1)
  if (!local || !domain) return false
  if (domain.indexOf('.') === -1) return false
  return true
}

/**
 * Builds a mailto: URL for the temporary invitation delivery hack.
 * invitation: { invited_email, accept_url, session_title, inviter_name, inviter_email, expires_at }
 * Uses encodeURIComponent for subject/body so spaces are %20 (not +) for better client display.
 */
export function buildInviteMailto(invitation) {
  if (!invitation?.invited_email || !invitation?.accept_url) return null
  const subject = "You've been invited to a TalkBack session"
  const body = buildInviteMessageBody(invitation)
  const to = invitation.invited_email
  const subjectEnc = encodeURIComponent(subject)
  const bodyEnc = encodeURIComponent(body)
  return `mailto:${encodeURIComponent(to)}?subject=${subjectEnc}&body=${bodyEnc}`
}

/**
 * Plain-text body for the invitation email (for mailto and copy-message fallback).
 * Uses inviter_email when available; does not expose invited user role.
 * invitation: { session_title, inviter_name, inviter_email, accept_url, expires_at }
 */
export function buildInviteMessageBody(invitation) {
  const inviterEmail = invitation?.inviter_email || invitation?.inviter_name || 'Someone'
  const sessionTitle = invitation?.session_title || 'a session'
  const acceptUrl = invitation?.accept_url || ''
  const expiresAt = invitation?.expires_at
    ? new Date(invitation.expires_at).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })
    : 'soon'

  return [
    'Hi,',
    '',
    `${inviterEmail} invited you to join the TalkBack session "${sessionTitle}".`,
    '',
    'TalkBack lets you review session content and ask questions asynchronously.',
    '',
    'Accept your invitation here:',
    acceptUrl ? `<${acceptUrl}>` : '',
    '',
    `This invitation link expires on ${expiresAt}.`
  ].join('\n')
}
