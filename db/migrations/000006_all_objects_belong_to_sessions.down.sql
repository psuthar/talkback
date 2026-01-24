-- Revert: Remove session_id from materials, video_sources, and make questions.session_id nullable

-- Step 1: Drop indexes
DROP INDEX IF EXISTS idx_questions_session_id;
DROP INDEX IF EXISTS idx_video_sources_session_id;
DROP INDEX IF EXISTS idx_materials_session_id;

-- Step 2: Make questions.session_id nullable and remove constraint
ALTER TABLE questions DROP CONSTRAINT IF EXISTS fk_questions_session_id;
ALTER TABLE questions ALTER COLUMN session_id DROP NOT NULL;

-- Step 3: Remove session_id from video_sources
ALTER TABLE video_sources DROP CONSTRAINT IF EXISTS fk_video_sources_session_id;
ALTER TABLE video_sources DROP COLUMN session_id;

-- Step 4: Remove session_id from materials
ALTER TABLE materials DROP CONSTRAINT IF EXISTS fk_materials_session_id;
ALTER TABLE materials DROP COLUMN session_id;
