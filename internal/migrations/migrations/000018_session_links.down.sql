ALTER TABLE session_chunks DROP CONSTRAINT IF EXISTS session_chunks_source_type_check;
ALTER TABLE session_chunks ADD CONSTRAINT session_chunks_source_type_check
    CHECK (source_type IN ('transcript', 'material'));

DROP INDEX IF EXISTS idx_session_links_session_id;
DROP TABLE IF EXISTS session_links;
