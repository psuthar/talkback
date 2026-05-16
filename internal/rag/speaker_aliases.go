// SCRUM-425: speaker-alias resolution for RAG / QA / decisions citations.
// SCRUM-404 stores per-session aliases (source_label -> canonical_person_id
// + display name); SCRUM-XX13's People panel writes them; this file lets
// the caller (session_ask / orchestration_draft / mcp Q&A) resolve a
// raw transcript speaker_label to its canonical display name at query
// time. Recording-scoped aliases win over session-wide ones — matches
// SessionSpeakerAliases.ResolveCanonical precedence.
package rag

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
)

// SessionSpeakerCanonical is the resolved citation-rendering payload for
// one raw speaker_label in a session.
type SessionSpeakerCanonical struct {
	SourceLabel       string
	CanonicalPersonID uuid.UUID
	DisplayName       string
	Email             *string
}

// BuildSpeakerCanonicalMap loads every alias for the session in a single
// SELECT and returns a map from raw source_label to its canonical
// representation. When the same source_label has both a recording-scoped
// and a session-wide alias row, the recording-scoped one wins (mirrors
// ResolveCanonical's lookup precedence).
//
// Callers should invoke this once per QA / decisions request and pass the
// map to the citation-rendering layer. Resolution is at query time so
// SCRUM-XX13's People panel writes are observed immediately without
// re-indexing.
func BuildSpeakerCanonicalMap(ctx context.Context, db *database.DB, sessionID uuid.UUID) (map[string]SessionSpeakerCanonical, error) {
	rows, err := db.ListAliasesBySession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("build speaker canonical map: %w", err)
	}
	return buildCanonicalMapFromAliases(rows), nil
}

// buildCanonicalMapFromAliases is the pure precedence-resolving helper —
// extracted so the SCRUM-425 recording-scoped vs session-wide rule can be
// unit-tested without the DB. Recording-scoped rows (SourceRecordingID
// != nil) outrank session-wide rows for the same source_label.
func buildCanonicalMapFromAliases(rows []models.SessionSpeakerAlias) map[string]SessionSpeakerCanonical {
	// We process the list twice: first pass picks any alias per label,
	// second pass overwrites with the recording-scoped row if one exists.
	out := make(map[string]SessionSpeakerCanonical, len(rows))
	for _, a := range rows {
		if _, ok := out[a.SourceLabel]; !ok {
			out[a.SourceLabel] = SessionSpeakerCanonical{
				SourceLabel:       a.SourceLabel,
				CanonicalPersonID: a.CanonicalPersonID,
				DisplayName:       a.CanonicalDisplayName,
				Email:             a.CanonicalEmail,
			}
		}
	}
	for _, a := range rows {
		if a.SourceRecordingID == nil {
			continue
		}
		out[a.SourceLabel] = SessionSpeakerCanonical{
			SourceLabel:       a.SourceLabel,
			CanonicalPersonID: a.CanonicalPersonID,
			DisplayName:       a.CanonicalDisplayName,
			Email:             a.CanonicalEmail,
		}
	}
	return out
}

// ResolveLabel returns the canonical display name for a raw speaker_label
// using the precomputed map, or the raw label when no mapping exists
// (current behavior — caller still renders something).
func ResolveLabel(canonicalMap map[string]SessionSpeakerCanonical, sourceLabel string) string {
	if canonicalMap == nil {
		return sourceLabel
	}
	if c, ok := canonicalMap[sourceLabel]; ok && c.DisplayName != "" {
		return c.DisplayName
	}
	return sourceLabel
}

// LabelsForCanonical returns every raw speaker_label that maps to the
// given canonical_person_id. The QA / decisions filter UI uses this when
// a user filters by "speaker X" — we expand to every alias in the same
// canonical group.
func LabelsForCanonical(canonicalMap map[string]SessionSpeakerCanonical, canonical uuid.UUID) []string {
	if canonicalMap == nil {
		return nil
	}
	var out []string
	for _, c := range canonicalMap {
		if c.CanonicalPersonID == canonical {
			out = append(out, c.SourceLabel)
		}
	}
	return out
}
