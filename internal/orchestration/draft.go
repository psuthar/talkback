package orchestration

import (
	"strings"

	"github.com/psuthar/talkback/internal/models"
)

// DraftAnswerModel is stored on answers.model for AI drafts pending creator approval (SCRUM-12).
const DraftAnswerModel = "orchestration-draft"

// IsOrchestrationDraftAnswer reports whether the answer row is an orchestration-generated draft
// (same marker used by handlers; evaluator also treats other non-manual AI models as drafts).
func IsOrchestrationDraftAnswer(a *models.Answer) bool {
	if a == nil || a.Model == nil {
		return false
	}
	return strings.TrimSpace(*a.Model) == DraftAnswerModel
}
