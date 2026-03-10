CREATE TABLE IF NOT EXISTS decision_stances (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id  UUID        NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    user_id     UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    stance      TEXT        NOT NULL CHECK (stance IN ('agree', 'disagree', 'conditional', 'abstain', 'need_more_info')),
    rationale   TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (session_id, user_id)
);

CREATE INDEX idx_decision_stances_session_id ON decision_stances(session_id);
CREATE INDEX idx_decision_stances_user_id    ON decision_stances(user_id);
