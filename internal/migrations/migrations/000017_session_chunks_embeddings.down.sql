DROP INDEX IF EXISTS idx_session_chunk_embeddings_session_id;
DROP INDEX IF EXISTS idx_session_chunk_embeddings_chunk_id;
DROP TABLE IF EXISTS session_chunk_embeddings;
DROP INDEX IF EXISTS idx_session_chunks_session_id;
DROP TABLE IF EXISTS session_chunks;
ALTER TABLE sessions DROP COLUMN IF EXISTS index_updated_at;
ALTER TABLE sessions DROP COLUMN IF EXISTS index_status;
