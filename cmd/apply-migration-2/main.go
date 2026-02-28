// apply-migration-2 runs the file_artifacts_r2 migration (000002) against the DB.
// Use when Render DB is missing primary_video_artifact_id (migration 2 never ran).
// Usage: $env:MIGRATION_FIX_DATABASE_URL = "postgresql://..."; go run ./cmd/apply-migration-2
package main

import (
	"database/sql"
	"log"
	"os"

	_ "github.com/lib/pq"
)

func main() {
	dsn := os.Getenv("MIGRATION_FIX_DATABASE_URL")
	if dsn == "" {
		log.Fatal("MIGRATION_FIX_DATABASE_URL is not set. Set it to the Render external DB URL for this run.")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	// Run migration 2: file_artifacts table + sessions.primary_video_artifact_id
	// Use IF NOT EXISTS / DO blocks to be idempotent.
	_, err = db.Exec(`
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ language 'plpgsql';
`)
	if err != nil {
		log.Fatalf("create function: %v", err)
	}

	_, err = db.Exec(`
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
`)
	if err != nil {
		log.Fatalf("create file_artifacts: %v", err)
	}

	for _, idx := range []string{
		"CREATE INDEX IF NOT EXISTS idx_file_artifacts_session_id ON file_artifacts(session_id)",
		"CREATE INDEX IF NOT EXISTS idx_file_artifacts_owner_user_id ON file_artifacts(owner_user_id)",
		"CREATE INDEX IF NOT EXISTS idx_file_artifacts_storage_key ON file_artifacts(storage_key)",
		"CREATE INDEX IF NOT EXISTS idx_file_artifacts_status ON file_artifacts(status)",
	} {
		if _, err := db.Exec(idx); err != nil {
			log.Fatalf("create index: %v", err)
		}
	}

	// Trigger: drop if exists then create (no IF NOT EXISTS for trigger)
	_, _ = db.Exec("DROP TRIGGER IF EXISTS update_file_artifacts_updated_at ON file_artifacts")
	_, err = db.Exec(`
CREATE TRIGGER update_file_artifacts_updated_at BEFORE UPDATE ON file_artifacts
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
`)
	if err != nil {
		log.Fatalf("create trigger: %v", err)
	}

	// Add column to sessions if missing (Postgres has no ADD COLUMN IF NOT EXISTS in older versions)
	_, err = db.Exec(`
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public' AND table_name = 'sessions' AND column_name = 'primary_video_artifact_id'
    ) THEN
        ALTER TABLE sessions ADD COLUMN primary_video_artifact_id UUID NULL REFERENCES file_artifacts(id) ON DELETE SET NULL;
    END IF;
END $$;
`)
	if err != nil {
		log.Fatalf("add primary_video_artifact_id: %v", err)
	}

	_, err = db.Exec("CREATE INDEX IF NOT EXISTS idx_sessions_primary_video_artifact_id ON sessions(primary_video_artifact_id)")
	if err != nil {
		log.Fatalf("create index on sessions: %v", err)
	}

	log.Println("Done. Migration 2 (file_artifacts_r2) applied. List sessions should work now.")
}
