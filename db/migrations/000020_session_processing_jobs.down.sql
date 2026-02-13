ALTER TABLE sessions DROP COLUMN IF EXISTS processing_updated_at;
ALTER TABLE sessions DROP COLUMN IF EXISTS processing_state;

DROP INDEX IF EXISTS idx_session_processing_jobs_session_id;
DROP INDEX IF EXISTS idx_session_processing_jobs_state_retry;
DROP INDEX IF EXISTS idx_session_processing_jobs_session_source;
DROP TABLE IF EXISTS session_processing_jobs;
