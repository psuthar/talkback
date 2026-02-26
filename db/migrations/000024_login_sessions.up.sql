CREATE TABLE login_sessions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL,
  revoked_at timestamptz NULL,
  ip text NULL,
  user_agent text NULL
);

CREATE INDEX login_sessions_user_id_idx ON login_sessions (user_id);
CREATE INDEX login_sessions_expires_at_idx ON login_sessions (expires_at);
CREATE INDEX login_sessions_active_idx ON login_sessions (id) WHERE revoked_at IS NULL;
