-- Rollback migration: Remove playback modes and video source enhancements

-- Remove video_time_seconds from questions
DROP INDEX IF EXISTS idx_questions_video_time_seconds;
ALTER TABLE questions
DROP COLUMN IF EXISTS video_time_seconds;

-- Remove new columns from video_sources
ALTER TABLE video_sources
DROP COLUMN IF EXISTS poster_url,
DROP COLUMN IF EXISTS duration_seconds,
DROP COLUMN IF EXISTS media_url,
DROP COLUMN IF EXISTS embed_url,
DROP COLUMN IF EXISTS playback_mode;
