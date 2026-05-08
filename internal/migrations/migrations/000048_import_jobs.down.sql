ALTER TABLE sessions DROP CONSTRAINT IF EXISTS sessions_source_provider_check;
ALTER TABLE sessions ADD CONSTRAINT sessions_source_provider_check
    CHECK (source_provider IN ('zoom', 'upload', 'teams', 'google_meet'));

DROP INDEX IF EXISTS idx_import_jobs_state;
DROP INDEX IF EXISTS idx_import_jobs_session_id;
DROP INDEX IF EXISTS idx_import_jobs_user_id;
DROP TABLE IF EXISTS import_jobs;
