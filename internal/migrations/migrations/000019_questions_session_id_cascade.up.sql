-- Fix DeleteSession: questions.session_id was added with ON DELETE SET NULL,
-- which violates NOT NULL when session is deleted. Use ON DELETE CASCADE so questions
-- (and answers via their FK to questions) are deleted with the session.

ALTER TABLE questions DROP CONSTRAINT IF EXISTS questions_session_id_fkey;
ALTER TABLE questions DROP CONSTRAINT IF EXISTS fk_questions_session_id;

ALTER TABLE questions ADD CONSTRAINT fk_questions_session_id
  FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE;
