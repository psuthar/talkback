export function shouldShowAppAuthCluster({
  isParticipantMode,
  hasValidSession,
  sessionUserMode,
}) {
  // Participant shell owns identity/actions in participant mode.
  if (isParticipantMode && hasValidSession) return false
  // In creator mode with an active session, top-right controls live in creator session menu.
  if (sessionUserMode === 'creator' && hasValidSession) return false
  return true
}
