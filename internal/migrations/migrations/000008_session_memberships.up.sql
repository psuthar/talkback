CREATE TABLE session_memberships (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  session_id uuid NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role text NOT NULL,
  invited_by_user_id uuid NULL REFERENCES users(id) ON DELETE SET NULL,
  joined_at timestamptz NOT NULL DEFAULT now(),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(session_id, user_id),
  CONSTRAINT session_memberships_role_check CHECK (role IN ('participant', 'creator'))
);
CREATE INDEX session_memberships_session_id_idx ON session_memberships (session_id);
CREATE INDEX session_memberships_user_id_idx ON session_memberships (user_id);
CREATE OR REPLACE FUNCTION update_session_memberships_updated_at()
RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trigger_update_session_memberships_updated_at
  BEFORE UPDATE ON session_memberships
  FOR EACH ROW
  EXECUTE FUNCTION update_session_memberships_updated_at();
