-- Ensure transcript_status allows 'processing' (required by job worker / UpdateVideoSourceTranscriptStatus).
-- Some databases may still have the older CHECK from 000002 if 000012 did not complete successfully.
ALTER TABLE video_sources DROP CONSTRAINT IF EXISTS video_sources_transcript_status_check;
ALTER TABLE video_sources ADD CONSTRAINT video_sources_transcript_status_check
    CHECK (transcript_status IN ('missing', 'pending', 'processing', 'ready', 'failed'));
