package orchestration

import (
	"testing"

	"github.com/psuthar/talkback/internal/models"
)

func TestIsOrchestrationDraftAnswer(t *testing.T) {
	t.Parallel()
	m := DraftAnswerModel
	if !IsOrchestrationDraftAnswer(&models.Answer{Model: &m, Confirmed: false}) {
		t.Fatal("expected true for draft model")
	}
	if !IsOrchestrationDraftAnswer(&models.Answer{Model: &m, Confirmed: true}) {
		t.Fatal("expected true for draft model by marker regardless of confirmed flag")
	}
	manual := "manual"
	if IsOrchestrationDraftAnswer(&models.Answer{Model: &manual}) {
		t.Fatal("expected false for manual")
	}
	if IsOrchestrationDraftAnswer(nil) {
		t.Fatal("expected false for nil")
	}
}
