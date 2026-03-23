-- Revert: narrow source CHECK constraint back to 'zoom' only.
ALTER TABLE session_processing_jobs
    DROP CONSTRAINT IF EXISTS session_processing_jobs_source_check;

ALTER TABLE session_processing_jobs
    ADD CONSTRAINT session_processing_jobs_source_check
    CHECK (source IN ('zoom'));
