package utils

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExtractFirstJSONObject(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
		valid bool // whether want should parse as JSON
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

// TestQAResponseToleratesNumericChunkID is a regression for SCRUM-359: when the LLM
// emits chunk_id (or source_id) as a JSON number, the QA pipeline must still parse
// the response into a usable QAResponse rather than zeroing out the answer with
// "cannot unmarshal number into Go struct field Citation.citations.chunk_id of type string".
func TestQAResponseToleratesNumericChunkID(t *testing.T) {
	raw := `{"answer_status":"answered","answer_text":"The committee recommends X.","confidence":0.85,"citations":[` +
		`{"chunk_id":42,"source_type":"material","source_id":"art-1","snippet":"recommendation X is preferred"},` +
		`{"chunk_id":"c2","source_type":"transcript","source_id":7,"snippet":"the committee agreed"}` +
		`]}`
	var q QAResponse
	if err := json.Unmarshal([]byte(raw), &q); err != nil {
		t.Fatalf("expected lenient parse to succeed, got: %v", err)
	}
	if q.AnswerStatus != "answered" {
		t.Errorf("AnswerStatus: got %q, want %q", q.AnswerStatus, "answered")
	}
	if len(q.Citations) != 2 {
		t.Fatalf("expected 2 citations, got %d", len(q.Citations))
	}
	if q.Citations[0].ChunkID != "42" {
		t.Errorf("citation[0].ChunkID: got %q, want %q", q.Citations[0].ChunkID, "42")
	}
	if q.Citations[0].SourceID != "art-1" {
		t.Errorf("citation[0].SourceID: got %q, want %q", q.Citations[0].SourceID, "art-1")
	}
	if q.Citations[1].ChunkID != "c2" {
		t.Errorf("citation[1].ChunkID: got %q, want %q", q.Citations[1].ChunkID, "c2")
	}
	if q.Citations[1].SourceID != "7" {
		t.Errorf("citation[1].SourceID: got %q, want %q", q.Citations[1].SourceID, "7")
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

// TestNormalizeQAResponse_InvalidAnswerStatusPreservesOriginal is a regression
// for SCRUM-575: when the LLM returns an answer_status outside the
// {"answered", "not_covered", "error"} enum, the resulting AnswerText must
// quote the *original* LLM-returned value, not the "error" string the
// validator just assigned. The prior implementation formatted qa.AnswerStatus
// after overwriting it, so every invalid status was misreported as
// "Invalid answer_status: error".
func TestNormalizeQAResponse_InvalidAnswerStatusPreservesOriginal(t *testing.T) {
	tests := []struct {
		name           string
		inputStatus    string
		wantAnswerText string
	}{
		{
			name:           "common LLM colloquial: no",
			inputStatus:    "no",
			wantAnswerText: `Invalid answer_status: "no"`,
		},
		{
			name:           "capitalised colloquial: Negative",
			inputStatus:    "Negative",
			wantAnswerText: `Invalid answer_status: "Negative"`,
		},
		{
			name:           "empty string surfaces visibly via %q",
			inputStatus:    "",
			wantAnswerText: `Invalid answer_status: ""`,
		},
		{
			name:           "whitespace-only surfaces visibly via %q",
			inputStatus:    " ",
			wantAnswerText: `Invalid answer_status: " "`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qa := &QAResponse{
				AnswerStatus: tt.inputStatus,
				AnswerText:   "irrelevant — should be overwritten",
				Confidence:   0.9,
			}
			normalizeQAResponse(qa)
			if qa.AnswerStatus != "error" {
				t.Errorf("AnswerStatus: got %q, want %q", qa.AnswerStatus, "error")
			}
			// After the SCRUM-575 fix the AnswerText must quote the
			// *original* LLM value, not the post-assignment "error" value.
			// Note: low-confidence path is not triggered here because
			// Confidence=0.9 keeps us above the 0.55 threshold; status
			// gets normalised to "error" rather than "not_covered".
			if qa.AnswerText != tt.wantAnswerText {
				t.Errorf("AnswerText:\n  got:  %q\n  want: %q", qa.AnswerText, tt.wantAnswerText)
			}
		})
	}
}

// TestNormalizeQAResponse_ValidStatusPreservesAnswerText confirms the fix
// did not regress the happy path: when the LLM returns a valid status,
// normalizeQAResponse must not touch AnswerText via the invalid-status
// branch.
func TestNormalizeQAResponse_ValidStatusPreservesAnswerText(t *testing.T) {
	for _, status := range []string{"answered", "error"} {
		t.Run(status, func(t *testing.T) {
			qa := &QAResponse{
				AnswerStatus: status,
				AnswerText:   "original answer text",
				Confidence:   0.9,
			}
			normalizeQAResponse(qa)
			if qa.AnswerStatus != status {
				t.Errorf("AnswerStatus: got %q, want %q", qa.AnswerStatus, status)
			}
			if qa.AnswerText != "original answer text" {
				t.Errorf("AnswerText should be untouched for valid status %q, got %q", status, qa.AnswerText)
			}
		})
	}
}

func TestBuildIdentityContextSection_ParticipantAsker(t *testing.T) {
	asker := "invitee@example.com"
	role := "participant"
	creator := "creator@example.com"
	section := buildIdentityContextSection(SessionContext{
		AskerEmail:          &asker,
		AskerRole:           &role,
		SessionCreatorEmail: &creator,
	})
	if section == "" {
		t.Fatal("expected identity context section to be present")
	}
	if !containsAll(section,
		"ASKER IDENTITY CONTEXT",
		"Current asker email: invitee@example.com",
		"Current asker role in session: participant",
		"Session creator email: creator@example.com",
		"Do not describe the asker as the creator unless the asker identity explicitly matches the session creator email.",
	) {
		t.Fatalf("identity section missing expected participant guidance:\n%s", section)
	}
}

func TestBuildIdentityContextSection_EmptyWhenNoIdentityFields(t *testing.T) {
	section := buildIdentityContextSection(SessionContext{})
	if section != "" {
		t.Fatalf("expected empty identity section, got: %q", section)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}
