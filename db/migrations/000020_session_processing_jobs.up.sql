-- Mission #4: Unified pipeline state machine for ingestion + indexing
-- session_processing_jobs: one job per session/source (Zoom import → transcript → chunk → embed)

CREATE TABLE IF NOT EXISTS session_processing_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    source TEXT NOT NULL DEFAULT 'zoom' CHECK (source IN ('zoom')),
    state TEXT NOT NULL DEFAULT 'queued' CHECK (state IN (
        'queued', 'fetching', 'downloading', 'parsing', 'chunking', 'embedding',
        'waiting', 'ready', 'failed_transient', 'failed_permanent', 'canceled'
    )),
    stage TEXT NOT NULL DEFAULT 'fetch' CHECK (stage IN ('fetch', 'download', 'parse', 'chunk', 'embed', 'ready')),
    attempt_count INT NOT NULL DEFAULT 0,
    next_retry_at TIMESTAMPTZ NULL,
    last_error_code TEXT NULL,
    last_error_message TEXT NULL,
    locked_at TIMESTAMPTZ NULL,
    lock_owner TEXT NULL,
    meeting_uuid TEXT NULL,
    instance_uuid TEXT NULL,
    creator_identity TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_session_processing_jobs_session_source
    ON session_processing_jobs(session_id, source);
CREATE INDEX IF NOT EXISTS idx_session_processing_jobs_state_retry
    ON session_processing_jobs(state, next_retry_at);
CREATE INDEX IF NOT EXISTS idx_session_processing_jobs_session_id
    ON session_processing_jobs(session_id);

-- Mirror latest job state on sessions for quick UI
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS processing_state TEXT NULL;
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS processing_updated_at TIMESTAMPTZ NULL;
