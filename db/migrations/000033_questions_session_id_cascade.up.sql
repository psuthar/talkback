-- Fix DeleteSession: questions.session_id was added in 000004 with ON DELETE SET NULL,
-- which violates NOT NULL when session is deleted. Use ON DELETE CASCADE so questions
-- (and answers via their FK to questions) are deleted with the session.

-- Drop whichever constraint(s) exist (PostgreSQL names ADD COLUMN REFERENCES as questions_session_id_fkey; 000006 added fk_questions_session_id)
ALTER TABLE questions DROP CONSTRAINT IF EXISTS questions_session_id_fkey;
ALTER TABLE questions DROP CONSTRAINT IF EXISTS fk_questions_session_id;

-- Add single FK with CASCADE so deleting a session deletes its questions (answers cascade from questions)
ALTER TABLE questions ADD CONSTRAINT fk_questions_session_id
  FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE;
