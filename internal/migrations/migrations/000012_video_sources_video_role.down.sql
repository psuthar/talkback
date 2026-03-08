DROP INDEX IF EXISTS idx_video_sources_one_primary_per_session;
ALTER TABLE video_sources DROP COLUMN IF EXISTS video_role;
