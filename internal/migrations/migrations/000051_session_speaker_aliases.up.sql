-- SCRUM-404: per-session speaker alias mapping. Maps raw
-- transcript_segments.speaker_label strings to a canonical synthetic person
-- (canonical_person_id) within the same session. Cross-session reconciliation
-- is explicitly out of scope (no FK to a global user table).

CREATE TABLE IF NOT EXISTS session_speaker_aliases (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id             UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    canonical_person_id    UUID NOT NULL,
    source_label           TEXT NOT NULL,
    source_recording_id    UUID NULL REFERENCES video_sources(id) ON DELETE CASCADE,
    canonical_display_name TEXT NOT NULL,
    canonical_email        TEXT NULL,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One alias per (session, source label, recording). NULL source_recording_id
-- means "applies session-wide for that label" (e.g. uploaded MP4 with no
-- recording-specific scoping); coalesced to the zero UUID for a null-safe
-- unique key.
CREATE UNIQUE INDEX IF NOT EXISTS uniq_speaker_alias_per_session
    ON session_speaker_aliases (
        session_id,
        source_label,
        COALESCE(source_recording_id, '00000000-0000-0000-0000-000000000000'::uuid)
    );

CREATE INDEX IF NOT EXISTS idx_speaker_alias_canonical
    ON session_speaker_aliases (session_id, canonical_person_id);
