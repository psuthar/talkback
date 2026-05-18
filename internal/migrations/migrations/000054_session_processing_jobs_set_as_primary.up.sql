-- SCRUM-471: gate auto-primary promotion in pipeline workers on a
-- per-job flag. CreateSessionFromZoom (and any future
-- "session-born-with-video" entry point) inserts with the default
-- (TRUE) so the worker continues to promote the originating recording
-- to primary. Post-creation import handlers
-- (POST /api/sessions/:id/import/*) override to FALSE so subsequent
-- imports land as secondary; the user explicitly promotes via the
-- SCRUM-426 RecordingsSection kebab.
--
-- Default TRUE preserves back-compat for existing/in-flight jobs.

ALTER TABLE session_processing_jobs
    ADD COLUMN IF NOT EXISTS set_as_primary BOOLEAN NOT NULL DEFAULT TRUE;
