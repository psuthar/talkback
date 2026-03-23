-- Widen session_processing_jobs.source CHECK constraint to include 'teams'.
-- The inline CHECK created in migration 000020 is auto-named
-- session_processing_jobs_source_check by PostgreSQL.
ALTER TABLE session_processing_jobs
    DROP CONSTRAINT IF EXISTS session_processing_jobs_source_check;

ALTER TABLE session_processing_jobs
    ADD CONSTRAINT session_processing_jobs_source_check
    CHECK (source IN ('zoom', 'teams'));
