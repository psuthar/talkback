package utils

import (
	"testing"

	"github.com/psuthar/talkback/internal/models"
)

// SCRUM-565 (Slice 4a of SCRUM-560): unit tests for the citation-
// enforcement helpers in qa.go. The full retry-then-refuse path uses
// `callOpenAIForQA` which is exercised end-to-end by the qa-eval
// harness on workflow_dispatch (not on PR CI); these tests cover the
// pure-logic surfaces.

func TestExtractCitationChunkIDs_HappyPath(t *testing.T) {
	qa := &QAResponse{
		Citations: []models.Citation{
			{ChunkID: "chunk-A"},
			{ChunkID: "chunk-B"},
		},
	}
	got := extractCitationChunkIDs(qa)
	if len(got) != 2 || got[0] != "chunk-A" || got[1] != "chunk-B" {
		t.Fatalf("got %v, want [chunk-A chunk-B]", got)
	}
}

func TestExtractCitationChunkIDs_DropsUnknownPlaceholders(t *testing.T) {
	// normalizeQAResponse fills missing chunk_id with "unknown_N" — that
	// placeholder must NOT count as a real citation when CheckCitations
	// looks for grounded references. extractCitationChunkIDs drops them
	// so the guardrail sees the "0 valid citations" state.
	qa := &QAResponse{
		Citations: []models.Citation{
			{ChunkID: "chunk-A"},
			{ChunkID: "unknown_0"},
			{ChunkID: "unknown_42"},
		},
	}
	got := extractCitationChunkIDs(qa)
	if len(got) != 1 || got[0] != "chunk-A" {
		t.Errorf("got %v, want [chunk-A] (unknown_* placeholders should be dropped)", got)
	}
}

func TestExtractCitationChunkIDs_DropsEmptyIDs(t *testing.T) {
	qa := &QAResponse{
		Citations: []models.Citation{
			{ChunkID: ""},
			{ChunkID: "chunk-A"},
			{ChunkID: ""},
		},
	}
	got := extractCitationChunkIDs(qa)
	if len(got) != 1 || got[0] != "chunk-A" {
		t.Errorf("got %v, want [chunk-A]", got)
	}
}

func TestExtractCitationChunkIDs_EmptyResponseReturnsEmptySlice(t *testing.T) {
	got := extractCitationChunkIDs(&QAResponse{})
	if got == nil {
		t.Errorf("expected non-nil empty slice, got nil — callers compare len(...) not nil-ness")
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}

func TestExtractRetrievedChunkIDs_DropsEmpty(t *testing.T) {
	chunks := []Chunk{
		{ChunkID: "chunk-A"},
		{ChunkID: ""},
		{ChunkID: "chunk-B"},
	}
	got := extractRetrievedChunkIDs(chunks)
	if len(got) != 2 || got[0] != "chunk-A" || got[1] != "chunk-B" {
		t.Errorf("got %v, want [chunk-A chunk-B]", got)
	}
}

func TestNormalizeQAResponse_ClampsConfidenceAndForcesNotCovered(t *testing.T) {
	qa := &QAResponse{
		AnswerStatus: "answered",
		AnswerText:   "Something",
		Confidence:   0.3, // below 0.55 floor
		Citations: []models.Citation{
			{ChunkID: "chunk-A"},
		},
	}
	normalizeQAResponse(qa)
	if qa.AnswerStatus != "not_covered" {
		t.Errorf("low confidence should force not_covered, got %s", qa.AnswerStatus)
	}
	if len(qa.Citations) != 0 {
		t.Errorf("not_covered should clear citations, got %v", qa.Citations)
	}
}

func TestNormalizeQAResponse_FillsMissingChunkID(t *testing.T) {
	qa := &QAResponse{
		AnswerStatus: "answered",
		Confidence:   0.9,
		Citations: []models.Citation{
			{ChunkID: ""},
			{ChunkID: "chunk-A"},
		},
	}
	normalizeQAResponse(qa)
	if qa.Citations[0].ChunkID != "unknown_0" {
		t.Errorf("missing chunk_id should be filled with unknown_0, got %q", qa.Citations[0].ChunkID)
	}
	if qa.Citations[1].ChunkID != "chunk-A" {
		t.Errorf("present chunk_id should be left alone, got %q", qa.Citations[1].ChunkID)
	}
}

func TestNormalizeQAResponse_CapsCitationsAndTruncatesSnippets(t *testing.T) {
	long := make([]byte, 500)
	for i := range long {
		long[i] = 'x'
	}
	qa := &QAResponse{
		AnswerStatus: "answered",
		Confidence:   0.9,
	}
	for i := 0; i < 8; i++ {
		qa.Citations = append(qa.Citations, models.Citation{
			ChunkID: "chunk",
			Snippet: string(long),
		})
	}
	normalizeQAResponse(qa)
	if len(qa.Citations) != 5 {
		t.Errorf("expected citation count capped to 5, got %d", len(qa.Citations))
	}
	for i, c := range qa.Citations {
		// 300 chars + "..." ellipsis
		if len(c.Snippet) > 305 {
			t.Errorf("citation %d snippet too long: %d chars", i, len(c.Snippet))
		}
	}
}
