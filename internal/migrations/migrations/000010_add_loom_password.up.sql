-- Migration: Add password field to transcript_jobs for password-protected Loom videos

ALTER TABLE transcript_jobs
ADD COLUMN IF NOT EXISTS loom_password TEXT NULL;

-- Note: Password is stored as plain text but only used during resolution
-- Security: Passwords are not logged and kept in-memory only during job processing
