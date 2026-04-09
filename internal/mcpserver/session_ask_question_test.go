package mcpserver

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
)

func TestMcpBuildAskOutput_AutomationRecommended(t *testing.T) {
	sid := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	ts := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	q := &models.Question{
		ID:        uuid.MustParse("10000000-0000-4000-8000-000000000001"),
		CreatedAt: ts,
	}
	baseA := func() *models.Answer {
		return &models.Answer{
			ID:        uuid.MustParse("20000000-0000-4000-8000-000000000001"),
			CreatedAt: ts,
			AnswerText: "ok",
		}
	}

	tests := []struct {
		name     string
		status   models.AnswerStatus
		conf     float32
		wantAuto bool
	}{
		{"answered_high", models.AnswerStatusAnswered, 0.9, true},
		{"answered_at_threshold", models.AnswerStatusAnswered, 0.55, true},
		{"answered_below_threshold", models.AnswerStatusAnswered, 0.54, false},
		{"not_covered", models.AnswerStatusNotCovered, 0.9, false},
		{"error", models.AnswerStatusError, 0.9, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := baseA()
			a.AnswerStatus = tt.status
			a.Confidence = tt.conf
			out := mcpBuildAskOutput(sid, q, a)
			if out.AutomationRecommended != tt.wantAuto {
				t.Fatalf("AutomationRecommended=%v want %v (status=%s conf=%v)",
					out.AutomationRecommended, tt.wantAuto, tt.status, tt.conf)
			}
		})
	}
}
