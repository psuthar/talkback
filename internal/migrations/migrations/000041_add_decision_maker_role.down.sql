-- Revert: remove 'decision_maker' from accepted roles. Any existing decision_maker rows
-- must be cleaned up manually before applying this down migration; otherwise the new
-- CHECK constraint will fail to apply.

ALTER TABLE session_memberships
  DROP CONSTRAINT IF EXISTS session_memberships_role_check;

ALTER TABLE session_memberships
  ADD CONSTRAINT session_memberships_role_check
  CHECK (role IN ('participant', 'creator'));

ALTER TABLE session_invitations
  DROP CONSTRAINT IF EXISTS session_invitations_invited_role_check;

ALTER TABLE session_invitations
  ADD CONSTRAINT session_invitations_invited_role_check
  CHECK (invited_role IN ('participant', 'creator'));
