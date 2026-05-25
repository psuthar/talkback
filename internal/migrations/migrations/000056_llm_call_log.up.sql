-- SCRUM-568 (Slice 5 of SCRUM-560): llm_call_log table for guardrail telemetry.
-- Schema per docs/guardrails/log-shape.md. Backward-compatible — no NOT NULL
-- columns without defaults.

CREATE TABLE IF NOT EXISTS llm_call_log (
    id                   uuid        PRIMARY KEY,
    ts                   timestamptz NOT NULL DEFAULT now(),
    site                 text        NOT NULL,
    model                text        NOT NULL,
    user_id              uuid        NULL,
    session_id           uuid        NULL,
    prompt_hash          text        NOT NULL,
    input_tokens         integer     NULL,
    output_tokens        integer     NULL,
    latency_ms           integer     NOT NULL,
    guardrails_fired     text[]      NOT NULL DEFAULT '{}',
    decision             text        NOT NULL,
    refusal_code         text        NULL,
    refusal_user_message text        NULL
);

CREATE INDEX IF NOT EXISTS llm_call_log_ts_desc          ON llm_call_log (ts DESC);
CREATE INDEX IF NOT EXISTS llm_call_log_site_ts_desc     ON llm_call_log (site, ts DESC);
CREATE INDEX IF NOT EXISTS llm_call_log_decision_ts_desc ON llm_call_log (decision, ts DESC);
