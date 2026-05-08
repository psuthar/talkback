package models

import (
	"encoding/json"
	"testing"
)

// SCRUM-359: LLM occasionally emits chunk_id and source_id as JSON numbers instead
// of strings. The default decoder rejects this against the declared string fields
// and surfaces "cannot unmarshal number into Go struct field" to the user as the
// answer body. Citation.UnmarshalJSON must coerce both shapes to string.
func TestCitationUnmarshalCoercesNumericIdentifiers(t *testing.T) {
	cases := []struct {
		name           string
		raw            string
		wantChunkID    string
		wantSourceID   string
		wantSourceType string
	}{
		{
			name:           "string chunk_id and source_id (normal path)",
			raw:            `{"chunk_id":"c1","source_type":"material","source_id":"art-1"}`,
			wantChunkID:    "c1",
			wantSourceID:   "art-1",
			wantSourceType: "material",
		},
		{
			name:           "numeric chunk_id",
			raw:            `{"chunk_id":42,"source_type":"material","source_id":"art-1"}`,
			wantChunkID:    "42",
			wantSourceID:   "art-1",
			wantSourceType: "material",
		},
		{
			name:           "numeric source_id",
			raw:            `{"chunk_id":"c1","source_type":"transcript","source_id":7}`,
			wantChunkID:    "c1",
			wantSourceID:   "7",
			wantSourceType: "transcript",
		},
		{
			name:           "both numeric",
			raw:            `{"chunk_id":42,"source_type":"transcript","source_id":7}`,
			wantChunkID:    "42",
			wantSourceID:   "7",
			wantSourceType: "transcript",
		},
		{
			name:           "large numeric chunk_id preserved as string",
			raw:            `{"chunk_id":12345678901234567890,"source_type":"material","source_id":"art-1"}`,
			wantChunkID:    "12345678901234567890",
			wantSourceID:   "art-1",
			wantSourceType: "material",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var c Citation
			if err := json.Unmarshal([]byte(tc.raw), &c); err != nil {
				t.Fatalf("unmarshal failed: %v", err)
			}
			if c.ChunkID != tc.wantChunkID {
				t.Errorf("ChunkID: got %q, want %q", c.ChunkID, tc.wantChunkID)
			}
			if c.SourceID != tc.wantSourceID {
				t.Errorf("SourceID: got %q, want %q", c.SourceID, tc.wantSourceID)
			}
			if c.SourceType != tc.wantSourceType {
				t.Errorf("SourceType: got %q, want %q", c.SourceType, tc.wantSourceType)
			}
		})
	}
}

// Verify that other Citation fields (snippet, label, anchor pointer, etc.) round-trip
// through the custom unmarshaler — the alias-embedding pattern must not silently drop
// fields that we do not coerce explicitly.
func TestCitationUnmarshalPreservesOtherFields(t *testing.T) {
	raw := `{
		"citation_id":"C1",
		"chunk_id":42,
		"source_type":"material",
		"source_id":"art-1",
		"locator":"Slide 3",
		"snippet":"Clear structure helps.",
		"label":"Document p. 4",
		"excerpt":"first 200 chars"
	}`
	var c Citation
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if c.CitationID != "C1" {
		t.Errorf("CitationID: got %q, want %q", c.CitationID, "C1")
	}
	if c.ChunkID != "42" {
		t.Errorf("ChunkID: got %q, want %q", c.ChunkID, "42")
	}
	if c.Locator != "Slide 3" {
		t.Errorf("Locator: got %q, want %q", c.Locator, "Slide 3")
	}
	if c.Snippet != "Clear structure helps." {
		t.Errorf("Snippet: got %q, want %q", c.Snippet, "Clear structure helps.")
	}
	if c.Label != "Document p. 4" {
		t.Errorf("Label: got %q, want %q", c.Label, "Document p. 4")
	}
	if c.Excerpt != "first 200 chars" {
		t.Errorf("Excerpt: got %q, want %q", c.Excerpt, "first 200 chars")
	}
}

// Null and missing fields must leave string identifiers at their zero value rather
// than panicking or treating "null" as the literal string "null".
func TestCitationUnmarshalNullAndMissing(t *testing.T) {
	t.Run("null chunk_id leaves field empty", func(t *testing.T) {
		var c Citation
		if err := json.Unmarshal([]byte(`{"chunk_id":null,"source_type":"material","source_id":"art-1"}`), &c); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}
		if c.ChunkID != "" {
			t.Errorf("ChunkID for null: got %q, want empty string", c.ChunkID)
		}
		if c.SourceID != "art-1" {
			t.Errorf("SourceID: got %q, want %q", c.SourceID, "art-1")
		}
	})
	t.Run("missing chunk_id leaves field empty", func(t *testing.T) {
		var c Citation
		if err := json.Unmarshal([]byte(`{"source_type":"material","source_id":"art-1"}`), &c); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}
		if c.ChunkID != "" {
			t.Errorf("ChunkID for missing: got %q, want empty string", c.ChunkID)
		}
	})
}
