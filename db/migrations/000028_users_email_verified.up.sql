-- Add email_verified_at for invite-based signup (redemption counts as verification).
ALTER TABLE users ADD COLUMN IF NOT EXISTS email_verified_at timestamptz NULL;
