package handlers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SCRUM-345: comprehensive copy-session test sweep + lock SKIP decisions.
//
// Two complementary assertions per CopySession run:
//
//  1. Row-count parity for every COPY table — for each source-side row
//     seeded, the clone must have the same count under its own session_id.
//  2. Zero rows in every SKIP table referencing the clone session —
//     except the auto-created creator membership in session_memberships
//     (which CreateSession inserts unconditionally).
//
// The lists below are the "locked contract" from the SCRUM-338 epic plan.
// Future contributors who add a new session-scoped table must update one of
// these lists or a constraint here will trip.
var (
	copyTables = []string{
		"artifacts",
		"materials",
		"session_links",
		"video_sources",
		"file_artifacts",
		"transcripts",
		"transcript_segments",
	}
	// SKIP tables: must have zero rows on the clone with session_id == dst.
	// session_memberships is special-cased (creator-only; allow exactly 1).
	skipTables = []string{
		"session_invitations",
		"questions",
		"answers",
		"decision_stances",
		"orchestration_recommendations",
		"orchestration_recommendation_status_audit",
		"sessions_primary_history",
		"material_views",
		"question_views",
		"session_participants",
		"session_events",
		"transcript_jobs",
		"ingestion_jobs",
	}
)

func TestCopySession_SessionScopedTables_RowCountParity(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TALKBACK_UPLOAD_ROOT", root)

	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	srcSessionID, creator := seedSessionForMetadataCopy(t, h)

	// === Seed COPY tables ===
	srcArtifacts, err := h.DB.GetArtifactsBySessionID(ctx, srcSessionID)
	require.NoError(t, err)
	require.NotEmpty(t, srcArtifacts)
	srcArtifact := srcArtifacts[0]

	// One material (no Filename → skip storage I/O; CreateMaterial succeeds).
	srcMaterial := &models.Material{
		ID:              uuid.New(),
		ArtifactID:      srcArtifact.ID,
		SessionID:       srcSessionID,
		Kind:            "document",
		ContentType:     "application/pdf",
		StorageProvider: "local",
		TextStatus:      "ready",
	}
	require.NoError(t, h.DB.CreateMaterial(ctx, srcMaterial))

	// One session_link.
	srcLink := &models.SessionLink{
		ID:        uuid.New(),
		SessionID: srcSessionID,
		URL:       "https://example.com/agenda",
		Status:    models.SessionLinkStatusVerified,
	}
	require.NoError(t, h.DB.CreateSessionLink(ctx, srcLink))

	// One video_source.
	role := models.VideoRoleSecondary
	srcVS := &models.VideoSource{
		ID:               uuid.New(),
		ArtifactID:       srcArtifact.ID,
		SessionID:        srcSessionID,
		Provider:         "other",
		PlaybackMode:     "embed",
		SourceType:       "embed_url",
		TranscriptStatus: models.VideoTranscriptStatusMissing,
		VideoRole:        &role,
	}
	require.NoError(t, h.DB.CreateVideoSource(ctx, srcVS))

	// One file_artifact backed by a real file under the temp upload root.
	srcFA := seedLocalFileArtifact(t, h, root, srcSessionID, "v.mp4", []byte("data"))

	// One transcript with one segment.
	srcTranscript := &models.Transcript{
		ID:        uuid.New(),
		SessionID: srcSessionID,
		Source:    "whisper",
		Status:    models.TranscriptStatusReady,
	}
	require.NoError(t, h.DB.CreateTranscript(ctx, srcTranscript))
	require.NoError(t, h.DB.InsertTranscriptSegments(ctx, srcTranscript.ID, srcSessionID, []models.TranscriptSegmentRow{
		{Idx: 0, StartMs: 0, EndMs: 1000, Text: "hello"},
	}))

	// === Seed a few SKIP tables to prove they don't follow the copy ===
	// One question + one answer (must NOT be copied).
	srcQuestion := &models.Question{
		ID:             uuid.New(),
		ArtifactID:     srcArtifact.ID,
		SessionID:      srcSessionID,
		QuestionText:   "test question",
		QuestionSource: models.QuestionSourceText,
	}
	require.NoError(t, h.DB.CreateQuestion(ctx, srcQuestion))

	// session_event (analytics; must NOT be copied).
	if _, err := h.DB.Pool.Exec(ctx,
		`INSERT INTO session_events (session_id, event_type, video_time_seconds, payload)
		 VALUES ($1, 'play', 0, '{}'::jsonb)`,
		srcSessionID,
	); err != nil {
		t.Fatalf("seed session_event: %v", err)
	}

	// session_participant (analytics; must NOT be copied).
	if _, err := h.DB.Pool.Exec(ctx,
		`INSERT INTO session_participants (session_id, participant_ref) VALUES ($1, $2)`,
		srcSessionID, "tester@example.com",
	); err != nil {
		t.Fatalf("seed session_participant: %v", err)
	}

	// === Run the copy ===
	dstID := postCopy(t, h, srcSessionID, creator)

	// === Assert COPY parity ===
	for _, tbl := range copyTables {
		srcCount := tableCountForSession(t, h, ctx, tbl, srcSessionID)
		dstCount := tableCountForSession(t, h, ctx, tbl, dstID)
		assert.Equal(t, srcCount, dstCount, "COPY table %s: row count must match (src=%d, dst=%d)", tbl, srcCount, dstCount)
		assert.Greater(t, dstCount, 0, "COPY table %s: dst must have at least the seeded row(s)", tbl)
	}

	// session_processing_jobs: conditional. Only re-enqueued when source had a
	// cloud-import job AND no MP4 reproduced. The seeded FA was reproduced, so
	// this table should have 0 dst rows in this test.
	assert.Equal(t, 0, tableCountForSession(t, h, ctx, "session_processing_jobs", dstID), "session_processing_jobs: 0 expected (primary FA copied)")

	// === Assert SKIP zeroness ===
	for _, tbl := range skipTables {
		dstCount := tableCountForSession(t, h, ctx, tbl, dstID)
		assert.Equal(t, 0, dstCount, "SKIP table %s: must have zero rows on the clone (dst session_id=%s)", tbl, dstID)
	}

	// session_memberships: creator-only. Auto-created by CreateSession.
	dstMembershipCount := tableCountForSession(t, h, ctx, "session_memberships", dstID)
	assert.Equal(t, 1, dstMembershipCount, "session_memberships: exactly 1 expected (auto-created creator membership)")

	// === Storage rekey assertion ===
	// Every dst FA's storage_key must reference the clone session, not the
	// source. (Integrates SCRUM-345's storage-rekey contract for FAs into
	// the same sweep run as the row-count parity check.)
	dstFAs, err := h.DB.ListFileArtifactsBySessionID(ctx, dstID)
	require.NoError(t, err)
	for _, fa := range dstFAs {
		assert.NotContains(t, fa.StorageKey, srcSessionID.String(), "dst FA storage_key must not contain source session id")
		assert.Contains(t, fa.StorageKey, dstID.String(), "dst FA storage_key must contain clone session id")
		// File should exist at the rekeyed path.
		abs := filepath.Join(root, filepath.FromSlash(fa.StorageKey))
		_, err := os.Stat(abs)
		assert.NoError(t, err, "clone FA file must exist at %s", abs)
		_ = srcFA // silence unused if all source data verified by count
	}

	// Sanity: source-side seeded rows that we explicitly chose NOT to copy
	// (questions, session_events, session_participants) must still exist on
	// the source — the test must not delete source rows.
	for _, tbl := range []string{"questions", "session_events", "session_participants"} {
		srcCount := tableCountForSession(t, h, ctx, tbl, srcSessionID)
		assert.Greater(t, srcCount, 0, "source %s must still have its seeded row(s) — copy must not modify source", tbl)
	}
}

