DROP INDEX IF EXISTS idx_google_meet_connections_creator_identity;
DROP TABLE IF EXISTS google_meet_connections;

ALTER TABLE session_processing_jobs
    DROP CONSTRAINT IF EXISTS session_processing_jobs_source_check;

ALTER TABLE session_processing_jobs
    ADD CONSTRAINT session_processing_jobs_source_check
    CHECK (source IN ('zoom', 'teams'));

ALTER TABLE video_sources
    DROP CONSTRAINT IF EXISTS video_sources_transcription_source_check;

ALTER TABLE video_sources
    ADD CONSTRAINT video_sources_transcription_source_check
    CHECK (transcription_source IS NULL OR transcription_source IN ('manual', 'loom_api', 'whisper', 'zoom_api', 'teams_api'));

ALTER TABLE video_sources
    DROP CONSTRAINT IF EXISTS video_sources_provider_check;

ALTER TABLE video_sources
    ADD CONSTRAINT video_sources_provider_check
    CHECK (provider IN ('loom', 'zoom', 'other', 'teams'));

ALTER TABLE sessions
    DROP CONSTRAINT IF EXISTS sessions_source_provider_check;

ALTER TABLE sessions
    ADD CONSTRAINT sessions_source_provider_check
    CHECK (source_provider IN ('zoom', 'upload', 'teams'));
