package database

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParticipantHasAnyMaterialView(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "Engagement session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "A", nil)
	require.NoError(t, err)

	mid := uuid.New()
	require.NoError(t, db.CreateMaterial(ctx, &models.Material{
		ID:          mid,
		ArtifactID:  artifact.ID,
		SessionID:   session.ID,
		Kind:        "document",
		Filename:    "x.txt",
		ContentType: "text/plain",
		StorageURL:  "data/x.txt",
		TextStatus:    models.MaterialTextStatusReady,
	}))

	ref := "viewer@example.com"
	ok, err := db.ParticipantHasAnyMaterialView(ctx, session.ID, ref)
	require.NoError(t, err)
	assert.False(t, ok)

	require.NoError(t, db.MarkMaterialsSeenByParticipant(ctx, session.ID, ref, []uuid.UUID{mid}))

	ok, err = db.ParticipantHasAnyMaterialView(ctx, session.ID, ref)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestParticipantHasAnyQuestionView(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "Q engagement")
	artifact, err := db.CreateArtifact(ctx, session.ID, "A", nil)
	require.NoError(t, err)

	qid := uuid.New()
	require.NoError(t, db.CreateQuestion(ctx, &models.Question{
		ID:             qid,
		ArtifactID:     artifact.ID,
		SessionID:      session.ID,
		QuestionText:   "Hi?",
		QuestionSource: models.QuestionSourceText,
	}))

	ref := "asker@example.com"
	ok, err := db.ParticipantHasAnyQuestionView(ctx, session.ID, ref)
	require.NoError(t, err)
	assert.False(t, ok)

	require.NoError(t, db.MarkQuestionViewed(ctx, session.ID, ref, qid))

	ok, err = db.ParticipantHasAnyQuestionView(ctx, session.ID, ref)
	require.NoError(t, err)
	assert.True(t, ok)
}
