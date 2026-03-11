-- answered_by: email (or name) of the person who provided the answer when model = 'manual'
ALTER TABLE answers ADD COLUMN IF NOT EXISTS answered_by TEXT NULL;
