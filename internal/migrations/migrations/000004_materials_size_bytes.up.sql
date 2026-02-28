-- Store file size (bytes) for materials; nullable for existing/pasted content.
ALTER TABLE materials ADD COLUMN IF NOT EXISTS size_bytes BIGINT NULL;
