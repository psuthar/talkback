package orchestration_test

import (
	"context"
	"strings"
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
	assert.Contains(t, unanswered.Summary, "Unanswered question")
	require.Len(t, unanswered.Evidence, 1)
	assert.Equal(t, "question", unanswered.Evidence[0].SourceType)
	require.NotNil(t, unanswered.MetadataJSON)
	assert.Equal(t, "unanswered", unanswered.MetadataJSON["reason"])
	assert.Equal(t, false, unanswered.MetadataJSON["overdue"])
	assert.Equal(t, "unanswered_question:"+q.ID.String(), unanswered.MetadataJSON["dedupe_key"])
	require.NotNil(t, readiness)
	assert.Equal(t, "inputs_incomplete", readiness.MetadataJSON["readiness"])
}

func TestEvaluator_OverdueUnansweredQuestion(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "Overdue session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "A", nil)
	require.NoError(t, err)

	q := &models.Question{
		ID:             uuid.New(),
		ArtifactID:     artifact.ID,
		SessionID:      session.ID,
		QuestionText:   "Stale question?",
		QuestionSource: models.QuestionSourceText,
	}
	require.NoError(t, db.CreateQuestion(ctx, q))

	_, err = db.Pool.Exec(ctx, `UPDATE questions SET created_at = now() - interval '72 hours' WHERE id = $1`, q.ID)
	require.NoError(t, err)

	ev := orchestration.NewEvaluator(db)
	recs, err := ev.EvaluateSession(ctx, session.ID)
	require.NoError(t, err)

	var unanswered *models.OrchestrationRecommendation
	for _, r := range recs {
		if r.RecommendationType == models.RecommendationTypeUnansweredQuestion {
			unanswered = r
			break
		}
	}
	require.NotNil(t, unanswered)
	assert.Contains(t, unanswered.Summary, "Overdue unanswered question")
	assert.Equal(t, "overdue_unanswered", unanswered.MetadataJSON["reason"])
	assert.Equal(t, true, unanswered.MetadataJSON["overdue"])
}

func TestEvaluator_TwoUnansweredQuestionsDistinctDedupeKeys(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "Dedupe session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "A", nil)
	require.NoError(t, err)

	for i := 0; i < 2; i++ {
		q := &models.Question{
			ID:             uuid.New(),
			ArtifactID:     artifact.ID,
			SessionID:      session.ID,
			QuestionText:   "Q?",
			QuestionSource: models.QuestionSourceText,
		}
		require.NoError(t, db.CreateQuestion(ctx, q))
	}

	ev := orchestration.NewEvaluator(db)
	recs, err := ev.EvaluateSession(ctx, session.ID)
	require.NoError(t, err)

	keys := make(map[string]struct{})
	var unanswered int
	for _, r := range recs {
		if r.RecommendationType != models.RecommendationTypeUnansweredQuestion {
			continue
		}
		unanswered++
		k := r.MetadataJSON["dedupe_key"].(string)
		require.NotEmpty(t, k)
		require.NotContains(t, keys, k, "duplicate dedupe_key for two questions")
		keys[k] = struct{}{}
	}
	assert.Equal(t, 2, unanswered, "one recommendation per unanswered root question")
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
	assert.Equal(t, "not_viewed", missing.MetadataJSON["engagement_level"])
	assert.Equal(t, "p1@example.com", missing.MetadataJSON["participant_email"])
	assert.Equal(t, "missing_participant_input:"+u.ID.String(), missing.MetadataJSON["dedupe_key"])
	assert.Equal(t, false, missing.MetadataJSON["has_material_view"])
	assert.Equal(t, false, missing.MetadataJSON["has_question_view"])
}

func TestEvaluator_MissingParticipant_ViewedNotResponded(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "Viewed session")

	pd := "Decide soon"
	require.NoError(t, db.UpdateSessionContext(ctx, session.ID, nil, &pd, nil))

	u := &models.User{
		ID:          uuid.New(),
		Email:       "engaged@example.com",
		DisplayName: "Engaged",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleParticipant,
	}
	require.NoError(t, db.CreateUser(ctx, u))
	require.NoError(t, db.CreateSessionMembership(ctx, session.ID, u.ID, "participant", nil))

	artifact, err := db.CreateArtifact(ctx, session.ID, "A", nil)
	require.NoError(t, err)
	mid := uuid.New()
	require.NoError(t, db.CreateMaterial(ctx, &models.Material{
		ID:          mid,
		ArtifactID:  artifact.ID,
		SessionID:   session.ID,
		Kind:        "document",
		Filename:    "doc.txt",
		ContentType: "text/plain",
		StorageURL:  "data/doc.txt",
		TextStatus:  models.MaterialTextStatusReady,
	}))
	require.NoError(t, db.MarkMaterialsSeenByParticipant(ctx, session.ID, strings.ToLower(strings.TrimSpace(u.Email)), []uuid.UUID{mid}))

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
	assert.Equal(t, "viewed_not_responded", missing.MetadataJSON["engagement_level"])
	assert.Equal(t, true, missing.MetadataJSON["has_material_view"])
	assert.Contains(t, missing.Summary, "has viewed session content")
}

func TestEvaluator_MissingParticipant_MarkDismissedInDB(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "Dismiss session")
	pd := "PD"
	require.NoError(t, db.UpdateSessionContext(ctx, session.ID, nil, &pd, nil))

	u := &models.User{
		ID:          uuid.New(),
		Email:       "dismiss@example.com",
		DisplayName: "D",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleParticipant,
	}
	require.NoError(t, db.CreateUser(ctx, u))
	require.NoError(t, db.CreateSessionMembership(ctx, session.ID, u.ID, "participant", nil))

	ev := orchestration.NewEvaluator(db)
	_, err := ev.SyncSessionRecommendations(ctx, session.ID)
	require.NoError(t, err)

	list, err := db.ListOrchestrationRecommendationsBySessionID(ctx, session.ID)
	require.NoError(t, err)
	var target uuid.UUID
	for _, r := range list {
		if r.RecommendationType == models.RecommendationTypeMissingParticipant {
			target = r.ID
			break
		}
	}
	require.NotEqual(t, uuid.Nil, target)

	require.NoError(t, db.UpdateOrchestrationRecommendationStatus(ctx, target, models.RecommendationStatusDismissed))
	list2, err := db.ListOrchestrationRecommendationsBySessionID(ctx, session.ID)
	require.NoError(t, err)
	var found *models.OrchestrationRecommendation
	for _, r := range list2 {
		if r.ID == target {
			found = r
			break
		}
	}
	require.NotNil(t, found)
	assert.Equal(t, models.RecommendationStatusDismissed, found.Status)
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
