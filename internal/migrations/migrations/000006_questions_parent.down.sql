DROP INDEX IF EXISTS idx_questions_parent_question_id;
ALTER TABLE questions DROP COLUMN IF EXISTS parent_question_id;
