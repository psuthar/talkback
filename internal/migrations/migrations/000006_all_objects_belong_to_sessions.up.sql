-- Phase 3 Update: All objects belong to sessions
-- This migration adds session_id to materials, video_sources, and makes questions.session_id required

-- Step 1: Add session_id to materials table
ALTER TABLE materials ADD COLUMN session_id UUID;

-- Step 2: Populate session_id for existing materials from their artifacts
UPDATE materials m
SET session_id = a.session_id
FROM artifacts a
WHERE m.artifact_id = a.id;

-- Step 3: Make session_id required for materials and add foreign key
ALTER TABLE materials ALTER COLUMN session_id SET NOT NULL;
ALTER TABLE materials ADD CONSTRAINT fk_materials_session_id FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE;

-- Step 4: Add session_id to video_sources table
ALTER TABLE video_sources ADD COLUMN session_id UUID;

-- Step 5: Populate session_id for existing video_sources from their artifacts
UPDATE video_sources v
SET session_id = a.session_id
FROM artifacts a
WHERE v.artifact_id = a.id;

-- Step 6: Make session_id required for video_sources and add foreign key
ALTER TABLE video_sources ALTER COLUMN session_id SET NOT NULL;
ALTER TABLE video_sources ADD CONSTRAINT fk_video_sources_session_id FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE;

-- Step 7: Make questions.session_id required (populate from artifact if null)
UPDATE questions q
SET session_id = a.session_id
FROM artifacts a
WHERE q.artifact_id = a.id AND q.session_id IS NULL;

-- Step 8: Make session_id required for questions
ALTER TABLE questions ALTER COLUMN session_id SET NOT NULL;
ALTER TABLE questions ADD CONSTRAINT fk_questions_session_id FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE;

-- Step 9: Create indexes for session_id columns
CREATE INDEX idx_materials_session_id ON materials(session_id);
CREATE INDEX idx_video_sources_session_id ON video_sources(session_id);
CREATE INDEX idx_questions_session_id ON questions(session_id);
