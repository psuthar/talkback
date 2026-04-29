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
