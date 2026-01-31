-- Remove video ingestion fields from video_sources table

-- Drop indexes
DROP INDEX IF EXISTS idx_video_sources_stored_video_object_key;
DROP INDEX IF EXISTS idx_video_sources_source_type;

-- Restore original transcript_status constraint (without 'processing')
ALTER TABLE video_sources DROP CONSTRAINT IF EXISTS video_sources_transcript_status_check;
ALTER TABLE video_sources ADD CONSTRAINT video_sources_transcript_status_check 
    CHECK (transcript_status IN ('missing', 'pending', 'ready', 'failed'));

-- Drop columns
ALTER TABLE video_sources DROP COLUMN IF EXISTS failure_reason;
ALTER TABLE video_sources DROP COLUMN IF EXISTS original_url;
ALTER TABLE video_sources DROP COLUMN IF EXISTS stored_video_object_key;
ALTER TABLE video_sources DROP COLUMN IF EXISTS source_type;
