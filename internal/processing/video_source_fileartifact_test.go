// SCRUM-475: pin the video_source.file_artifact_id linkage. Pre-fix, the
// import pipelines created video_source rows without FileArtifactID set,
// so the frontend couldn't resolve a per-recording R2 stream URL and
// fell back to iframing the platform URL (teams.microsoft.com / Drive
// viewer / zoom.us) — none of which actually embed.
//
// Going forward CreateVideoSource must persist FileArtifactID when the
// caller sets it. This test exercises the DB layer directly so a future
// CRUD refactor that drops the column from INSERT trips immediately.
package processing

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateVideoSource_PersistsFileArtifactID(t *testing.T) {
	db, cleanup := setupTestDBForProcessing(t)
	defer cleanup()
	ctx := context.Background()

	session := createTestSession(t, db, "scrum475-fa-link")

	// Seed an artifact (legacy table; required by FK) and a file_artifact.
	legacyArtifactID := uuid.New()
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO artifacts (id, session_id, title, status, created_at, updated_at)
		VALUES ($1, $2, 'a', 'ready', now(), now())
	`, legacyArtifactID, session.ID)
	require.NoError(t, err)

	faID := uuid.New()
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO file_artifacts (id, session_id, kind, content_type, storage_provider, storage_bucket, storage_key, status, created_at, updated_at)
		VALUES ($1, $2, 'video', 'video/mp4', 'r2', 'b', $3, 'ready', now(), now())
	`, faID, session.ID, "k-"+faID.String())
	require.NoError(t, err)

	vsID := uuid.New()
	vs := &models.VideoSource{
		ID:               vsID,
		ArtifactID:       legacyArtifactID,
		SessionID:        session.ID,
		Provider:         "teams",
		VideoURL:         "https://teams.microsoft.com/",
		PlaybackMode:     "embed",
		TranscriptStatus: models.VideoTranscriptStatusReady,
		SourceType:       models.VideoSourceTypeEmbedURL,
		FileArtifactID:   &faID,
	}
	require.NoError(t, db.CreateVideoSource(ctx, vs))

	got, err := db.GetVideoSourceByID(ctx, vsID)
	require.NoError(t, err)
	require.NotNil(t, got.FileArtifactID, "file_artifact_id must persist through CreateVideoSource — frontend stream URL resolution depends on it")
	assert.Equal(t, faID, *got.FileArtifactID)
}
