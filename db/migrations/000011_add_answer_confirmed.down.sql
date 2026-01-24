-- Remove confirmed column from answers table
DROP INDEX IF EXISTS idx_answers_confirmed;
ALTER TABLE answers DROP COLUMN IF EXISTS confirmed;
