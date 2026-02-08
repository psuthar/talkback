package utils

import (
	"testing"
)

func TestParseZoomRecordingURL(t *testing.T) {
	tests := []struct {
		name      string
		rawURL    string
		wantErr   bool
		wantLen   int    // minimum length of returned UUID (0 = not checked)
		wantExact string // if set, exact match required
	}{
		{
			name:    "play URL",
			rawURL:  "https://zoom.us/rec/play/abc123def456ghi789jkl012mno345pq",
			wantErr: false,
			wantLen: 32,
		},
		{
			name:    "share URL",
			rawURL:  "https://zoom.us/rec/share/xyz789uuidlikestringhere123456789012",
			wantErr: false,
			wantLen: 32,
		},
		{
			name:    "us02web subdomain",
			rawURL:  "https://us02web.zoom.us/rec/play/someMeetingUUIDWithEnoughLengthHere",
			wantErr: false,
			wantLen: 32,
		},
		{
			name:      "recording detail URL with meeting_id",
			rawURL:    "https://zoom.us/recording/detail?meeting_id=abc123def456ghi789jkl012mno345pq",
			wantErr:   false,
			wantExact: "abc123def456ghi789jkl012mno345pq",
		},
		{
			name:      "recording detail URL with encoded meeting_id",
			rawURL:    "https://zoom.us/recording/detail?meeting_id=opaque-id-with-hyphens",
			wantErr:   false,
			wantExact: "opaque-id-with-hyphens",
		},
		{
			name:      "recording detail URL with single URL-encoded meeting_id",
			rawURL:    "https://zoom.us/recording/detail?meeting_id=id%2Bwith%2Bplus",
			wantErr:   false,
			wantExact: "id+with+plus", // Query().Get decodes once
		},
		{
			name:    "recording detail URL missing meeting_id",
			rawURL:  "https://zoom.us/recording/detail",
			wantErr: true,
		},
		{
			name:    "recording detail URL empty meeting_id",
			rawURL:  "https://zoom.us/recording/detail?meeting_id=",
			wantErr: true,
		},
		{
			name:    "invalid URL",
			rawURL:  "not-a-url",
			wantErr: true,
		},
		{
			name:    "non-Zoom URL",
			rawURL:  "https://example.com/rec/play/short",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseZoomRecordingURL(tt.rawURL)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseZoomRecordingURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantExact != "" && got != tt.wantExact {
				t.Errorf("ParseZoomRecordingURL() = %q, want %q", got, tt.wantExact)
			}
			if !tt.wantErr && tt.wantLen > 0 && len(got) < tt.wantLen {
				t.Errorf("ParseZoomRecordingURL() = %q (len %d), want length >= %d", got, len(got), tt.wantLen)
			}
		})
	}
}

func TestIsZoomShareLink(t *testing.T) {
	tests := []struct {
		name   string
		rawURL string
		want   bool
	}{
		{"share URL", "https://zoom.us/rec/share/xyz789uuidlikestringhere123456789012", true},
		{"share URL with path", "https://zoom.us/rec/share/abc123", true},
		{"play URL", "https://zoom.us/rec/play/abc123def456ghi789jkl012mno345pq", false},
		{"recording detail URL", "https://zoom.us/recording/detail?meeting_id=xyz", false},
		{"non-Zoom URL", "https://example.com/rec/share/foo", false},
		{"invalid URL", "not-a-url", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsZoomShareLink(tt.rawURL)
			if got != tt.want {
				t.Errorf("IsZoomShareLink(%q) = %v, want %v", tt.rawURL, got, tt.want)
			}
		})
	}
}
