-- Threaded questions: follow-up questions attach to a parent (mini-dialogue per topic).
ALTER TABLE questions ADD COLUMN parent_question_id UUID NULL REFERENCES questions(id) ON DELETE CASCADE;
CREATE INDEX idx_questions_parent_question_id ON questions(parent_question_id);
