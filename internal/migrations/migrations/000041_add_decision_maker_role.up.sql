-- Add 'decision_maker' as an accepted session-scoped role for invitations and memberships.
-- Backward-compatible: existing 'participant' and 'creator' rows are unaffected.

ALTER TABLE session_memberships
  DROP CONSTRAINT IF EXISTS session_memberships_role_check;

ALTER TABLE session_memberships
  ADD CONSTRAINT session_memberships_role_check
  CHECK (role IN ('participant', 'creator', 'decision_maker'));

ALTER TABLE session_invitations
  DROP CONSTRAINT IF EXISTS session_invitations_invited_role_check;

ALTER TABLE session_invitations
  ADD CONSTRAINT session_invitations_invited_role_check
  CHECK (invited_role IN ('participant', 'creator', 'decision_maker'));
