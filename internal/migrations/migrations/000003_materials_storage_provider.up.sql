-- Materials: support R2-backed storage (storage_provider + storage_key).
ALTER TABLE materials ADD COLUMN IF NOT EXISTS storage_provider TEXT NOT NULL DEFAULT 'local';
ALTER TABLE materials ADD COLUMN IF NOT EXISTS storage_key TEXT;
