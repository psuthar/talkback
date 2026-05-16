package rag

import (
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
)

// TestBuildCanonicalMapFromAliases pins the SCRUM-425 precedence rule:
// recording-scoped alias rows outrank session-wide rows for the same
// source_label. Pure unit — no DB, no rag.BuildSpeakerCanonicalMap
// wrapper.
func TestBuildCanonicalMapFromAliases(t *testing.T) {
	t.Parallel()
	alice := uuid.New()
	bob := uuid.New()
	recID := uuid.New()
	email := "alice@example.com"

	rows := []models.SessionSpeakerAlias{
		{SourceLabel: "Speaker 0", CanonicalPersonID: alice, CanonicalDisplayName: "Alice Liddell"},
		{SourceLabel: "alice@example.com", CanonicalPersonID: alice, CanonicalDisplayName: "Alice Liddell", CanonicalEmail: &email},
		{SourceLabel: "Speaker 1", CanonicalPersonID: bob, CanonicalDisplayName: "Bob (session-wide)"},
		{SourceLabel: "Speaker 1", CanonicalPersonID: bob, CanonicalDisplayName: "Bob (recording-scoped)", SourceRecordingID: &recID},
	}

	got := buildCanonicalMapFromAliases(rows)

	t.Run("session-wide row populates the map", func(t *testing.T) {
		c, ok := got["Speaker 0"]
		require := assert.New(t)
		require.True(ok)
		require.Equal("Alice Liddell", c.DisplayName)
		require.Equal(alice, c.CanonicalPersonID)
	})

	t.Run("email is preserved from the row that carries it", func(t *testing.T) {
		c, ok := got["alice@example.com"]
		require := assert.New(t)
		require.True(ok)
		require.NotNil(c.Email)
		require.Equal(email, *c.Email)
	})

	t.Run("recording-scoped row wins over session-wide for the same label", func(t *testing.T) {
		c, ok := got["Speaker 1"]
		require := assert.New(t)
		require.True(ok)
		require.Equal("Bob (recording-scoped)", c.DisplayName,
			"recording-scoped row must outrank the session-wide row")
	})
}

// TestResolveLabel is a pure-Go unit test for the in-memory resolution
// helpers — no DB required. The BuildSpeakerCanonicalMap DB integration is
// covered by internal/database/speaker_aliases_test.go's existing
// ResolveCanonical sub-tests and by the SCRUM-424 handler tests.
func TestResolveLabel(t *testing.T) {
	t.Parallel()
	alice := uuid.New()
	bob := uuid.New()
	canonicalMap := map[string]SessionSpeakerCanonical{
		"Speaker 0":         {SourceLabel: "Speaker 0", CanonicalPersonID: alice, DisplayName: "Alice"},
		"alice@example.com": {SourceLabel: "alice@example.com", CanonicalPersonID: alice, DisplayName: "Alice"},
		"Speaker 1":         {SourceLabel: "Speaker 1", CanonicalPersonID: bob, DisplayName: "Bob"},
	}

	t.Run("known label resolves to display name", func(t *testing.T) {
		assert.Equal(t, "Alice", ResolveLabel(canonicalMap, "Speaker 0"))
		assert.Equal(t, "Bob", ResolveLabel(canonicalMap, "Speaker 1"))
	})

	t.Run("unknown label falls through to the raw value", func(t *testing.T) {
		assert.Equal(t, "Speaker 9", ResolveLabel(canonicalMap, "Speaker 9"))
	})

	t.Run("nil map is safe", func(t *testing.T) {
		assert.Equal(t, "Speaker 0", ResolveLabel(nil, "Speaker 0"))
	})
}

func TestLabelsForCanonical(t *testing.T) {
	t.Parallel()
	alice := uuid.New()
	bob := uuid.New()
	canonicalMap := map[string]SessionSpeakerCanonical{
		"Speaker 0":         {SourceLabel: "Speaker 0", CanonicalPersonID: alice, DisplayName: "Alice"},
		"alice@example.com": {SourceLabel: "alice@example.com", CanonicalPersonID: alice, DisplayName: "Alice"},
		"Speaker 1":         {SourceLabel: "Speaker 1", CanonicalPersonID: bob, DisplayName: "Bob"},
	}

	t.Run("returns every label mapping to the canonical (filter expansion)", func(t *testing.T) {
		got := LabelsForCanonical(canonicalMap, alice)
		assert.ElementsMatch(t, []string{"Speaker 0", "alice@example.com"}, got)
	})

	t.Run("single-label canonical returns that one label", func(t *testing.T) {
		got := LabelsForCanonical(canonicalMap, bob)
		assert.Equal(t, []string{"Speaker 1"}, got)
	})

	t.Run("unknown canonical returns nil", func(t *testing.T) {
		assert.Nil(t, LabelsForCanonical(canonicalMap, uuid.New()))
	})

	t.Run("nil map is safe", func(t *testing.T) {
		assert.Nil(t, LabelsForCanonical(nil, alice))
	})
}
