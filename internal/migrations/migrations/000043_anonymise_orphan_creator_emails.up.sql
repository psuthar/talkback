-- SCRUM-229: anonymise sessions.created_by for any sessions whose creator
-- email no longer maps to a users row. This is the existing-data complement
-- to the SCRUM-229 forward fix in db.DeleteUser, which anonymises sessions
-- on every future user-delete in the same transaction.
--
-- Sessions whose creator user has already been deleted previously (legacy
-- user-deletion path that did not cascade) are left referencing a non-
-- existent users row. The current email-match fallback in UserCanAccessSession
-- still grants access via that string; SCRUM-228 wants to drop that fallback.
-- Setting created_by to NULL here (a) eliminates the orphan-creator state
-- that the verification query for SCRUM-228 detects, (b) keeps the session
-- itself plus any other members' session_memberships rows intact, and
-- (c) is honest — there is no creator email left to point at.
--
-- Idempotent: re-running this migration is a no-op once orphans are gone.

UPDATE sessions
SET created_by = NULL,
    updated_at = now()
WHERE created_by IS NOT NULL
  AND NOT EXISTS (
    SELECT 1 FROM users u WHERE u.email = sessions.created_by
  );
