-- Teams MVP: widen session/video constraints; teams_connections table.

ALTER TABLE sessions
    DROP CONSTRAINT IF EXISTS sessions_source_provider_check;

ALTER TABLE sessions
    ADD CONSTRAINT sessions_source_provider_check
    CHECK (source_provider IN ('zoom', 'upload', 'teams'));

ALTER TABLE video_sources
    DROP CONSTRAINT IF EXISTS video_sources_provider_check;

ALTER TABLE video_sources
    ADD CONSTRAINT video_sources_provider_check
    CHECK (provider IN ('loom', 'zoom', 'other', 'teams'));

ALTER TABLE video_sources
    DROP CONSTRAINT IF EXISTS video_sources_transcription_source_check;

ALTER TABLE video_sources
    ADD CONSTRAINT video_sources_transcription_source_check
  CHECK (transcription_source IS NULL OR transcription_source IN ('manual', 'loom_api', 'whisper', 'zoom_api', 'teams_api'));

CREATE TABLE IF NOT EXISTS teams_connections (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    creator_identity_id TEXT NOT NULL UNIQUE,
    tenant_id TEXT NOT NULL,
    teams_user_id TEXT NOT NULL,
    teams_user_email TEXT NULL,
    access_token_encrypted BYTEA NOT NULL,
    refresh_token_encrypted BYTEA NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_teams_connections_creator_identity ON teams_connections(creator_identity_id);
