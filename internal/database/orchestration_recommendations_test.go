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

func TestCreateAndListOrchestrationRecommendationsBySessionID(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	sessionA := createTestSession(t, db, "Orchestration A")
	sessionB := createTestSession(t, db, "Orchestration B")

	questionID := uuid.New()
	rec := &models.OrchestrationRecommendation{
		SessionID:          sessionA.ID,
		RecommendationType: models.RecommendationTypeUnansweredQuestion,
		Status:             models.RecommendationStatusNew,
		Summary:            "Unanswered question needs creator review",
		Evidence: []models.RecommendationEvidenceRef{
			{
				SourceType: "question",
				QuestionID: &questionID,
			},
		},
		MetadataJSON: map[string]interface{}{
			"reason": "overdue_question",
		},
	}
	confidence := float32(0.82)
	rec.ConfidenceScore = &confidence

	require.NoError(t, db.CreateOrchestrationRecommendation(ctx, rec))
	require.NotEqual(t, uuid.Nil, rec.ID)
	require.False(t, rec.CreatedAt.IsZero())
	require.False(t, rec.UpdatedAt.IsZero())

	other := &models.OrchestrationRecommendation{
		SessionID:          sessionB.ID,
		RecommendationType: models.RecommendationTypeMissingParticipant,
		Summary:            "Another session recommendation",
	}
	require.NoError(t, db.CreateOrchestrationRecommendation(ctx, other))

	got, err := db.ListOrchestrationRecommendationsBySessionID(ctx, sessionA.ID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, rec.ID, got[0].ID)
	assert.Equal(t, sessionA.ID, got[0].SessionID)
	assert.Equal(t, models.RecommendationTypeUnansweredQuestion, got[0].RecommendationType)
	assert.Equal(t, models.RecommendationStatusNew, got[0].Status)
	assert.Equal(t, "Unanswered question needs creator review", got[0].Summary)
	require.Len(t, got[0].Evidence, 1)
	assert.Equal(t, "question", got[0].Evidence[0].SourceType)
	require.NotNil(t, got[0].Evidence[0].QuestionID)
	assert.Equal(t, questionID, *got[0].Evidence[0].QuestionID)
	require.NotNil(t, got[0].ConfidenceScore)
	assert.InDelta(t, 0.82, *got[0].ConfidenceScore, 0.001)
	require.NotNil(t, got[0].MetadataJSON)
	assert.Equal(t, "overdue_question", got[0].MetadataJSON["reason"])
}

func TestCreateOrchestrationRecommendation_DefaultStatusAndStatusUpdate(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "Orchestration status")

	rec := &models.OrchestrationRecommendation{
		SessionID:          session.ID,
		RecommendationType: models.RecommendationTypeDecisionReadiness,
		Summary:            "Decision readiness review recommended",
	}
	require.NoError(t, db.CreateOrchestrationRecommendation(ctx, rec))
	assert.Equal(t, models.RecommendationStatusNew, rec.Status)

	require.NoError(t, db.UpdateOrchestrationRecommendationStatus(ctx, rec.ID, models.RecommendationStatusReviewed))

	got, err := db.ListOrchestrationRecommendationsBySessionID(ctx, session.ID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, models.RecommendationStatusReviewed, got[0].Status)
}

