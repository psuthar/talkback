package orchestration_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/orchestration"
	"github.com/psuthar/talkback/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setSessionIndexStatus(t *testing.T, db *database.DB, sessionID uuid.UUID, status string) {
	t.Helper()
	ctx := context.Background()
	_, err := db.Pool.Exec(ctx, `UPDATE sessions SET index_status = $1, index_updated_at = now() WHERE id = $2`, status, sessionID)
	require.NoError(t, err)
}

func TestEvaluator_UnansweredRootQuestion(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "Eval session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "A", nil)
	require.NoError(t, err)

	q := &models.Question{
		ID:             uuid.New(),
		ArtifactID:     artifact.ID,
		SessionID:      session.ID,
		QuestionText:   "What is the decision?",
		QuestionSource: models.QuestionSourceText,
	}
	require.NoError(t, db.CreateQuestion(ctx, q))

	ev := orchestration.NewEvaluator(db)
	recs, err := ev.EvaluateSession(ctx, session.ID)
	require.NoError(t, err)
	require.Len(t, recs, 2)

	var unanswered *models.OrchestrationRecommendation
	var readiness *models.OrchestrationRecommendation
	for _, r := range recs {
		switch r.RecommendationType {
		case models.RecommendationTypeUnansweredQuestion:
			unanswered = r
		case models.RecommendationTypeDecisionReadiness:
			readiness = r
		}
	}
	require.NotNil(t, unanswered)
	assert.Equal(t, models.RecommendationStatusNew, unanswered.Status)
	assert.Contains(t, unanswered.Summary, "Unanswered")
	require.Len(t, unanswered.Evidence, 1)
	assert.Equal(t, "question", unanswered.Evidence[0].SourceType)
	require.NotNil(t, readiness)
	assert.Equal(t, "inputs_incomplete", readiness.MetadataJSON["readiness"])
}

func TestEvaluator_ReviewDraftAnswer(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "Draft session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "A", nil)
	require.NoError(t, err)

	q := &models.Question{
		ID:             uuid.New(),
		ArtifactID:     artifact.ID,
		SessionID:      session.ID,
		QuestionText:   "Need AI answer?",
		QuestionSource: models.QuestionSourceText,
	}
	require.NoError(t, db.CreateQuestion(ctx, q))

	model := "gpt-4o-mini"
	ans := &models.Answer{
		ID:           uuid.New(),
		QuestionID:   q.ID,
		AnswerText:   "Suggested text",
		AnswerStatus: models.AnswerStatusAnswered,
		Confidence:   0.7,
		Citations:    []models.Citation{},
		Model:        &model,
		Confirmed:    false,
	}
	require.NoError(t, db.CreateAnswer(ctx, ans))

	ev := orchestration.NewEvaluator(db)
	recs, err := ev.EvaluateSession(ctx, session.ID)
	require.NoError(t, err)

	var draft *models.OrchestrationRecommendation
	for _, r := range recs {
		if r.RecommendationType == models.RecommendationTypeReviewDraftAnswer {
			draft = r
			break
		}
	}
	require.NotNil(t, draft)
	require.Len(t, draft.Evidence, 2)
}

func TestEvaluator_MissingParticipantStance(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "Stance session")

	pd := "Ship MVP next week"
	require.NoError(t, db.UpdateSessionContext(ctx, session.ID, nil, &pd, nil))

	u := &models.User{
		ID:          uuid.New(),
		Email:       "p1@example.com",
		DisplayName: "P1",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleParticipant,
	}
	require.NoError(t, db.CreateUser(ctx, u))
	require.NoError(t, db.CreateSessionMembership(ctx, session.ID, u.ID, "participant", nil))

	ev := orchestration.NewEvaluator(db)
	recs, err := ev.EvaluateSession(ctx, session.ID)
	require.NoError(t, err)

	var missing *models.OrchestrationRecommendation
	for _, r := range recs {
		if r.RecommendationType == models.RecommendationTypeMissingParticipant {
			missing = r
			break
		}
	}
	require.NotNil(t, missing)
	assert.Equal(t, u.ID.String(), missing.MetadataJSON["user_id"])
}

func TestEvaluator_DecisionReadinessPositive(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "Ready session")

	premise := "We need alignment"
	pd := "Go with option A"
	require.NoError(t, db.UpdateSessionContext(ctx, session.ID, &premise, &pd, nil))
	setSessionIndexStatus(t, db, session.ID, "ready")

	u := &models.User{
		ID:          uuid.New(),
		Email:       "voter@example.com",
		DisplayName: "Voter",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleParticipant,
	}
	require.NoError(t, db.CreateUser(ctx, u))
	require.NoError(t, db.CreateSessionMembership(ctx, session.ID, u.ID, "participant", nil))
	_, err := db.CreateOrUpdateStance(ctx, session.ID, u.ID, models.StanceAgree, nil)
	require.NoError(t, err)

	ev := orchestration.NewEvaluator(db)
	recs, err := ev.EvaluateSession(ctx, session.ID)
	require.NoError(t, err)

	var positive *models.OrchestrationRecommendation
	for _, r := range recs {
		if r.RecommendationType == models.RecommendationTypeDecisionReadiness && r.MetadataJSON != nil && r.MetadataJSON["readiness"] == "inputs_complete" {
			positive = r
			break
		}
	}
	require.NotNil(t, positive)
}

func TestEvaluator_SyncSessionRecommendations(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "Sync session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "A", nil)
	require.NoError(t, err)

	q := &models.Question{
		ID:             uuid.New(),
		ArtifactID:     artifact.ID,
		SessionID:      session.ID,
		QuestionText:   "Open?",
		QuestionSource: models.QuestionSourceText,
	}
	require.NoError(t, db.CreateQuestion(ctx, q))

	ev := orchestration.NewEvaluator(db)
	_, err = ev.SyncSessionRecommendations(ctx, session.ID)
	require.NoError(t, err)

	stored, err := db.ListOrchestrationRecommendationsBySessionID(ctx, session.ID)
	require.NoError(t, err)
	require.NotEmpty(t, stored)

	_, err = ev.SyncSessionRecommendations(ctx, session.ID)
	require.NoError(t, err)
	stored2, err := db.ListOrchestrationRecommendationsBySessionID(ctx, session.ID)
	require.NoError(t, err)
	assert.Len(t, stored2, len(stored))
}
