package database

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/psuthar/talkback/internal/models"
)

func (db *DB) CreateTranscript(ctx context.Context, t *models.Transcript) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	query := `
		INSERT INTO transcripts (id, session_id, source, language, status, raw_text, error_message)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (session_id, source) DO UPDATE SET
			status = EXCLUDED.status,
			raw_text = EXCLUDED.raw_text,
			error_message = EXCLUDED.error_message,
			updated_at = now()
		RETURNING id, created_at, updated_at
	`
	err := db.Pool.QueryRow(ctx, query,
		t.ID,
		t.SessionID,
		t.Source,
		t.Language,
		t.Status,
		t.RawText,
		t.ErrorMessage,
	).Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create transcript: %w", err)
	}
	return nil
}

// GetTranscriptBySessionID returns the transcript for a session (by source if provided).
// If source is "", returns the first transcript for the session (e.g. zoom).
func (db *DB) GetTranscriptBySessionID(ctx context.Context, sessionID uuid.UUID, source string) (*models.Transcript, error) {
	var query string
	var args []interface{}
	if source != "" {
		query = `SELECT id, session_id, source, language, status, raw_text, error_message, created_at, updated_at
			FROM transcripts WHERE session_id = $1 AND source = $2`
		args = []interface{}{sessionID, source}
	} else {
		query = `SELECT id, session_id, source, language, status, raw_text, error_message, created_at, updated_at
			FROM transcripts WHERE session_id = $1 ORDER BY created_at DESC LIMIT 1`
		args = []interface{}{sessionID}
	}
	var t models.Transcript
	err := db.Pool.QueryRow(ctx, query, args...).Scan(
		&t.ID,
		&t.SessionID,
		&t.Source,
		&t.Language,
		&t.Status,
		&t.RawText,
		&t.ErrorMessage,
		&t.CreatedAt,
		&t.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get transcript: %w", err)
	}
	return &t, nil
}

func (db *DB) UpdateTranscriptStatus(ctx context.Context, transcriptID uuid.UUID, status models.TranscriptStatus, rawText, errorMessage *string) error {
	query := `
		UPDATE transcripts SET status = $1, raw_text = $2, error_message = $3, updated_at = now()
		WHERE id = $4
	`
	_, err := db.Pool.Exec(ctx, query, status, rawText, errorMessage, transcriptID)
	if err != nil {
		return fmt.Errorf("update transcript status: %w", err)
	}
	return nil
}

// DeleteSegmentsByTranscriptID removes all segments for a transcript (for idempotent re-insert).
func (db *DB) DeleteSegmentsByTranscriptID(ctx context.Context, transcriptID uuid.UUID) error {
	_, err := db.Pool.Exec(ctx, `DELETE FROM transcript_segments WHERE transcript_id = $1`, transcriptID)
	if err != nil {
		return fmt.Errorf("delete transcript segments: %w", err)
	}
	return nil
}

// InsertTranscriptSegments inserts segments for a transcript. Caller should delete existing segments first for idempotency.
func (db *DB) InsertTranscriptSegments(ctx context.Context, transcriptID, sessionID uuid.UUID, segments []models.TranscriptSegmentRow) error {
	if len(segments) == 0 {
		return nil
	}
	for _, s := range segments {
		id := s.ID
		if id == uuid.Nil {
			id = uuid.New()
		}
		query := `INSERT INTO transcript_segments (id, transcript_id, session_id, idx, start_ms, end_ms, text, speaker_label, source_ref)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
		_, err := db.Pool.Exec(ctx, query,
			id, transcriptID, sessionID, s.Idx, s.StartMs, s.EndMs, s.Text, s.SpeakerLabel, s.SourceRef,
		)
		if err != nil {
			return fmt.Errorf("insert transcript segment: %w", err)
		}
	}
	return nil
}

// ListSegmentsByTranscriptID returns segments ordered by idx.
func (db *DB) ListSegmentsByTranscriptID(ctx context.Context, transcriptID uuid.UUID) ([]models.TranscriptSegmentRow, error) {
	query := `SELECT id, transcript_id, session_id, idx, start_ms, end_ms, text, speaker_label, source_ref, created_at
		FROM transcript_segments WHERE transcript_id = $1 ORDER BY idx`
	rows, err := db.Pool.Query(ctx, query, transcriptID)
	if err != nil {
		return nil, fmt.Errorf("list transcript segments: %w", err)
	}
	defer rows.Close()
	var out []models.TranscriptSegmentRow
	for rows.Next() {
		var s models.TranscriptSegmentRow
		err := rows.Scan(&s.ID, &s.TranscriptID, &s.SessionID, &s.Idx, &s.StartMs, &s.EndMs, &s.Text, &s.SpeakerLabel, &s.SourceRef, &s.CreatedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
