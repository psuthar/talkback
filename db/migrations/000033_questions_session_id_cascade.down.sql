-- Revert: drop the CASCADE constraint. Do not re-add ON DELETE SET NULL (incompatible with NOT NULL column).
ALTER TABLE questions DROP CONSTRAINT IF EXISTS fk_questions_session_id;
