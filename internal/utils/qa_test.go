package utils

import (
	"encoding/json"
	"testing"
)

func TestExtractFirstJSONObject(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   string
		valid  bool // whether want should parse as JSON
	}{
		{
			name:  "plain json only",
			input: `{"answer_status":"answered","answer_text":"Hi","confidence":0.9,"citations":[]}`,
			want:  `{"answer_status":"answered","answer_text":"Hi","confidence":0.9,"citations":[]}`,
			valid: true,
		},
		{
			name:  "json then trailing text starting with f",
			input: `{"answer_status":"answered","answer_text":"Yes.","confidence":1.0,"citations":[]} From the slides, the key points are...`,
			want:  `{"answer_status":"answered","answer_text":"Yes.","confidence":1.0,"citations":[]}`,
			valid: true,
		},
		{
			name:  "json with nested braces in string",
			input: `{"answer_text":"Use {curly} braces","answer_status":"answered","confidence":0.8,"citations":[]} extra`,
			want:  `{"answer_text":"Use {curly} braces","answer_status":"answered","confidence":0.8,"citations":[]}`,
			valid: true,
		},
		{
			name:  "no object",
			input: `no json here`,
			want:  "",
			valid: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFirstJSONObject(tt.input)
			if got != tt.want {
				t.Errorf("extractFirstJSONObject() = %q, want %q", got, tt.want)
			}
			if tt.valid && got != "" {
				var q QAResponse
				if err := json.Unmarshal([]byte(got), &q); err != nil {
					t.Errorf("extracted JSON should be valid: %v", err)
				}
		}
	})
	}
}

// TestAppendedTextAfterJSONIsHandled verifies that when the LLM returns valid JSON followed by
// extra text (e.g. "From the slides..." or "Note: ..."), we still parse the JSON correctly and
// do not fail with "invalid character 'f' after object key:value pair".
func TestAppendedTextAfterJSONIsHandled(t *testing.T) {
	// Simulates a real model response: valid QA JSON then trailing prose
	contentWithAppendedText := `{"answer_status":"answered","answer_text":"To make a message memorable, use clear structure and repetition.","confidence":0.85,"citations":[{"chunk_id":"c1","source_type":"material","source_id":"art-1","locator":"Slide 3","snippet":"Clear structure and repetition help."}]} From the slides, the key points are structure and repetition.`
	extracted := extractFirstJSONObject(contentWithAppendedText)
	if extracted == "" {
		t.Fatal("extractFirstJSONObject must return the JSON object when text is appended after it")
	}
	var q QAResponse
	if err := json.Unmarshal([]byte(extracted), &q); err != nil {
		t.Fatalf("extracted JSON must parse without error (appended text must be stripped): %v", err)
	}
	if q.AnswerStatus != "answered" || q.Confidence != 0.85 {
		t.Errorf("parsed response: answer_status=%q confidence=%f (expected answered, 0.85)", q.AnswerStatus, q.Confidence)
	}
	if len(q.Citations) != 1 || q.Citations[0].ChunkID != "c1" {
		t.Errorf("parsed response should have one citation with chunk_id c1, got %d citations", len(q.Citations))
	}
}
