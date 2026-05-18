-- SCRUM-471 rollback.
ALTER TABLE session_processing_jobs DROP COLUMN IF EXISTS set_as_primary;
