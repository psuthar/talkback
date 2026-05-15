-- SCRUM-403 down: restore the prior strict UNIQUE(session_id, source) on
-- session_processing_jobs and drop the external_recording_id column +
-- partial UNIQUE on video_sources.

DROP INDEX IF EXISTS idx_video_sources_session_provider_external_id;

ALTER TABLE video_sources
    DROP COLUMN IF EXISTS external_recording_id;

DROP INDEX IF EXISTS idx_session_processing_jobs_session_source_meeting;

CREATE UNIQUE INDEX IF NOT EXISTS idx_session_processing_jobs_session_source
    ON session_processing_jobs (session_id, source);
