package utils

import (
	"testing"
)

func TestParseVTT(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantNum  int
		wantFirst string
		wantStart float64
		wantEnd   float64
	}{
		{
			name: "simple VTT with long timestamp",
			content: `WEBVTT

00:00:00.000 --> 00:00:05.000
Hello world.

00:00:05.500 --> 00:00:10.000
Second line.`,
			wantNum:     2,
			wantFirst:   "Hello world.",
			wantStart:   0,
			wantEnd:     5,
		},
		{
			name: "VTT with short timestamp (MM:SS.mmm)",
			content: `WEBVTT

00:01.000 --> 00:05.500
Short format.`,
			wantNum:     1,
			wantFirst:   "Short format.",
			wantStart:   1,   // 00:01.000 = 1 second
			wantEnd:     5.5, // 00:05.500 = 5.5 seconds
		},
		{
			name: "empty and NOTE lines skipped",
			content: `WEBVTT
NOTE optional

00:00:00.000 --> 00:00:02.000
Only one.`,
			wantNum:    1,
			wantFirst:  "Only one.",
			wantStart:  0,
			wantEnd:    2,
		},
		{
			name:     "empty content",
			content:  ``,
			wantNum:  0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			segments, err := ParseVTT(tt.content)
			if err != nil {
				t.Fatalf("ParseVTT() error = %v", err)
			}
			if len(segments) != tt.wantNum {
				t.Errorf("ParseVTT() got %d segments, want %d", len(segments), tt.wantNum)
			}
			if tt.wantNum > 0 {
				if segments[0].Text != tt.wantFirst {
					t.Errorf("first segment text = %q, want %q", segments[0].Text, tt.wantFirst)
				}
				if tt.wantStart >= 0 && segments[0].StartTime != tt.wantStart {
					t.Errorf("first segment StartTime = %v, want %v", segments[0].StartTime, tt.wantStart)
				}
				if tt.wantEnd >= 0 && segments[0].EndTime != tt.wantEnd {
					t.Errorf("first segment EndTime = %v, want %v", segments[0].EndTime, tt.wantEnd)
				}
			}
		})
	}
}

func TestFullTextFromSegments(t *testing.T) {
	segments, _ := ParseVTT(`WEBVTT

00:00:00.000 --> 00:00:02.000
First.

00:00:02.000 --> 00:00:05.000
Second.`)
	got := FullTextFromSegments(segments)
	want := "First.\nSecond."
	if got != want {
		t.Errorf("FullTextFromSegments() = %q, want %q", got, want)
	}
}
