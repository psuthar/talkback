-- SCRUM-410 down: the backfill is a one-way data UPDATE. We CANNOT
-- distinguish backfilled rows from rows authored after this migration,
-- so a true rollback isn't possible. This down migration is intentionally
-- a no-op — invoking it does not error, but does not undo the backfill
-- either. To roll back end-to-end, restore from a pre-migration DB
-- snapshot.
SELECT 1;
