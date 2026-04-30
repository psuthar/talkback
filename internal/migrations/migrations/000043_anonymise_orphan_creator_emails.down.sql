-- SCRUM-229: anonymising orphan sessions.created_by has no fully-reversible
-- down — once the original creator email is dropped, the linkage cannot be
-- restored without an external audit trail. Rollback is a no-op; re-running
-- the up migration is idempotent.
SELECT 1;
