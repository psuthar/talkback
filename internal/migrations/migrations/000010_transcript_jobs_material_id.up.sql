-- Allow transcript jobs for materials (e.g. MP4 uploads) without a video source
ALTER TABLE transcript_jobs
  ALTER COLUMN video_source_id DROP NOT NULL;

-- Link job to material when transcribing an uploaded material file
ALTER TABLE transcript_jobs
  ADD COLUMN IF NOT EXISTS material_id UUID NULL REFERENCES materials(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS idx_transcript_jobs_material_id ON transcript_jobs(material_id);