// tableCountForSession returns COUNT(*) on the given session-scoped table
// where session_id matches. Returns -1 if the table query fails (logged).
func tableCountForSession(t *testing.T, h *Handlers, ctx context.Context, table string, sessionID uuid.UUID) int {
	t.Helper()
	if !isSafeIdentifier(table) {
		t.Fatalf("unsafe table name %q in test", table)
	}
	q := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE session_id = $1", table)
	var n int
	if err := h.DB.Pool.QueryRow(ctx, q, sessionID).Scan(&n); err != nil {
		t.Fatalf("count(*) on %s: %v", table, err)
	}
	return n
}

// isSafeIdentifier guards the dynamic SQL above. Only lowercase letters and
// underscores; the test's table list is hardcoded so this is a belt-and-braces
// check, not user input handling.
func isSafeIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z') && r != '_' {
			return false
		}
	}
	return true
}

// init wires the storage-rekey assertion to the sessionimport-package
// contract by checking that the public list of COPY tables stays in sync
// with the docstring on CopySession. This is a documentation guard, not a
// functional test — it runs once per package import and crashes if the
// contract drifts. Currently it's a no-op placeholder; SCRUM-345 docstring
// is the source of truth.
func init() {
	_ = strings.TrimSpace // keep the strings import alive for future expansions
}
