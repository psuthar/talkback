-- Revert session_chunks.source_type back to the baseline two-value enum.
-- Any rows with source_type NOT IN ('transcript', 'material') will block constraint
-- re-creation; delete or migrate them before applying this down migration.
ALTER TABLE session_chunks DROP CONSTRAINT IF EXISTS session_chunks_source_type_check;
ALTER TABLE session_chunks ADD CONSTRAINT session_chunks_source_type_check
    CHECK (source_type IN ('transcript', 'material'));
