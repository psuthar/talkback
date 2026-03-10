ALTER TABLE sessions
    DROP COLUMN IF EXISTS premise,
    DROP COLUMN IF EXISTS primary_decision;
