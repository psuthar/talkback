export function shouldShowCreatorParticipantCta({ hasValidSession, participantUrl, sessionUserMode }) {
  return Boolean(hasValidSession && participantUrl && sessionUserMode === 'creator')
}

export function shouldShowAppAuthCluster({
  isParticipantMode,
  hasValidSession,
  showCreatorParticipantCta,
}) {
  // Participant shell owns identity/actions in participant mode.
  if (isParticipantMode && hasValidSession) return false
  // In creator mode with an active session, keep top-right focused on participant view CTA.
  if (showCreatorParticipantCta) return false
  return true
}
