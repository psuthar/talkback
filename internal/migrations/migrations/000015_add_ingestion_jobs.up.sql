-- ingestion_jobs: track Zoom (and future) import jobs per session
CREATE TABLE IF NOT EXISTS ingestion_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    source TEXT NOT NULL DEFAULT 'zoom' CHECK (source IN ('zoom')),
    state TEXT NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'fetching', 'ready', 'failed')),
    meeting_uuid TEXT NULL,
    instance_uuid TEXT NULL,
    last_error TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_ingestion_jobs_session_id ON ingestion_jobs(session_id);
