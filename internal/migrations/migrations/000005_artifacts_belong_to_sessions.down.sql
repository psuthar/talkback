-- Revert: Restore original relationship (sessions belong to artifacts)

-- Step 1: Add artifact_id back to sessions
ALTER TABLE sessions ADD COLUMN artifact_id UUID;

-- Step 2: Populate artifact_id from artifacts (take first artifact for each session)
UPDATE sessions s
SET artifact_id = (
    SELECT a.id 
    FROM artifacts a 
    WHERE a.session_id = s.id 
    LIMIT 1
);

-- Step 3: Make artifact_id required
ALTER TABLE sessions ALTER COLUMN artifact_id SET NOT NULL;
ALTER TABLE sessions ADD CONSTRAINT fk_sessions_artifact_id FOREIGN KEY (artifact_id) REFERENCES artifacts(id) ON DELETE CASCADE;

-- Step 4: Remove session_id from artifacts
ALTER TABLE artifacts DROP CONSTRAINT IF EXISTS fk_artifacts_session_id;
ALTER TABLE artifacts DROP COLUMN session_id;

-- Step 5: Recreate original index
DROP INDEX IF EXISTS idx_artifacts_session_id;
CREATE INDEX idx_sessions_artifact_id ON sessions(artifact_id);
