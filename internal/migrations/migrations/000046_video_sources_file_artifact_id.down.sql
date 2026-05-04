DROP INDEX IF EXISTS idx_video_sources_file_artifact_id;

ALTER TABLE video_sources
    DROP COLUMN IF EXISTS file_artifact_id;
