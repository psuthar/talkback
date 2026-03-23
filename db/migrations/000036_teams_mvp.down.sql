DROP TABLE IF EXISTS teams_connections;

ALTER TABLE video_sources
    DROP CONSTRAINT IF EXISTS video_sources_transcription_source_check;

ALTER TABLE video_sources
    ADD CONSTRAINT video_sources_transcription_source_check
  CHECK (transcription_source IS NULL OR transcription_source IN ('manual', 'loom_api', 'whisper', 'zoom_api'));

ALTER TABLE video_sources
    DROP CONSTRAINT IF EXISTS video_sources_provider_check;

ALTER TABLE video_sources
    ADD CONSTRAINT video_sources_provider_check
    CHECK (provider IN ('loom', 'zoom', 'other'));

ALTER TABLE sessions
    DROP CONSTRAINT IF EXISTS sessions_source_provider_check;

ALTER TABLE sessions
    ADD CONSTRAINT sessions_source_provider_check
    CHECK (source_provider IN ('zoom', 'upload'));
