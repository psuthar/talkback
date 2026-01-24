-- Phase 3 Update: Artifacts belong to sessions (reverse relationship)
-- This migration changes the relationship so that artifacts belong to sessions,
-- rather than sessions belonging to artifacts.

-- Step 1: Add session_id column to artifacts (nullable initially)
ALTER TABLE artifacts ADD COLUMN session_id UUID;

-- Step 2: For each existing session, create a default artifact and link it
-- OR: For each artifact that has a session, link the artifact to that session
-- Strategy: For each session, find its artifact and link that artifact to the session
UPDATE artifacts a
SET session_id = s.id
FROM sessions s
WHERE s.artifact_id = a.id;

-- Step 3: For artifacts without sessions, create a default session for each
-- and link the artifact to it
DO $$
DECLARE
    artifact_rec RECORD;
    new_session_id UUID;
BEGIN
    FOR artifact_rec IN SELECT id, title, created_at FROM artifacts WHERE session_id IS NULL
    LOOP
        new_session_id := gen_random_uuid();
        INSERT INTO sessions (id, title, created_by, status, created_at, updated_at)
        VALUES (new_session_id, 'Default Session for ' || artifact_rec.title, NULL, 'open', artifact_rec.created_at, artifact_rec.created_at);
        
        UPDATE artifacts SET session_id = new_session_id WHERE id = artifact_rec.id;
    END LOOP;
END $$;

-- Step 4: Make session_id required (NOT NULL) and add foreign key
ALTER TABLE artifacts ALTER COLUMN session_id SET NOT NULL;
ALTER TABLE artifacts ADD CONSTRAINT fk_artifacts_session_id FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE;

-- Step 5: Remove artifact_id from sessions table
ALTER TABLE sessions DROP COLUMN artifact_id;

-- Step 6: Update indexes
DROP INDEX IF EXISTS idx_sessions_artifact_id;
CREATE INDEX idx_artifacts_session_id ON artifacts(session_id);
