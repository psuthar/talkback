ALTER TABLE materials DROP CONSTRAINT IF EXISTS materials_text_status_check;
ALTER TABLE materials ADD CONSTRAINT materials_text_status_check
    CHECK (text_status IN ('pending', 'ready', 'failed'));
ALTER TABLE materials DROP COLUMN IF EXISTS error_message;
ALTER TABLE materials DROP COLUMN IF EXISTS title;
