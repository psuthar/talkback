-- Rollback: Remove transcript jobs table and related columns

-- Drop foreign key constraint from video_sources
ALTER TABLE video_sources
DROP COLUMN IF EXISTS transcription_job_id,
DROP COLUMN IF EXISTS transcription_source,
DROP COLUMN IF EXISTS auto_transcribe_enabled;

-- Drop transcript_jobs table
DROP TABLE IF EXISTS transcript_jobs CASCADE;
