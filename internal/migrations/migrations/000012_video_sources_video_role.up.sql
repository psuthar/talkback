-- Add video_role to video_sources: 'primary' | 'secondary' (nullable for backward compatibility).
-- At most one primary per session (enforced by partial unique index).
ALTER TABLE video_sources ADD COLUMN IF NOT EXISTS video_role TEXT NULL CHECK (video_role IN ('primary', 'secondary'));

CREATE UNIQUE INDEX IF NOT EXISTS idx_video_sources_one_primary_per_session
ON video_sources (session_id) WHERE video_role = 'primary';
