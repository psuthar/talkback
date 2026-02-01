-- Revert to original constraint (no zoom_api)
ALTER TABLE video_sources DROP CONSTRAINT IF EXISTS video_sources_transcription_source_check;
ALTER TABLE video_sources ADD CONSTRAINT video_sources_transcription_source_check
  CHECK (transcription_source IS NULL OR transcription_source IN ('manual', 'loom_api', 'whisper'));
