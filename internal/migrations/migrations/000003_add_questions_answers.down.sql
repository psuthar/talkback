-- Phase 2: Remove questions and answers tables

DROP INDEX IF EXISTS idx_answers_created_at;
DROP INDEX IF EXISTS idx_answers_question_id;
DROP INDEX IF EXISTS idx_questions_created_at;
DROP INDEX IF EXISTS idx_questions_artifact_id;

DROP TABLE IF EXISTS answers;
DROP TABLE IF EXISTS questions;
