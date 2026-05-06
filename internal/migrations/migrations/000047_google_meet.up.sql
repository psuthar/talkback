-- Google Meet MVP: widen session/video/job constraints; google_meet_connections table.

ALTER TABLE sessions
    DROP CONSTRAINT IF EXISTS sessions_source_provider_check;

ALTER TABLE sessions
    ADD CONSTRAINT sessions_source_provider_check
    CHECK (source_provider IN ('zoom', 'upload', 'teams', 'google_meet'));

ALTER TABLE video_sources
    DROP CONSTRAINT IF EXISTS video_sources_provider_check;

ALTER TABLE video_sources
    ADD CONSTRAINT video_sources_provider_check
    CHECK (provider IN ('loom', 'zoom', 'other', 'teams', 'google_meet'));

ALTER TABLE video_sources
    DROP CONSTRAINT IF EXISTS video_sources_transcription_source_check;

ALTER TABLE video_sources
    ADD CONSTRAINT video_sources_transcription_source_check
    CHECK (transcription_source IS NULL OR transcription_source IN ('manual', 'loom_api', 'whisper', 'zoom_api', 'teams_api', 'google_meet_api'));

ALTER TABLE session_processing_jobs
    DROP CONSTRAINT IF EXISTS session_processing_jobs_source_check;

ALTER TABLE session_processing_jobs
    ADD CONSTRAINT session_processing_jobs_source_check
    CHECK (source IN ('zoom', 'teams', 'google_meet'));

CREATE TABLE IF NOT EXISTS google_meet_connections (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    creator_identity_id TEXT NOT NULL UNIQUE,
    google_user_id TEXT NOT NULL,
    google_user_email TEXT NULL,
    granted_scope TEXT NULL,
    workspace_eligible BOOLEAN NULL,
    access_token_encrypted BYTEA NOT NULL,
    refresh_token_encrypted BYTEA NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_google_meet_connections_creator_identity ON google_meet_connections(creator_identity_id);
