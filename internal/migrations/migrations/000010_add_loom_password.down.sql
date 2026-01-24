-- Migration: Remove password field from transcript_jobs

ALTER TABLE transcript_jobs
DROP COLUMN IF EXISTS loom_password;
