-- Revert to admin/user only. Map creator -> user; participant -> user (lossy).
ALTER TABLE users ALTER COLUMN global_role SET DEFAULT 'user';
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_global_role_check;
UPDATE users SET global_role = 'user' WHERE global_role IN ('creator', 'participant');
ALTER TABLE users ADD CONSTRAINT users_global_role_check CHECK (global_role IN ('admin', 'user'));
