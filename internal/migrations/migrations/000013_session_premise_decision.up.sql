ALTER TABLE sessions
    ADD COLUMN IF NOT EXISTS premise          TEXT,
    ADD COLUMN IF NOT EXISTS primary_decision TEXT;
