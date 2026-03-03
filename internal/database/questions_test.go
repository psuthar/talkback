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

func TestCreateQuestion_WithParent(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "Parent Test Session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "Artifact", nil)
	require.NoError(t, err)

	rootQ := &models.Question{
		ID:             uuid.New(),
		ArtifactID:     artifact.ID,
		SessionID:      session.ID,
		QuestionText:   "Root?",
		QuestionSource: models.QuestionSourceText,
	}
	err = db.CreateQuestion(ctx, rootQ)
	require.NoError(t, err)

	replyQ := &models.Question{
		ID:                uuid.New(),
		ArtifactID:        artifact.ID,
		SessionID:         session.ID,
		ParentQuestionID:  &rootQ.ID,
		QuestionText:      "Reply?",
		QuestionSource:    models.QuestionSourceText,
	}
	err = db.CreateQuestion(ctx, replyQ)
	require.NoError(t, err)

	got, err := db.GetQuestionByID(ctx, replyQ.ID)
	require.NoError(t, err)
	require.NotNil(t, got.ParentQuestionID)
	assert.Equal(t, rootQ.ID, *got.ParentQuestionID)
	assert.Equal(t, "Reply?", got.QuestionText)
}

func TestFindExistingQuestionByText_ThreadAware(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "Find Session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "Artifact", nil)
	require.NoError(t, err)

	rootQ := &models.Question{
		ID:             uuid.New(),
		ArtifactID:     artifact.ID,
		SessionID:      session.ID,
		QuestionText:   "Same text",
		QuestionSource: models.QuestionSourceText,
	}
	err = db.CreateQuestion(ctx, rootQ)
	require.NoError(t, err)

	t.Run("finds root when parent is nil", func(t *testing.T) {
		q, a, err := db.FindExistingQuestionByText(ctx, session.ID, "Same text", nil)
		require.NoError(t, err)
		require.NotNil(t, q)
		assert.Equal(t, rootQ.ID, q.ID)
		assert.Nil(t, q.ParentQuestionID)
		assert.Nil(t, a)
	})

	t.Run("does not find root when parent is set", func(t *testing.T) {
		q, a, err := db.FindExistingQuestionByText(ctx, session.ID, "Same text", &rootQ.ID)
		require.NoError(t, err)
		assert.Nil(t, q)
		assert.Nil(t, a)
	})

	replyQ := &models.Question{
		ID:                uuid.New(),
		ArtifactID:        artifact.ID,
		SessionID:         session.ID,
		ParentQuestionID:  &rootQ.ID,
		QuestionText:      "Same text",
		QuestionSource:    models.QuestionSourceText,
	}
	err = db.CreateQuestion(ctx, replyQ)
	require.NoError(t, err)

	t.Run("finds reply when parent matches", func(t *testing.T) {
		q, a, err := db.FindExistingQuestionByText(ctx, session.ID, "Same text", &rootQ.ID)
		require.NoError(t, err)
		require.NotNil(t, q)
		assert.Equal(t, replyQ.ID, q.ID)
		require.NotNil(t, q.ParentQuestionID)
		assert.Equal(t, rootQ.ID, *q.ParentQuestionID)
		assert.Nil(t, a)
	})

	t.Run("different thread same text returns reply not root", func(t *testing.T) {
		otherParentID := uuid.New()
		q, _, err := db.FindExistingQuestionByText(ctx, session.ID, "Same text", &otherParentID)
		require.NoError(t, err)
		assert.Nil(t, q)
	})
}
