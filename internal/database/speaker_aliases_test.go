package database

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSessionSpeakerAliasesMigration covers the SCRUM-404 schema:
//   - session_speaker_aliases table exists with the expected columns,
//   - uniq_speaker_alias_per_session enforces one alias per
//     (session, source_label, source_recording_id) with NULL recording
//     coalesced to the zero UUID,
//   - ON DELETE CASCADE cleans up rows when the session or recording is
//     deleted,
//   - the DB-layer wrappers (Upsert/List/Resolve/Delete and the distinct
//     speaker-label observation listing) round-trip correctly.
func TestSessionSpeakerAliasesMigration(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "speaker alias session")

	canonicalAlice := uuid.New()
	canonicalBob := uuid.New()

	// Helper: insert a video_sources row tied to this session so we can scope
	// aliases to a recording id (and exercise the recording cascade).
	createRecording := func(t *testing.T) uuid.UUID {
		t.Helper()
		artifactID := uuid.New()
		_, err := db.Pool.Exec(ctx, `
			INSERT INTO artifacts (id, session_id, title, status, created_at, updated_at)
			VALUES ($1, $2, 'alias artifact', 'ready', now(), now())
		`, artifactID, session.ID)
		require.NoError(t, err)

		vsID := uuid.New()
		_, err = db.Pool.Exec(ctx, `
			INSERT INTO video_sources (id, artifact_id, session_id, provider, video_url, source_type)
			VALUES ($1, $2, $3, 'zoom', 'https://example.com/v.mp4', 'upload')
		`, vsID, artifactID, session.ID)
		require.NoError(t, err)
		return vsID
	}

	t.Run("upsert + list session-wide alias", func(t *testing.T) {
		alice := "alice@example.com"
		require.NoError(t, db.UpsertAlias(ctx, session.ID, "Speaker 0", nil, canonicalAlice, "Alice", &alice))

		aliases, err := db.ListAliasesBySession(ctx, session.ID)
		require.NoError(t, err)
		require.Len(t, aliases, 1)
		assert.Equal(t, "Speaker 0", aliases[0].SourceLabel)
		assert.Equal(t, canonicalAlice, aliases[0].CanonicalPersonID)
		assert.Equal(t, "Alice", aliases[0].CanonicalDisplayName)
		require.NotNil(t, aliases[0].CanonicalEmail)
		assert.Equal(t, alice, *aliases[0].CanonicalEmail)
		assert.Nil(t, aliases[0].SourceRecordingID, "session-wide alias has NULL recording")
	})

	t.Run("upsert is idempotent + updates display name + email", func(t *testing.T) {
		require.NoError(t, db.UpsertAlias(ctx, session.ID, "Speaker 0", nil, canonicalAlice, "Alice Liddell", nil))

		aliases, err := db.ListAliasesBySession(ctx, session.ID)
		require.NoError(t, err)
		require.Len(t, aliases, 1, "second upsert must replace, not duplicate")
		assert.Equal(t, "Alice Liddell", aliases[0].CanonicalDisplayName)
		assert.Nil(t, aliases[0].CanonicalEmail, "email should be cleared on update with nil")
	})

	t.Run("uniq index treats NULL recording as the same key", func(t *testing.T) {
		// Direct INSERT should violate the partial-null-safe unique index for the
		// same (session, source_label, NULL recording) triple.
		_, err := db.Pool.Exec(ctx, `
			INSERT INTO session_speaker_aliases
			    (session_id, canonical_person_id, source_label, source_recording_id,
			     canonical_display_name, canonical_email)
			VALUES ($1, $2, 'Speaker 0', NULL, 'Different', NULL)
		`, session.ID, canonicalBob)
		assert.Error(t, err, "second NULL-recording row for same (session, label) must be rejected")
	})

	t.Run("recording-scoped + session-wide coexist for same label", func(t *testing.T) {
		recID := createRecording(t)
		require.NoError(t, db.UpsertAlias(ctx, session.ID, "Speaker 0", &recID, canonicalBob, "Bob", nil))

		aliases, err := db.ListAliasesBySession(ctx, session.ID)
		require.NoError(t, err)
		require.Len(t, aliases, 2, "session-wide and recording-scoped rows must both persist")
	})

	t.Run("resolve prefers recording-scoped row when sourceRecordingID provided", func(t *testing.T) {
		recID := createRecording(t)
		require.NoError(t, db.UpsertAlias(ctx, session.ID, "Host", nil, canonicalAlice, "Alice", nil))
		require.NoError(t, db.UpsertAlias(ctx, session.ID, "Host", &recID, canonicalBob, "Bob", nil))

		cpRec, nameRec, ok, err := db.ResolveCanonical(ctx, session.ID, "Host", &recID)
		require.NoError(t, err)
		require.True(t, ok)
		require.NotNil(t, cpRec)
		assert.Equal(t, canonicalBob, *cpRec, "recording-scoped row must win when recording id provided")
		assert.Equal(t, "Bob", nameRec)

		cpFallback, nameFallback, ok, err := db.ResolveCanonical(ctx, session.ID, "Host", nil)
		require.NoError(t, err)
		require.True(t, ok)
		require.NotNil(t, cpFallback)
		assert.Equal(t, canonicalAlice, *cpFallback, "no recording id falls back to session-wide row")
		assert.Equal(t, "Alice", nameFallback)
	})

	t.Run("resolve unknown label returns ok=false, not an error", func(t *testing.T) {
		cp, name, ok, err := db.ResolveCanonical(ctx, session.ID, "Speaker 99999", nil)
		require.NoError(t, err)
		assert.False(t, ok)
		assert.Nil(t, cp)
		assert.Equal(t, "", name)
	})

	t.Run("delete removes a single row", func(t *testing.T) {
		s := createTestSession(t, db, "alias delete session")
		require.NoError(t, db.UpsertAlias(ctx, s.ID, "X", nil, uuid.New(), "X disp", nil))

		aliases, err := db.ListAliasesBySession(ctx, s.ID)
		require.NoError(t, err)
		require.Len(t, aliases, 1)

		require.NoError(t, db.DeleteAlias(ctx, aliases[0].ID))

		aliases, err = db.ListAliasesBySession(ctx, s.ID)
		require.NoError(t, err)
		assert.Empty(t, aliases)
	})

	t.Run("delete is idempotent", func(t *testing.T) {
		err := db.DeleteAlias(ctx, uuid.New())
		assert.NoError(t, err, "deleting a non-existent alias is a no-op")
	})

	t.Run("aliases cascade on session delete", func(t *testing.T) {
		s := createTestSession(t, db, "alias cascade session")
		require.NoError(t, db.UpsertAlias(ctx, s.ID, "Q", nil, uuid.New(), "Q disp", nil))

		_, err := db.Pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, s.ID)
		require.NoError(t, err)

		aliases, err := db.ListAliasesBySession(ctx, s.ID)
		require.NoError(t, err)
		assert.Empty(t, aliases)
	})

	t.Run("recording-scoped alias cascades when recording is deleted", func(t *testing.T) {
		s := createTestSession(t, db, "alias recording cascade session")
		artifactID := uuid.New()
		_, err := db.Pool.Exec(ctx, `
			INSERT INTO artifacts (id, session_id, title, status, created_at, updated_at)
			VALUES ($1, $2, 'a', 'ready', now(), now())
		`, artifactID, s.ID)
		require.NoError(t, err)

		recID := uuid.New()
		_, err = db.Pool.Exec(ctx, `
			INSERT INTO video_sources (id, artifact_id, session_id, provider, video_url, source_type)
			VALUES ($1, $2, $3, 'zoom', 'https://example.com/v.mp4', 'upload')
		`, recID, artifactID, s.ID)
		require.NoError(t, err)

		require.NoError(t, db.UpsertAlias(ctx, s.ID, "R", &recID, uuid.New(), "R disp", nil))
		// Session-wide alias should survive the recording delete.
		require.NoError(t, db.UpsertAlias(ctx, s.ID, "R", nil, uuid.New(), "R session", nil))

		_, err = db.Pool.Exec(ctx, `DELETE FROM video_sources WHERE id = $1`, recID)
		require.NoError(t, err)

		aliases, err := db.ListAliasesBySession(ctx, s.ID)
		require.NoError(t, err)
		require.Len(t, aliases, 1, "only the session-wide alias should remain")
		assert.Nil(t, aliases[0].SourceRecordingID)
	})

	t.Run("ListDistinctSpeakerLabels reads from transcript_segments", func(t *testing.T) {
		s := createTestSession(t, db, "alias label observation session")

		// transcript_segments requires a parent transcript row (FK).
		transcriptID := uuid.New()
		_, err := db.Pool.Exec(ctx, `
			INSERT INTO transcripts (id, session_id, source, status)
			VALUES ($1, $2, 'zoom', 'ready')
		`, transcriptID, s.ID)
		require.NoError(t, err)

		segs := []struct {
			idx   int
			label string
		}{
			{1, "Speaker 0"},
			{2, "Speaker 0"},
			{3, "Speaker 1"},
			{4, ""},    // empty label must be filtered out
		}
		for _, sg := range segs {
			label := sg.label
			var labelArg interface{} = label
			if label == "" {
				labelArg = nil
			}
			_, err := db.Pool.Exec(ctx, `
				INSERT INTO transcript_segments (transcript_id, session_id, idx, start_ms, end_ms, text, speaker_label)
				VALUES ($1, $2, $3, $4, $5, $6, $7)
			`, transcriptID, s.ID, sg.idx, sg.idx*1000, sg.idx*1000+500, "hello", labelArg)
			require.NoError(t, err)
		}

		labels, err := db.ListDistinctSpeakerLabels(ctx, s.ID)
		require.NoError(t, err)
		require.Len(t, labels, 2, "two distinct non-empty labels expected")
		assert.Equal(t, "Speaker 0", labels[0].SourceLabel)
		assert.Equal(t, 2, labels[0].SegmentCount)
		assert.Equal(t, "Speaker 1", labels[1].SourceLabel)
		assert.Equal(t, 1, labels[1].SegmentCount)
	})
}
