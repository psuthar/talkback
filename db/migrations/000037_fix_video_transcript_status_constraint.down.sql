-- Reverts to constraint without 'processing' (may break transcription workers). Prefer not to run Down in production.
ALTER TABLE video_sources DROP CONSTRAINT IF EXISTS video_sources_transcript_status_check;
ALTER TABLE video_sources ADD CONSTRAINT video_sources_transcript_status_check
    CHECK (transcript_status IN ('missing', 'pending', 'ready', 'failed'));
