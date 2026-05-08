-- SCRUM-357: import_jobs table for asynchronous template-driven session import.
-- Also extends sessions.source_provider with the 'template' value.

CREATE TABLE IF NOT EXISTS import_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID REFERENCES sessions(id) ON DELETE SET NULL,
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    state TEXT NOT NULL DEFAULT 'queued'
        CHECK (state IN ('queued', 'running', 'succeeded', 'partial', 'failed')),
    template_descriptor JSONB NOT NULL,
    elements_state JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_import_jobs_user_id ON import_jobs(user_id);
CREATE INDEX IF NOT EXISTS idx_import_jobs_session_id ON import_jobs(session_id);
CREATE INDEX IF NOT EXISTS idx_import_jobs_state ON import_jobs(state);

ALTER TABLE sessions DROP CONSTRAINT IF EXISTS sessions_source_provider_check;
ALTER TABLE sessions ADD CONSTRAINT sessions_source_provider_check
    CHECK (source_provider IN ('zoom', 'upload', 'teams', 'google_meet', 'template'));
