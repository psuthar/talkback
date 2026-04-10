package mcpserver

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/require"
)

func TestBuildGetSessionDecisionsOutput_empty(t *testing.T) {
	t.Parallel()
	sid := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	s := &models.Session{
		ID:        sid,
		Title:     "t",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	out := buildGetSessionDecisionsOutput(s, nil)
	require.Equal(t, sessionDecisionsSchemaVersion, out.SchemaVersion)
	require.Equal(t, sid.String(), out.SessionID)
	require.Nil(t, out.Premise)
	require.Nil(t, out.PrimaryDecision)
	require.Nil(t, out.DecisionOutcome)
	require.NotNil(t, out.DecisionStances)
	require.Len(t, out.DecisionStances, 0)

	b, err := json.Marshal(out)
	require.NoError(t, err)
	require.Contains(t, string(b), `"decision_stances":[]`)
}

func TestBuildGetSessionDecisionsOutput_withStances(t *testing.T) {
	t.Parallel()
	sid := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	uid := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	r := "because"
	s := &models.Session{
		ID:              sid,
		Title:           "t",
		Premise:         strPtr("p"),
		PrimaryDecision: strPtr("decide"),
		DecisionOutcome: strPtr("done"),
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	sw := &models.DecisionStanceWithUser{
		DecisionStance: models.DecisionStance{
			ID:        uuid.New(),
			SessionID: sid,
			UserID:    uid,
			Stance:    models.StanceAgree,
			Rationale: &r,
			CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
			UpdatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		},
		UserEmail: "u@example.com",
	}
	out := buildGetSessionDecisionsOutput(s, []*models.DecisionStanceWithUser{sw})
	require.Equal(t, "p", *out.Premise)
	require.Equal(t, "decide", *out.PrimaryDecision)
	require.Equal(t, "done", *out.DecisionOutcome)
	require.Len(t, out.DecisionStances, 1)
	require.Equal(t, models.StanceAgree, out.DecisionStances[0].Stance)
	require.Equal(t, "u@example.com", out.DecisionStances[0].UserEmail)
}

func strPtr(s string) *string { return &s }
