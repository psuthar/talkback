-- SCRUM-379: restore Loom-specific schema bits.
-- This is a best-effort reversal: row data backfilled by the up migration is
-- NOT restored (we replace 'loom' provider rows with 'other' on the way up, and
-- there is no way to know which were originally Loom).

ALTER TABLE transcript_jobs ADD COLUMN IF NOT EXISTS loom_password TEXT NULL;

ALTER TABLE video_sources DROP CONSTRAINT IF EXISTS video_sources_transcription_source_check;
ALTER TABLE video_sources ADD CONSTRAINT video_sources_transcription_source_check
  CHECK (transcription_source IS NULL OR transcription_source IN ('manual', 'loom_api', 'whisper', 'zoom_api'));

ALTER TABLE video_sources DROP CONSTRAINT IF EXISTS video_sources_provider_check;
ALTER TABLE video_sources ADD CONSTRAINT video_sources_provider_check
  CHECK (provider IN ('loom', 'zoom', 'teams', 'google_meet', 'other'));
