DROP INDEX IF EXISTS idx_transcript_jobs_material_id;
ALTER TABLE transcript_jobs DROP COLUMN IF EXISTS material_id;
ALTER TABLE transcript_jobs ALTER COLUMN video_source_id SET NOT NULL;
