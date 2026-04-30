-- SCRUM-226: there is no fully reversible down for the backfill once the
-- application has started inserting creator memberships transactionally on
-- session creation — old (backfill) and new (transactional) creator rows
-- become indistinguishable. The safe rollback is a no-op; the application can
-- continue to function with the rows present even if the migration is
-- considered logically reverted.
SELECT 1;
