-- Replace legacy session_invitations (user_id-based) with email + token-based invitations.
DROP TABLE IF EXISTS session_invitations;

CREATE TABLE session_invitations (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  session_id uuid NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  invited_email_normalized text NOT NULL,
  inviter_user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  invited_role text NOT NULL,
  token_hash text NOT NULL,
  status text NOT NULL,
  expires_at timestamptz NOT NULL,
  accepted_at timestamptz NULL,
  accepted_by_user_id uuid NULL REFERENCES users(id) ON DELETE SET NULL,
  email_verified_at timestamptz NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT session_invitations_invited_role_check CHECK (invited_role IN ('participant', 'creator')),
  CONSTRAINT session_invitations_status_check CHECK (status IN ('pending', 'accepted', 'revoked', 'expired'))
);

CREATE INDEX session_invitations_session_id_idx ON session_invitations (session_id);
CREATE INDEX session_invitations_invited_email_normalized_idx ON session_invitations (invited_email_normalized);
CREATE UNIQUE INDEX session_invitations_token_hash_idx ON session_invitations (token_hash);

CREATE OR REPLACE FUNCTION update_session_invitations_updated_at()
RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_update_session_invitations_updated_at
  BEFORE UPDATE ON session_invitations
  FOR EACH ROW
  EXECUTE FUNCTION update_session_invitations_updated_at();
