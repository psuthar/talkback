// Session-scoped role identifiers shared between invitation and membership flows.
// Mirrors backend invitations.Role (see internal/invitations/model.go).
export const ROLE_PARTICIPANT = 'participant'
export const ROLE_CREATOR = 'creator'
export const ROLE_DECISION_MAKER = 'decision_maker'

export const ROLE_VALUES = [ROLE_PARTICIPANT, ROLE_CREATOR, ROLE_DECISION_MAKER]

const ROLE_LABELS = {
  [ROLE_PARTICIPANT]: 'Participant',
  [ROLE_CREATOR]: 'Creator',
  [ROLE_DECISION_MAKER]: 'Decision Maker',
}

// Returns the display label for a session-scoped role. Unknown values fall back
// to the raw role string with underscores replaced by spaces, so a future role
// added on the server doesn't break the UI before the frontend is updated.
export function roleLabel(role) {
  if (role == null || role === '') return ROLE_LABELS[ROLE_PARTICIPANT]
  if (Object.prototype.hasOwnProperty.call(ROLE_LABELS, role)) return ROLE_LABELS[role]
  const s = String(role).replace(/_/g, ' ')
  return s.charAt(0).toUpperCase() + s.slice(1)
}
