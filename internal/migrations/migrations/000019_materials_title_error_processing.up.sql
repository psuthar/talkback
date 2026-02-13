-- Mission #6: materials title, error_message, and processing status
ALTER TABLE materials ADD COLUMN IF NOT EXISTS title TEXT NULL;
ALTER TABLE materials ADD COLUMN IF NOT EXISTS error_message TEXT NULL;

-- Allow text_status to include 'processing' (uploaded -> pending, processing -> processing, ready/failed unchanged)
ALTER TABLE materials DROP CONSTRAINT IF EXISTS materials_text_status_check;
ALTER TABLE materials ADD CONSTRAINT materials_text_status_check
    CHECK (text_status IN ('pending', 'processing', 'ready', 'failed'));

-- Backfill title from filename where title is null
UPDATE materials SET title = filename WHERE title IS NULL;
