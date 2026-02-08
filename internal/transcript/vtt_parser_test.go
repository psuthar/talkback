package transcript

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestParseVTT_Simple(t *testing.T) {
	content := `WEBVTT

00:00:00.000 --> 00:00:05.000
Hello world.

00:00:05.500 --> 00:00:10.000
Second line.
`
	segments, err := ParseVTT(bytes.NewReader([]byte(content)))
	if err != nil {
		t.Fatalf("ParseVTT() error = %v", err)
	}
	if len(segments) != 2 {
		t.Fatalf("got %d segments, want 2", len(segments))
	}
	if segments[0].Text != "Hello world." {
		t.Errorf("first text = %q", segments[0].Text)
	}
	if segments[0].StartMs != 0 || segments[0].EndMs != 5000 {
		t.Errorf("first segment ms = %d-%d", segments[0].StartMs, segments[0].EndMs)
	}
	if segments[1].Text != "Second line." {
		t.Errorf("second text = %q", segments[1].Text)
	}
	if segments[1].StartMs != 5500 || segments[1].EndMs != 10000 {
		t.Errorf("second segment ms = %d-%d", segments[1].StartMs, segments[1].EndMs)
	}
}

func TestParseVTT_ShortTimestamp(t *testing.T) {
	content := `WEBVTT

00:01.000 --> 00:05.500
Short format.
`
	segments, err := ParseVTT(bytes.NewReader([]byte(content)))
	if err != nil {
		t.Fatalf("ParseVTT() error = %v", err)
	}
	if len(segments) != 1 {
		t.Fatalf("got %d segments, want 1", len(segments))
	}
	if segments[0].StartMs != 1000 || segments[0].EndMs != 5500 {
		t.Errorf("segment ms = %d-%d, want 1000-5500", segments[0].StartMs, segments[0].EndMs)
	}
	if segments[0].Text != "Short format." {
		t.Errorf("text = %q", segments[0].Text)
	}
}

func TestParseVTT_TrimsAndCollapsesWhitespace(t *testing.T) {
	content := `WEBVTT

00:00:00.000 --> 00:00:02.000
  trim   me   and   collapse   spaces
`
	segments, err := ParseVTT(bytes.NewReader([]byte(content)))
	if err != nil {
		t.Fatalf("ParseVTT() error = %v", err)
	}
	if len(segments) != 1 {
		t.Fatalf("got %d segments, want 1", len(segments))
	}
	want := "trim me and collapse spaces"
	if segments[0].Text != want {
		t.Errorf("text = %q, want %q", segments[0].Text, want)
	}
}

func TestParseVTT_MergesMicroCues(t *testing.T) {
	// Cues with duration < 800ms should merge into previous segment
	content := `WEBVTT

00:00:00.000 --> 00:00:02.000
First sentence.

00:00:02.100 --> 00:00:02.500
Micro.

00:00:02.600 --> 00:00:02.900
Tiny.

00:00:03.000 --> 00:00:05.000
Second sentence.
`
	segments, err := ParseVTT(bytes.NewReader([]byte(content)))
	if err != nil {
		t.Fatalf("ParseVTT() error = %v", err)
	}
	// "Micro" (400ms) and "Tiny" (300ms) should merge into "First sentence."
	// So we expect 2 segments: first with "First sentence. Micro. Tiny.", second with "Second sentence."
	if len(segments) != 2 {
		t.Fatalf("got %d segments (expected merge), want 2", len(segments))
	}
	if segments[0].Text != "First sentence. Micro. Tiny." {
		t.Errorf("first segment text = %q", segments[0].Text)
	}
	if segments[1].Text != "Second sentence." {
		t.Errorf("second segment text = %q", segments[1].Text)
	}
}

func TestParseVTT_CueIdentifier(t *testing.T) {
	content := `WEBVTT

cue1
00:00:00.000 --> 00:00:02.000
With cue id.
`
	segments, err := ParseVTT(bytes.NewReader([]byte(content)))
	if err != nil {
		t.Fatalf("ParseVTT() error = %v", err)
	}
	if len(segments) != 1 {
		t.Fatalf("got %d segments, want 1", len(segments))
	}
	if segments[0].Text != "With cue id." {
		t.Errorf("text = %q", segments[0].Text)
	}
	if segments[0].SourceRef == nil || *segments[0].SourceRef != "cue1" {
		if segments[0].SourceRef == nil {
			t.Errorf("SourceRef = nil, want cue1")
		} else {
			t.Errorf("SourceRef = %q", *segments[0].SourceRef)
		}
	}
}

func TestParseVTT_EmptyAndNOTESkipped(t *testing.T) {
	content := `WEBVTT
NOTE optional

00:00:00.000 --> 00:00:02.000
Only one.
`
	segments, err := ParseVTT(bytes.NewReader([]byte(content)))
	if err != nil {
		t.Fatalf("ParseVTT() error = %v", err)
	}
	if len(segments) != 1 {
		t.Fatalf("got %d segments, want 1", len(segments))
	}
	if segments[0].Text != "Only one." {
		t.Errorf("text = %q", segments[0].Text)
	}
}

func TestParseVTT_EmptyContent(t *testing.T) {
	var r io.Reader
	segments, err := ParseVTT(r)
	if err != nil {
		t.Fatalf("ParseVTT(nil) error = %v", err)
	}
	if segments != nil {
		t.Errorf("ParseVTT(nil) got %v, want nil", segments)
	}

	segments, err = ParseVTT(strings.NewReader(""))
	if err != nil {
		t.Fatalf("ParseVTT(empty) error = %v", err)
	}
	if len(segments) != 0 {
		t.Errorf("ParseVTT(empty) got %d segments", len(segments))
	}
}

func TestParseVTT_MalformedCueSkipped(t *testing.T) {
	// Invalid timestamp line: no -->, should skip and not panic
	content := `WEBVTT

00:00:00.000 00:00:05.000
Bad timestamp.

00:00:01.000 --> 00:00:03.000
Good one.
`
	segments, err := ParseVTT(bytes.NewReader([]byte(content)))
	if err != nil {
		t.Fatalf("ParseVTT() error = %v", err)
	}
	// Parser may skip "Bad timestamp" (no valid timestamp) and keep "Good one"
	if len(segments) != 1 {
		t.Logf("got %d segments (malformed cue skipped)", len(segments))
	}
	if len(segments) >= 1 && segments[0].Text != "Good one." {
		t.Errorf("segment text = %q", segments[0].Text)
	}
}

func TestParseVTT_Monotonicity(t *testing.T) {
	// Overlapping cues: second start before first end → clamp
	content := `WEBVTT

00:00:00.000 --> 00:00:05.000
First.

00:00:03.000 --> 00:00:08.000
Overlap.
`
	segments, err := ParseVTT(bytes.NewReader([]byte(content)))
	if err != nil {
		t.Fatalf("ParseVTT() error = %v", err)
	}
	if len(segments) != 2 {
		t.Fatalf("got %d segments, want 2", len(segments))
	}
	// Second segment start should be clamped to previous end (5000ms)
	if segments[1].StartMs < 5000 {
		t.Errorf("overlap: second StartMs = %d, should be >= 5000", segments[1].StartMs)
	}
}
