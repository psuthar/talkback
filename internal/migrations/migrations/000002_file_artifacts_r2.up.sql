-- File artifacts: R2-backed binary files (video, documents, images, exports).
-- Distinct from existing artifacts table (session container with title/description).

-- Ensure trigger function exists (may be missing if DB was created without 000001).
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TABLE IF NOT EXISTS file_artifacts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NULL REFERENCES sessions(id) ON DELETE SET NULL,
    owner_user_id UUID NULL REFERENCES users(id) ON DELETE SET NULL,
    kind TEXT NOT NULL CHECK (kind IN ('video', 'document', 'image', 'attachment', 'export', 'transcript_raw')),
    filename TEXT NULL,
    content_type TEXT NOT NULL,
    size_bytes BIGINT NULL,
    sha256 TEXT NULL,
    storage_provider TEXT NOT NULL DEFAULT 'r2',
    storage_bucket TEXT NOT NULL,
    storage_key TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'ready', 'failed')),
    failure_reason TEXT NULL,
    metadata_json JSONB NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_file_artifacts_session_id ON file_artifacts(session_id);
CREATE INDEX IF NOT EXISTS idx_file_artifacts_owner_user_id ON file_artifacts(owner_user_id);
CREATE INDEX IF NOT EXISTS idx_file_artifacts_storage_key ON file_artifacts(storage_key);
CREATE INDEX IF NOT EXISTS idx_file_artifacts_status ON file_artifacts(status);

DROP TRIGGER IF EXISTS update_file_artifacts_updated_at ON file_artifacts;
CREATE TRIGGER update_file_artifacts_updated_at BEFORE UPDATE ON file_artifacts
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Sessions: link primary video to a file_artifact (R2) instead of streaming from Zoom.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'sessions' AND column_name = 'primary_video_artifact_id'
    ) THEN
        ALTER TABLE sessions ADD COLUMN primary_video_artifact_id UUID NULL REFERENCES file_artifacts(id) ON DELETE SET NULL;
    END IF;
END $$;
CREATE INDEX IF NOT EXISTS idx_sessions_primary_video_artifact_id ON sessions(primary_video_artifact_id);
