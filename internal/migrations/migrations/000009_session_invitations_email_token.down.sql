DROP TRIGGER IF EXISTS trigger_update_session_invitations_updated_at ON session_invitations;
DROP FUNCTION IF EXISTS update_session_invitations_updated_at();
DROP TABLE IF EXISTS session_invitations;
CREATE TABLE session_invitations (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  session_id uuid NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  invited_by_user_id uuid NULL REFERENCES users(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(session_id, user_id)
);
CREATE INDEX session_invitations_session_id_idx ON session_invitations (session_id);
CREATE INDEX session_invitations_user_id_idx ON session_invitations (user_id);
