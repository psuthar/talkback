-- Add video ingestion fields to video_sources table
-- This supports MP4 upload, direct URL ingestion, and better error tracking

-- Add source_type column (upload, direct_url, embed_url)
ALTER TABLE video_sources ADD COLUMN source_type TEXT NOT NULL DEFAULT 'embed_url' CHECK (source_type IN ('upload', 'direct_url', 'embed_url'));

-- Add stored_video_object_key column (path to uploaded/downloaded MP4 file)
ALTER TABLE video_sources ADD COLUMN stored_video_object_key TEXT NULL;

-- Add original_url column (original user-provided URL for reference)
ALTER TABLE video_sources ADD COLUMN original_url TEXT NULL;

-- Add failure_reason column (error message when ingestion/transcription fails)
ALTER TABLE video_sources ADD COLUMN failure_reason TEXT NULL;

-- Update transcript_status constraint to include 'processing' state
ALTER TABLE video_sources DROP CONSTRAINT IF EXISTS video_sources_transcript_status_check;
ALTER TABLE video_sources ADD CONSTRAINT video_sources_transcript_status_check 
    CHECK (transcript_status IN ('missing', 'pending', 'processing', 'ready', 'failed'));

-- Add indexes for efficient querying
CREATE INDEX idx_video_sources_source_type ON video_sources(source_type);
CREATE INDEX idx_video_sources_stored_video_object_key ON video_sources(stored_video_object_key) WHERE stored_video_object_key IS NOT NULL;
