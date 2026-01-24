-- Add confirmed column to answers table
ALTER TABLE answers ADD COLUMN confirmed BOOLEAN NOT NULL DEFAULT false;

-- Create index for confirmed answers
CREATE INDEX idx_answers_confirmed ON answers(confirmed) WHERE confirmed = true;
