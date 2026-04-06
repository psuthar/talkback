// Package orchestration implements creator-facing session orchestration:
// evaluating session state and producing typed recommendations (human-in-the-loop; no autonomous actions).
// Unanswered/overdue question handling: see OverdueUnansweredAfter and buildUnansweredQuestionRecommendation.
// Missing participant input uses engagement from material_views / question_views (not_viewed vs viewed_not_responded).
// Persisted recommendations are exposed over HTTP for creators (list, sync, status updates with audit); see
// internal/handlers/session_orchestration.go and database.UpdateOrchestrationRecommendationStatusWithAudit.
// EvaluateSession returns in-memory recommendations; SyncSessionRecommendations replaces persisted rows for a session.
package orchestration

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
)

const maxQuestionsScan = 500

// OverdueUnansweredAfter is how long a root question may remain without an answer before it is flagged overdue.
// Exposed so callers/tests can adjust; future: env or session policy.
var OverdueUnansweredAfter = 48 * time.Hour

// Evaluator loads TalkBack session state and emits orchestration recommendations.
type Evaluator struct {
	DB *database.DB
	// Now overrides the clock for tests; nil uses time.Now.
	Now func() time.Time
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
	now := e.now()

	// 1) Root questions without an answer → unanswered_question (overdue + reason/context + dedupe_key; one rec per question)
	for _, q := range questions {
		if q.ParentQuestionID != nil {
			continue
		}
		ans, err := e.DB.GetLatestAnswerByQuestionID(ctx, q.ID)
		if err != nil {
			return nil, fmt.Errorf("orchestration: answer for question %s: %w", q.ID, err)
		}
		if ans == nil {
			rec := buildUnansweredQuestionRecommendation(sessionID, q, now)
			out = append(out, rec)
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

	// 3) Invited participants without a stance when primary decision is set → missing_participant_input (engagement + context)
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
			if st != nil {
				continue
			}
			u, err := e.DB.GetUserByID(ctx, m.UserID)
			if err != nil {
				return nil, fmt.Errorf("orchestration: get user %s: %w", m.UserID, err)
			}
			ref := participantRefForOrchestration(u, m.UserID)
			matView, err := e.DB.ParticipantHasAnyMaterialView(ctx, sessionID, ref)
			if err != nil {
				return nil, fmt.Errorf("orchestration: material views: %w", err)
			}
			qView, err := e.DB.ParticipantHasAnyQuestionView(ctx, sessionID, ref)
			if err != nil {
				return nil, fmt.Errorf("orchestration: question views: %w", err)
			}
			out = append(out, buildMissingParticipantRecommendation(sessionID, m.UserID, u, matView, qView))
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

func (e *Evaluator) now() time.Time {
	if e != nil && e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// buildUnansweredQuestionRecommendation emits a single recommendation per root question (no duplicate unanswered vs overdue rows).
func buildUnansweredQuestionRecommendation(sessionID uuid.UUID, q *models.Question, now time.Time) *models.OrchestrationRecommendation {
	dedupeKey := "unanswered_question:" + q.ID.String()
	age := now.Sub(q.CreatedAt)
	overdue := age >= OverdueUnansweredAfter
	ageStr := humanizeDuration(age)

	reason := "unanswered"
	if overdue {
		reason = "overdue_unanswered"
	}

	var summary string
	if overdue {
		summary = fmt.Sprintf(
			"Overdue unanswered question (open %s, threshold %s): %s",
			ageStr,
			humanizeDuration(OverdueUnansweredAfter),
			truncate(q.QuestionText, 100),
		)
	} else {
		summary = fmt.Sprintf("Unanswered question (open %s): %s", ageStr, truncate(q.QuestionText, 110))
	}
	action := "Review the question and add or generate an answer."
	if overdue {
		a := "Prioritize answering or explicitly defer this question so participants are not blocked."
		action = a
	}

	return &models.OrchestrationRecommendation{
		SessionID:          sessionID,
		RecommendationType: models.RecommendationTypeUnansweredQuestion,
		Status:             models.RecommendationStatusNew,
		Summary:            summary,
		SuggestedAction:    &action,
		Evidence: []models.RecommendationEvidenceRef{
			{SourceType: "question", QuestionID: &q.ID},
		},
		MetadataJSON: map[string]interface{}{
			"dedupe_key":                dedupeKey,
			"reason":                    reason,
			"question_age_seconds":      int64(age.Seconds()),
			"overdue":                   overdue,
			"overdue_threshold_seconds": int64(OverdueUnansweredAfter.Seconds()),
		},
	}
}

func participantRefForOrchestration(u *models.User, userID uuid.UUID) string {
	if u != nil {
		e := strings.TrimSpace(u.Email)
		if e != "" {
			return strings.ToLower(e)
		}
	}
	return userID.String()
}

func buildMissingParticipantRecommendation(
	sessionID uuid.UUID,
	userID uuid.UUID,
	u *models.User,
	matView, qView bool,
) *models.OrchestrationRecommendation {
	engaged := matView || qView
	engagementLevel := "not_viewed"
	if engaged {
		engagementLevel = "viewed_not_responded"
	}
	label := userID.String()
	email := ""
	display := ""
	if u != nil {
		email = u.Email
		display = u.DisplayName
		if strings.TrimSpace(u.Email) != "" {
			label = strings.TrimSpace(u.Email)
		} else if strings.TrimSpace(u.DisplayName) != "" {
			label = strings.TrimSpace(u.DisplayName)
		}
	}

	var summary string
	if engaged {
		summary = fmt.Sprintf("Participant (%s) has viewed session content but has not recorded a stance on the primary decision.", label)
	} else {
		summary = fmt.Sprintf("Participant (%s) has no material or Q&A views recorded yet and has not recorded a stance on the primary decision.", label)
	}
	action := "Follow up with the participant or record their stance when they respond."
	ref := participantRefForOrchestration(u, userID)
	return &models.OrchestrationRecommendation{
		SessionID:          sessionID,
		RecommendationType: models.RecommendationTypeMissingParticipant,
		Status:             models.RecommendationStatusNew,
		Summary:            summary,
		SuggestedAction:    &action,
		Evidence: []models.RecommendationEvidenceRef{
			{SourceType: "session_membership", SourceID: &userID},
		},
		MetadataJSON: map[string]interface{}{
			"dedupe_key":               "missing_participant_input:" + userID.String(),
			"engagement_level":         engagementLevel,
			"participant_email":        email,
			"participant_display_name": display,
			"user_id":                  userID.String(),
			"has_material_view":        matView,
			"has_question_view":        qView,
			"participant_ref":          ref,
		},
	}
}

func humanizeDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return "under 1m"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%.0fh", d.Hours())
	}
	return fmt.Sprintf("%.0fd", d.Hours()/24)
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
