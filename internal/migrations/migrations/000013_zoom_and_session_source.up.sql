-- Zoom-centric: Session source, zoom_connections, video_sources VTT fields

-- Session: source_provider ('zoom' | 'upload'), source_reference_url (Zoom recording link)
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS source_provider TEXT DEFAULT 'upload' CHECK (source_provider IN ('zoom', 'upload'));
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS source_reference_url TEXT NULL;

-- Backfill existing sessions
UPDATE sessions SET source_provider = 'upload' WHERE source_provider IS NULL;

-- zoom_connections: OAuth tokens keyed by creator identity
CREATE TABLE IF NOT EXISTS zoom_connections (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    creator_identity_id TEXT NOT NULL UNIQUE,
    zoom_user_id TEXT NOT NULL,
    zoom_account_id TEXT NULL,
    zoom_user_email TEXT NULL,
    access_token_encrypted BYTEA NOT NULL,
    refresh_token_encrypted BYTEA NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_zoom_connections_creator_identity ON zoom_connections(creator_identity_id);

-- video_sources: optional raw VTT and normalized segments for Zoom transcripts
ALTER TABLE video_sources ADD COLUMN IF NOT EXISTS raw_vtt TEXT NULL;
ALTER TABLE video_sources ADD COLUMN IF NOT EXISTS transcript_segments JSONB NULL;
