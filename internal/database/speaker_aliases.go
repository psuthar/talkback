package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/psuthar/talkback/internal/models"
)

// ListAliasesBySession returns every alias mapping for a session, ordered by
// source_label so the People panel renders deterministically.
func (db *DB) ListAliasesBySession(ctx context.Context, sessionID uuid.UUID) ([]models.SessionSpeakerAlias, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT id, session_id, canonical_person_id, source_label, source_recording_id,
		       canonical_display_name, canonical_email, created_at, updated_at
		FROM session_speaker_aliases
		WHERE session_id = $1
		ORDER BY source_label, source_recording_id NULLS FIRST
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list speaker aliases: %w", err)
	}
	defer rows.Close()

	out := []models.SessionSpeakerAlias{}
	for rows.Next() {
		var a models.SessionSpeakerAlias
		if err := rows.Scan(
			&a.ID,
			&a.SessionID,
			&a.CanonicalPersonID,
			&a.SourceLabel,
			&a.SourceRecordingID,
			&a.CanonicalDisplayName,
			&a.CanonicalEmail,
			&a.CreatedAt,
			&a.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan speaker alias: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate speaker aliases: %w", err)
	}
	return out, nil
}

// UpsertAlias creates or replaces the alias mapping for the
// (session_id, source_label, source_recording_id) key. A nil sourceRecordingID
// means "applies session-wide for that label."
func (db *DB) UpsertAlias(
	ctx context.Context,
	sessionID uuid.UUID,
	sourceLabel string,
	sourceRecordingID *uuid.UUID,
	canonicalPersonID uuid.UUID,
	displayName string,
	email *string,
) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO session_speaker_aliases (
			session_id, canonical_person_id, source_label, source_recording_id,
			canonical_display_name, canonical_email
		) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (session_id, source_label, COALESCE(source_recording_id, '00000000-0000-0000-0000-000000000000'::uuid))
		DO UPDATE SET
			canonical_person_id = EXCLUDED.canonical_person_id,
			canonical_display_name = EXCLUDED.canonical_display_name,
			canonical_email = EXCLUDED.canonical_email,
			updated_at = now()
	`, sessionID, canonicalPersonID, sourceLabel, sourceRecordingID, displayName, email)
	if err != nil {
		return fmt.Errorf("upsert speaker alias: %w", err)
	}
	return nil
}

// ResolveCanonical looks up the canonical person for a raw speaker label in
// the given session. ok=false means no mapping; callers should fall back to
// the raw label.
//
// Lookup order: recording-scoped row (if sourceRecordingID is non-nil) wins
// over the session-wide row for the same label, so per-recording overrides
// take precedence over the default.
func (db *DB) ResolveCanonical(
	ctx context.Context,
	sessionID uuid.UUID,
	sourceLabel string,
	sourceRecordingID *uuid.UUID,
) (canonicalPersonID *uuid.UUID, displayName string, ok bool, err error) {
	// When sourceRecordingID is non-nil, prefer a recording-scoped row but fall
	// back to a session-wide (NULL recording) row for the same label. When
	// sourceRecordingID is nil, only match session-wide rows.
	query := `
		SELECT canonical_person_id, canonical_display_name
		FROM session_speaker_aliases
		WHERE session_id = $1
		  AND source_label = $2
		  AND (
		    ($3::uuid IS NOT NULL AND source_recording_id = $3)
		    OR source_recording_id IS NULL
		  )
		ORDER BY (source_recording_id IS NOT NULL) DESC
		LIMIT 1
	`
	var cp uuid.UUID
	var name string
	scanErr := db.Pool.QueryRow(ctx, query, sessionID, sourceLabel, sourceRecordingID).
		Scan(&cp, &name)
	if scanErr != nil {
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return nil, "", false, nil
		}
		return nil, "", false, fmt.Errorf("resolve speaker alias: %w", scanErr)
	}
	return &cp, name, true, nil
}

// DeleteAlias removes a single alias row by id (idempotent — no error if
// already gone).
func (db *DB) DeleteAlias(ctx context.Context, id uuid.UUID) error {
	_, err := db.Pool.Exec(ctx, `DELETE FROM session_speaker_aliases WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete speaker alias: %w", err)
	}
	return nil
}

// ListDistinctSpeakerLabels returns the raw speaker_label values observed in
// a session's transcript_segments along with how many segments each label
// produced. The People panel uses this to render the list of labels still
// needing reconciliation.
//
// Recording-scoped observations (SourceRecordingID) are not populated yet:
// the current schema has no transcript_segments → video_sources link.
// Once a later ticket joins those (e.g. SCRUM-408 / SCRUM-415's pipeline
// rework), this function will populate SourceRecordingID per row.
func (db *DB) ListDistinctSpeakerLabels(ctx context.Context, sessionID uuid.UUID) ([]models.SpeakerLabelObservation, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT speaker_label, COUNT(*) AS segment_count
		FROM transcript_segments
		WHERE session_id = $1
		  AND speaker_label IS NOT NULL
		  AND speaker_label <> ''
		GROUP BY speaker_label
		ORDER BY speaker_label
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list speaker labels: %w", err)
	}
	defer rows.Close()

	out := []models.SpeakerLabelObservation{}
	for rows.Next() {
		var o models.SpeakerLabelObservation
		if err := rows.Scan(&o.SourceLabel, &o.SegmentCount); err != nil {
			return nil, fmt.Errorf("scan speaker label: %w", err)
		}
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate speaker labels: %w", err)
	}
	return out, nil
}
