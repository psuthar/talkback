package rag

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildMaterialChunksFromPDF_InvalidPathReturnsNil(t *testing.T) {
	sessionID := uuid.New()
	materialID := uuid.New()
	out := BuildMaterialChunksFromPDF(sessionID, materialID, "/nonexistent/path/to/file.pdf")
	assert.Nil(t, out)
}

func TestBuildMaterialChunksFromPDF_EmptyPathReturnsNil(t *testing.T) {
	sessionID := uuid.New()
	materialID := uuid.New()
	out := BuildMaterialChunksFromPDF(sessionID, materialID, "")
	assert.Nil(t, out)
}

func TestBuildSessionMetadataChunks_NilSessionReturnsNil(t *testing.T) {
	assert.Nil(t, BuildSessionMetadataChunks(nil, SessionMetadataCounts{}))
}

func TestBuildSessionMetadataChunks_RendersDecisionFieldsAndCounts(t *testing.T) {
	sessionID := uuid.New()
	premise := "We need to decide on the APAC expansion."
	decision := "Approve 2.4M budget for APAC."
	outcome := "Approved unanimously."
	s := &models.Session{
		ID:              sessionID,
		Title:           "APAC budget review",
		Status:          models.SessionStatus("open"),
		Premise:         &premise,
		PrimaryDecision: &decision,
		DecisionOutcome: &outcome,
		CreatedAt:       time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC),
		UpdatedAt:       time.Date(2026, 5, 22, 14, 0, 0, 0, time.UTC),
	}
	counts := SessionMetadataCounts{
		Participants: 4,
		Materials:    3,
		MaterialsByKind: map[string]int{
			"text": 1,
			"pdf":  2,
		},
		Recordings:       2,
		Questions:        5,
		Links:            1,
		TranscriptStatus: "ready",
		Stances: &models.StanceAggregate{
			Agree:        3,
			Disagree:     0,
			Conditional:  1,
			Abstain:      0,
			NeedMoreInfo: 0,
			Total:        4,
		},
	}
	chunks := BuildSessionMetadataChunks(s, counts)
	require.Len(t, chunks, 1)
	c := chunks[0]
	assert.Equal(t, "session_metadata", c.SourceType)
	require.NotNil(t, c.SourceID)
	assert.Equal(t, sessionID, *c.SourceID)
	assert.Equal(t, 0, c.ChunkIdx)
	assert.Equal(t, "session_metadata", c.AnchorJSON["type"])
	text := c.Text
	assert.Contains(t, text, "APAC budget review")
	assert.Contains(t, text, "Status: open")
	assert.Contains(t, text, premise)
	assert.Contains(t, text, decision)
	assert.Contains(t, text, outcome)
	assert.Contains(t, text, "Participants: 4")
	assert.Contains(t, text, "Materials: 3 total")
	assert.Contains(t, text, "1 text")
	assert.Contains(t, text, "2 pdf")
	assert.Contains(t, text, "Video recordings: 2")
	assert.Contains(t, text, "Questions asked: 5")
	assert.Contains(t, text, "External links: 1")
	assert.Contains(t, text, "Decision stances: 3 agree")
	assert.Contains(t, text, "Transcript: ready")
}

func TestBuildSessionMetadataChunks_OmitsZeroValuedAggregates(t *testing.T) {
	s := &models.Session{
		ID:        uuid.New(),
		Title:     "Empty session",
		Status:    models.SessionStatus("open"),
		CreatedAt: time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC),
	}
	chunks := BuildSessionMetadataChunks(s, SessionMetadataCounts{
		MaterialsByKind:  map[string]int{},
		TranscriptStatus: "none",
	})
	require.Len(t, chunks, 1)
	text := chunks[0].Text
	assert.Contains(t, text, "Empty session")
	assert.Contains(t, text, "Materials: none.")
	assert.Contains(t, text, "Video recordings: none.")
	assert.NotContains(t, text, "Participants:")
	assert.NotContains(t, text, "Questions asked:")
	assert.NotContains(t, text, "External links:")
	assert.NotContains(t, text, "Decision stances:")
	assert.NotContains(t, text, "Transcript:")
	assert.NotContains(t, text, "Premise:")
	assert.NotContains(t, text, "Primary decision:")
}

func TestBuildSessionMetadataChunks_ContentHashChangesOnEdit(t *testing.T) {
	sessionID := uuid.New()
	base := &models.Session{
		ID:        sessionID,
		Title:     "S1",
		Status:    models.SessionStatus("open"),
		CreatedAt: time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC),
	}
	premise := "p"
	updated := *base
	updated.Premise = &premise
	a := BuildSessionMetadataChunks(base, SessionMetadataCounts{})
	b := BuildSessionMetadataChunks(&updated, SessionMetadataCounts{})
	require.Len(t, a, 1)
	require.Len(t, b, 1)
	assert.NotEqual(t, a[0].ContentHash, b[0].ContentHash, "hash must change when metadata mutates")
	assert.True(t, strings.Contains(b[0].Text, "Premise: p"))
}
