-- SCRUM-226: backfill session_memberships rows for session creators that
-- never had one. Going forward, db.CreateSession inserts the creator membership
-- in the same transaction; this migration covers existing data created before
-- that change.
--
-- For every session whose created_by email maps to a users row and which has
-- no membership for that user, insert a row with role='creator'. Sessions
-- whose created_by does not match any users.email row are left alone (the
-- legacy email-match access path in UserCanAccessSession still grants those
-- creators access). joined_at is mirrored from sessions.created_at so the
-- backfilled membership timestamps make sense relative to session age.

INSERT INTO session_memberships (session_id, user_id, role, invited_by_user_id, joined_at)
SELECT
  s.id,
  u.id,
  'creator',
  NULL,
  s.created_at
FROM sessions s
JOIN users u ON u.email = s.created_by
WHERE s.created_by IS NOT NULL
  AND NOT EXISTS (
    SELECT 1 FROM session_memberships sm
    WHERE sm.session_id = s.id AND sm.user_id = u.id
  );
