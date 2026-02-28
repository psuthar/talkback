-- Tombstone: soft-delete materials so we keep "file used to exist" record; file is removed from R2/local.
ALTER TABLE materials ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ NULL;
