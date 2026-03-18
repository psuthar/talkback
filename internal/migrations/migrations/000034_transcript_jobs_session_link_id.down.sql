DROP INDEX IF EXISTS idx_transcript_jobs_session_link_id;
ALTER TABLE transcript_jobs DROP COLUMN IF EXISTS session_link_id;

ALTER TABLE session_links DROP CONSTRAINT IF EXISTS session_links_status_check;
ALTER TABLE session_links ADD CONSTRAINT session_links_status_check
  CHECK (status IN ('pending', 'verified', 'failed'));

