-- Add creator and participant roles; migrate existing 'user' to 'creator'.
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_global_role_check;
UPDATE users SET global_role = 'creator' WHERE global_role = 'user';
ALTER TABLE users ADD CONSTRAINT users_global_role_check CHECK (global_role IN ('admin', 'creator', 'participant'));
ALTER TABLE users ALTER COLUMN global_role SET DEFAULT 'creator';
