-- Allow transcript jobs for link extraction (async fetch+extract) without video or material
ALTER TABLE transcript_jobs
  ADD COLUMN IF NOT EXISTS session_link_id UUID NULL REFERENCES session_links(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS idx_transcript_jobs_session_link_id ON transcript_jobs(session_link_id);

-- Allow session_links to show 'processing' while job is running (like materials)
ALTER TABLE session_links DROP CONSTRAINT IF EXISTS session_links_status_check;
ALTER TABLE session_links ADD CONSTRAINT session_links_status_check
  CHECK (status IN ('pending', 'processing', 'verified', 'failed'));
