// Package orchestration implements creator-facing session orchestration (SCRUM-7/9):
// evaluating session state and producing typed recommendations (human-in-the-loop; no autonomous actions).
package orchestration

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
)

const maxQuestionsScan = 500

// Evaluator loads TalkBack session state and emits orchestration recommendations.
type Evaluator struct {
	DB *database.DB
}

// NewEvaluator returns an evaluator backed by the given DB.
func NewEvaluator(db *database.DB) *Evaluator {
	if db == nil {
		return nil
	}
	return &Evaluator{DB: db}
}

// EvaluateSession inspects the current session and returns recommendations (in-memory; not persisted).
// Ordering is deterministic: unanswered questions, draft answers, missing participant input, decision readiness.
func (e *Evaluator) EvaluateSession(ctx context.Context, sessionID uuid.UUID) ([]*models.OrchestrationRecommendation, error) {
	if e == nil || e.DB == nil {
		return nil, fmt.Errorf("orchestration: nil evaluator or DB")
	}

	session, err := e.DB.GetSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("orchestration: get session: %w", err)
	}

	questions, _, err := e.DB.GetQuestionsBySessionID(ctx, sessionID, maxQuestionsScan)
	if err != nil {
		return nil, fmt.Errorf("orchestration: list questions: %w", err)
	}

	var out []*models.OrchestrationRecommendation

	// 1) Root questions without an answer → unanswered_question
	for _, q := range questions {
		if q.ParentQuestionID != nil {
			continue
		}
		ans, err := e.DB.GetLatestAnswerByQuestionID(ctx, q.ID)
		if err != nil {
			return nil, fmt.Errorf("orchestration: answer for question %s: %w", q.ID, err)
		}
		if ans == nil {
			summary := fmt.Sprintf("Unanswered question: %s", truncate(q.QuestionText, 120))
			action := "Review the question and add or generate an answer."
			out = append(out, &models.OrchestrationRecommendation{
				SessionID:          sessionID,
				RecommendationType: models.RecommendationTypeUnansweredQuestion,
				Status:             models.RecommendationStatusNew,
				Summary:            summary,
				SuggestedAction:    &action,
				Evidence: []models.RecommendationEvidenceRef{
					{SourceType: "question", QuestionID: &q.ID},
				},
			})
			continue
		}
		// 2) AI-generated answer not confirmed → review_draft_answer
		if isAIDraftPendingReview(ans) {
			summary := fmt.Sprintf("Draft answer awaiting review (question: %s)", truncate(q.QuestionText, 80))
			action := "Review, approve, or dismiss the generated answer before it is treated as final."
			ev := []models.RecommendationEvidenceRef{
				{SourceType: "question", QuestionID: &q.ID},
				{SourceType: "answer", AnswerID: &ans.ID, QuestionID: &q.ID},
			}
			out = append(out, &models.OrchestrationRecommendation{
				SessionID:          sessionID,
				RecommendationType: models.RecommendationTypeReviewDraftAnswer,
				Status:             models.RecommendationStatusNew,
				Summary:            summary,
				SuggestedAction:    &action,
				Evidence:           ev,
			})
		}
	}

	// 3) Invited participants without a stance when primary decision is set → missing_participant_input
	pd := ""
	if session.PrimaryDecision != nil {
		pd = strings.TrimSpace(*session.PrimaryDecision)
	}
	if pd != "" {
		members, err := e.DB.GetSessionMemberships(ctx, sessionID)
		if err != nil {
			return nil, fmt.Errorf("orchestration: memberships: %w", err)
		}
		for _, m := range members {
			if strings.ToLower(strings.TrimSpace(m.Role)) != "participant" {
				continue
			}
			st, err := e.DB.GetStanceByUserAndSession(ctx, sessionID, m.UserID)
			if err != nil {
				return nil, fmt.Errorf("orchestration: stance %s: %w", m.UserID, err)
			}
			if st == nil {
				summary := "Participant has not recorded a stance on the primary decision."
				action := "Invite follow-up or record input so the decision can proceed."
				uid := m.UserID
				out = append(out, &models.OrchestrationRecommendation{
					SessionID:          sessionID,
					RecommendationType: models.RecommendationTypeMissingParticipant,
					Status:             models.RecommendationStatusNew,
					Summary:            summary,
					SuggestedAction:    &action,
					Evidence: []models.RecommendationEvidenceRef{
						{SourceType: "session_membership", SourceID: &uid},
					},
					MetadataJSON: map[string]interface{}{
						"user_id": uid.String(),
					},
				})
			}
		}
	}

	// 4) Decision readiness (basic): premise + primary decision + index + stances
	premiseOK := session.Premise != nil && strings.TrimSpace(*session.Premise) != ""
	pdOK := pd != ""
	indexReady := strings.EqualFold(strings.TrimSpace(session.IndexStatus), "ready")

	if !premiseOK || !pdOK {
		summary := "Premise and primary decision should both be set for decision tracking."
		action := "Add or update premise and primary decision in session context."
		out = append(out, &models.OrchestrationRecommendation{
			SessionID:          sessionID,
			RecommendationType: models.RecommendationTypeDecisionReadiness,
			Status:             models.RecommendationStatusNew,
			Summary:            summary,
			SuggestedAction:    &action,
			MetadataJSON: map[string]interface{}{
				"readiness": "inputs_incomplete",
			},
		})
	} else if indexReady {
		agg, err := e.DB.GetStanceAggregate(ctx, sessionID)
		if err != nil {
			return nil, fmt.Errorf("orchestration: stance aggregate: %w", err)
		}
		if agg != nil && agg.Total > 0 {
			summary := "Core decision inputs are present and participant stances exist; review before finalizing."
			action := "Review stances and decision outcome before closing the session."
			out = append(out, &models.OrchestrationRecommendation{
				SessionID:          sessionID,
				RecommendationType: models.RecommendationTypeDecisionReadiness,
				Status:             models.RecommendationStatusNew,
				Summary:            summary,
				SuggestedAction:    &action,
				MetadataJSON: map[string]interface{}{
					"readiness": "inputs_complete",
				},
			})
		}
	}

	return out, nil
}

// SyncSessionRecommendations replaces persisted recommendations for the session with a fresh evaluation.
func (e *Evaluator) SyncSessionRecommendations(ctx context.Context, sessionID uuid.UUID) ([]*models.OrchestrationRecommendation, error) {
	recs, err := e.EvaluateSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if err := e.DB.DeleteOrchestrationRecommendationsBySessionID(ctx, sessionID); err != nil {
		return nil, err
	}
	for _, r := range recs {
		r.SessionID = sessionID
		if err := e.DB.CreateOrchestrationRecommendation(ctx, r); err != nil {
			return nil, err
		}
	}
	return recs, nil
}

func isAIDraftPendingReview(a *models.Answer) bool {
	if a == nil || a.Confirmed {
		return false
	}
	if a.Model == nil {
		return false
	}
	m := strings.TrimSpace(*a.Model)
	if m == "" || strings.EqualFold(m, "manual") {
		return false
	}
	return true
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return "..."
	}
	return s[:max-3] + "..."
}
