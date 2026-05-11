-- SCRUM-379: drop Loom video support.
-- 1. Backfill any existing rows that still carry Loom-specific enum values so
--    the tightened CHECK constraints below do not fail in environments where
--    Loom data was created before this migration.
-- 2. Tighten the video_sources.provider CHECK constraint to no longer allow
--    'loom'.
-- 3. Tighten the video_sources.transcription_source CHECK constraint to no
--    longer allow 'loom_api'.
-- 4. Drop the transcript_jobs.loom_password column, which is no longer
--    referenced by application code.

UPDATE video_sources
SET provider = 'other'
WHERE provider = 'loom';

UPDATE video_sources
SET transcription_source = 'manual'
WHERE transcription_source = 'loom_api';

ALTER TABLE video_sources DROP CONSTRAINT IF EXISTS video_sources_provider_check;
ALTER TABLE video_sources ADD CONSTRAINT video_sources_provider_check
  CHECK (provider IN ('zoom', 'teams', 'google_meet', 'other'));

ALTER TABLE video_sources DROP CONSTRAINT IF EXISTS video_sources_transcription_source_check;
ALTER TABLE video_sources ADD CONSTRAINT video_sources_transcription_source_check
  CHECK (transcription_source IS NULL OR transcription_source IN ('manual', 'whisper', 'zoom_api'));

ALTER TABLE transcript_jobs DROP COLUMN IF EXISTS loom_password;
