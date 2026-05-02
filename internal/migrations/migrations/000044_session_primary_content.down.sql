-- SCRUM-269: revert the primary_content_kind / pointer columns.
-- Reversible — no destructive data mutation in the up migration.

DROP INDEX IF EXISTS idx_sessions_primary_session_link_id;
DROP INDEX IF EXISTS idx_sessions_primary_material_id;

ALTER TABLE sessions
    DROP COLUMN IF EXISTS primary_session_link_id,
    DROP COLUMN IF EXISTS primary_material_id,
    DROP COLUMN IF EXISTS primary_content_kind;
